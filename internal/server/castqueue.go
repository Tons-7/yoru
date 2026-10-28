package server

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"

	"yoru/internal/transcode"
)

// The TV's own queue: the episodes after the one cast, each linked by the episode rather than a session and
// opened on its first ask, so the TV goes on with the phone locked or the page shut.

const (
	castEpisodePrefix = "castep:"
	// castQueued is an evening's worth; each opens only once the TV reaches it.
	castQueued = 12
	// tvAhead is about how far the TV's player reads ahead of what it shows.
	tvAhead = 20.0
	// pageWins: a page's exact report within this long beats the TV's estimate.
	pageWins = 45 * time.Second
)

type castTrack struct {
	Index    int    `json:"index"`
	Language string `json:"language,omitempty"`
	Title    string `json:"title,omitempty"`
	URL      string `json:"url"`
}

// queuedCast is one episode in the TV's queue, as the page hands it over.
type queuedCast struct {
	Key       string      `json:"key"`
	URL       string      `json:"url"`
	Title     string      `json:"title"`
	Subtitle  string      `json:"subtitle"`
	Artwork   string      `json:"artwork,omitempty"`
	Subtitles []castTrack `json:"subtitles"`
}

// castQueue is what plays after this episode with Auto next on, up to what Still watching lets play unasked.
func (s *Server) castQueue(ctx context.Context, animeID, episode int, base string, expires time.Time, subLang string) []queuedCast {
	out := []queuedCast{}
	prefs, err := s.store.Prefs(ctx, animeID)
	if err != nil || !prefs.Bool("playback.autonext") {
		return out
	}
	n := castQueued
	if ask, err := strconv.Atoi(prefs.String("playback.still_watching")); err == nil && ask > 0 {
		n = min(n, ask)
	}
	picks, _, err := s.store.PlayOrder(ctx, animeID, episode, n)
	if err != nil {
		s.log.Warn("cast queue", "anime", animeID, "episode", episode, "err", err)
		return out
	}
	// Looked up once a show: the queue is mostly one, maybe running on into its next season.
	shows := map[int][2]string{}
	for _, p := range picks {
		show, known := shows[p.AnimeID]
		if !known {
			show = [2]string{s.store.ShownTitle(ctx, p.AnimeID), s.store.BannerOf(ctx, p.AnimeID)}
			shows[p.AnimeID] = show
		}
		source := fmt.Sprintf("%s%s:%s", castEpisodePrefix, sessionID(p.AnimeID, p.Episode), subLang)
		root := base + "/c/" + s.signGrant(grant{Source: source, Expires: expires.Unix()}) + "/"
		// "Episode 5" alone: the TV's queue lists what's coming, and a title there could spoil it.
		item := queuedCast{
			Key: sessionID(p.AnimeID, p.Episode), URL: root + "playlist.m3u8", Title: show[0],
			Subtitle: "Episode " + strconv.Itoa(p.Episode), Artwork: show[1], Subtitles: []castTrack{},
		}
		if subLang != "" {
			item.Subtitles = append(item.Subtitles, castTrack{Language: subLang, URL: root + "sub-auto.vtt"})
		}
		out = append(out, item)
	}
	return out
}

// parseCastEpisode reads a queued episode's link: the episode, and the language its subtitles follow.
func parseCastEpisode(source string) (animeID, episode int, lang string, ok bool) {
	rest, found := strings.CutPrefix(source, castEpisodePrefix)
	if !found {
		return 0, 0, "", false
	}
	id, lang, _ := strings.Cut(rest, ":")
	animeID, episode, ok = parseSessionID(id)
	return animeID, episode, lang, ok
}

type tvOpen struct {
	done chan struct{}
	id   string
	err  error
}

// queuedSession is a queued episode's session, opened once however many of the TV's asks arrive at once.
func (s *Server) queuedSession(ctx context.Context, animeID, episode int) (string, error) {
	key := sessionID(animeID, episode)
	s.tvMu.Lock()
	o := s.tvOpens[key]
	// Failed, or reaped since: opened again.
	if o != nil && s.spent(o) {
		o = nil
	}
	if o == nil {
		// Each new one clears out the rest that are spent, so a night of casting doesn't keep them all.
		maps.DeleteFunc(s.tvOpens, func(_ string, old *tvOpen) bool { return s.spent(old) })
		o = &tvOpen{done: make(chan struct{})}
		s.tvOpens[key] = o
		go s.openForTV(o, animeID, episode)
	}
	s.tvMu.Unlock()
	select {
	case <-o.done:
		return o.id, o.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// spent: an open that has finished and failed, or whose session has since been reaped.
func (s *Server) spent(o *tvOpen) bool {
	select {
	case <-o.done:
		_, live := s.streams.Get(o.id)
		return o.err != nil || !live
	default:
		return false
	}
}

// openForTV plays and opens an episode as a page would, by the same handlers, for the TV's any-browser plan.
func (s *Server) openForTV(o *tvOpen, animeID, episode int) {
	defer close(o.done)
	// The TV gives up on a slow first answer and asks again; the open goes on for that ask.
	ctx, cancel := detached(5 * time.Minute)
	defer cancel()
	var started struct {
		StreamURL string `json:"streamUrl"`
	}
	if o.err = within(ctx, s.play, http.MethodPost, "/api/play", map[string]any{"animeId": animeID, "episode": episode},
		&started); o.err != nil {
		return
	}
	if started.StreamURL == "" {
		o.err = errors.New("nothing to play for the TV")
		return
	}
	q := url.Values{"id": {strconv.Itoa(animeID)}, "episode": {strconv.Itoa(episode)}, "source": {started.StreamURL},
		"audio": {s.preferences(ctx, animeID).Audio}}
	var opened struct {
		ID string `json:"id"`
	}
	if o.err = within(ctx, s.streamOpen, http.MethodPost, "/api/stream/open?"+q.Encode(), nil, &opened); o.err != nil {
		return
	}
	o.id = opened.ID
	if session, ok := s.streams.Get(o.id); ok {
		s.heldOnce(session)
	}
	s.log.Info("cast queue opened the next episode for the TV", "anime", animeID, "episode", episode, "session", o.id)
}

// within runs one of Yoru's own handlers as the page would ask it, and reads its answer.
func within(ctx context.Context, h http.HandlerFunc, method, target string, body, out any) error {
	var in io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		in = bytes.NewReader(raw)
	}
	req := httptest.NewRequestWithContext(ctx, method, target, in)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code >= http.StatusMultipleChoices {
		var answer struct {
			Error string `json:"error"`
		}
		json.Unmarshal(rec.Body.Bytes(), &answer)
		return errors.New(cmp.Or(answer.Error, http.StatusText(rec.Code)))
	}
	return json.Unmarshal(rec.Body.Bytes(), out)
}

// autoTrack is a queued episode's text track in the language the cast one had, if it has one.
func autoTrack(session *transcode.Session, lang string) (int, bool) {
	for _, sub := range session.Info.Subtitles {
		if transcode.TextSubtitle(sub.Codec) && sameLanguage(sub.Language, lang) {
			return sub.Index, true
		}
	}
	return 0, false
}

// sameLanguage: releases tag one language "en", "eng" or "en-US"; a tag nobody knows matches only itself.
func sameLanguage(a, b string) bool {
	if a == "" || b == "" || strings.EqualFold(a, b) {
		return a != "" && b != ""
	}
	ta, errA := language.Parse(a)
	tb, errB := language.Parse(b)
	if errA != nil || errB != nil || ta == language.Und || tb == language.Und {
		return false
	}
	baseA, _ := ta.Base()
	baseB, _ := tb.Base()
	return baseA == baseB
}

// tvEstimate moves the TV's place in an episode to at: whether to save it, and how much was played.
func (s *Server) tvEstimate(key string, at float64, now time.Time) (played float64, save bool) {
	s.tvMu.Lock()
	defer s.tvMu.Unlock()
	w := s.tvSeen[key]
	if w == nil {
		// Pruned on each new episode, so a day of casting doesn't keep them all; the first ask is the start.
		maps.DeleteFunc(s.tvSeen, func(_ string, old *tvWatch) bool { return now.Sub(old.savedAt) > 6*time.Hour })
		s.tvSeen[key] = &tvWatch{at: at, savedAt: now}
		return 0, false
	}
	// Back to an earlier place (a rewind, or the episode again): counted on from there.
	if at < w.at {
		w.at, w.savedAt = at, now
		return 0, false
	}
	if at == w.at || now.Sub(w.savedAt) < 10*time.Second {
		return 0, false
	}
	// Played no faster than the clock: a seek forward isn't time watched.
	played = min(at-w.at, now.Sub(w.savedAt).Seconds())
	w.at, w.savedAt = at, now
	return played, now.Sub(s.pageReported[key]) >= pageWins
}

// tvWatch is where the TV has read an episode to, for its progress when no page reports it.
type tvWatch struct {
	at      float64
	savedAt time.Time
}

// noteTV turns the TV's segment asks into the episode's progress, every 10 s, and only while no page reports
// it, as the page knows the TV's exact time and this only about where it reads.
func (s *Server) noteTV(session *transcode.Session, n int) {
	animeID, episode, ok := parseSessionID(session.ID)
	if !ok || session.Info == nil {
		return
	}
	at := max(0, session.SegmentStart(n)-tvAhead)
	played, save := s.tvEstimate(sessionID(animeID, episode), at, time.Now())
	if !save {
		return
	}
	go func() {
		ctx, cancel := detached(30 * time.Second)
		defer cancel()
		if _, err := s.savePosition(ctx, animeID, episode, at, session.Info.Duration, played); err != nil {
			s.log.Warn("save the TV's progress", "anime", animeID, "episode", episode, "err", err)
		}
		// Near the end the next is made ready, as for a page: the TV asks for it a little before.
		s.maybeReadyNext(ctx, animeID, episode, at, session.Info.Duration)
	}()
}
