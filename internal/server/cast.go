package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"yoru/internal/proc"
	"yoru/internal/transcode"
)

const castPrefix = "cast:"

func (s *Server) castLink(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	if s.rebind != nil && !s.bound() {
		send(w, http.StatusConflict, map[string]any{
			"error": "the TV can't reach Yoru. Turn on access from other devices under Settings, Devices.",
		})
		return
	}
	target, note := session, ""
	animeID, _, _ := parseSessionID(session.ID)
	want := s.streamWant(r.Context(), animeID, "")
	if raw := r.URL.Query().Get("burn"); raw != "" {
		track, err := strconv.Atoi(raw)
		if err != nil || track < 0 {
			send(w, http.StatusBadRequest, map[string]any{"error": "invalid subtitle track"})
			return
		}
		delay, _ := strconv.ParseFloat(r.URL.Query().Get("delay"), 64)
		if math.IsNaN(delay) || math.IsInf(delay, 0) {
			delay = 0
		}
		burned, err := s.burnedSession(r.Context(), session, track, delay, want)
		switch {
		case errors.Is(err, errStillArriving):
			note = "Subtitles go as text this time: drawing them in needs the whole episode downloaded."
		case err != nil:
			s.log.Warn("cast with subtitles drawn in", "session", session.ID, "err", err)
			note = "Subtitles go as text: drawing them in failed."
		default:
			s.tvHold(burned, true)
			target = burned
		}
	}
	if plan := session.CurrentPlan(); target == session && (plan.ForDevice || plan.MaxRate > 0) {
		id := transcode.BaseID(session.ID) + "-cast"
		if base, ok := s.streams.Get(transcode.BaseID(session.ID)); ok && want == (transcode.Want{}) &&
			base.Source == session.Source && base.AudioTrack() == session.AudioTrack() && base.Sound() == session.Sound() {
			id = base.ID
		}
		tv, err := s.streams.OpenFor(r.Context(), id, session.Source, s.readable(r.Context(), session), nil, want)
		if err != nil {
			s.fail(w, "open stream for the TV", err)
			return
		}
		s.tvHold(tv, true)
		if tv.ID != transcode.BaseID(session.ID) {
			tv.SetAudioTrack(session.AudioTrack())
			tv.SetSound(session.Sound())
		}
		session, target = tv, tv
	} else if target == session {
		s.tvHold(session, false)
	}

	base := s.tvBase(r)
	expires := time.Now().Add(externalFor)
	root := base + "/c/" + s.signGrant(grant{Source: castPrefix + target.ID, Expires: expires.Unix()}) + "/"

	tracks := []castTrack{}
	for _, sub := range session.Info.Subtitles {
		if target != session || !transcode.TextSubtitle(sub.Codec) {
			continue
		}
		tracks = append(tracks, castTrack{Index: sub.Index, Language: sub.Language, Title: sub.Title,
			URL: root + "sub-" + strconv.Itoa(sub.Index) + ".vtt"})
	}
	if note == "" && len(tracks) > 0 && !s.sourceComplete(r.Context(), session) {
		note = "TV subtitles cover only what's downloaded so far. Cast again once it finishes."
	}
	_, episode, _ := parseSessionID(session.ID)
	send(w, http.StatusOK, map[string]any{
		"url": root + "playlist.m3u8", "subtitles": tracks, "burned": target != session, "note": note,
		"queue": s.castQueue(r.Context(), animeID, episode, base, expires, r.URL.Query().Get("sub")),
	})
}

func (s *Server) tvBase(r *http.Request) string {
	if names := s.httpsNames(); len(names) > 0 && r.TLS == nil {
		return fmt.Sprintf("https://%s:%s", names[0], s.port())
	}
	if lan := lanAddrs(); loopbackHost(r.Host) && len(lan) > 0 {
		return fmt.Sprintf("http://%s:%s", lan[0], s.port())
	}
	return requestOrigin(r)
}

var errStillArriving = errors.New("the episode is still downloading")

func (s *Server) tvHold(sess *transcode.Session, opened bool) {
	s.tvMu.Lock()
	defer s.tvMu.Unlock()
	if s.tvHolds == nil {
		s.tvHolds = map[*transcode.Session]bool{}
	}
	for held := range s.tvHolds {
		if cur, ok := s.streams.Peek(held.ID); !ok || cur != held {
			delete(s.tvHolds, held)
		}
	}
	if s.tvHolds[sess] {
		if opened {
			s.streams.Close(sess.ID)
		}
		return
	}
	if !opened && !s.streams.Hold(sess.ID) {
		return
	}
	s.tvHolds[sess] = true
}

func (s *Server) burnedSession(ctx context.Context, session *transcode.Session, track int, delay float64, want transcode.Want) (*transcode.Session, error) {
	id := fmt.Sprintf("%s-burn%d-%d", transcode.BaseID(session.ID), track, int(math.Round(delay*10)))
	if want.Soften {
		id += "-soft"
	}
	if transcode.PictureSubtitle(session.Info.SubtitleCodec(track)) {
		return s.streams.OpenBurned(ctx, id, session.Source, s.readable(ctx, session), transcode.Burn{Pictures: track, Delay: delay}, want)
	}
	if s.subtitles == nil {
		return nil, errors.New("subtitles unavailable")
	}
	if !s.sourceComplete(ctx, session) {
		return nil, errStillArriving
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	req.SetPathValue("id", session.ID)
	req.SetPathValue("track", strconv.Itoa(track))
	rec := httptest.NewRecorder()
	s.streamSubtitle(rec, req)
	if rec.Code != http.StatusOK {
		return nil, fmt.Errorf("subtitle track %d: HTTP %d", track, rec.Code)
	}
	file := filepath.Join(session.Dir(), fmt.Sprintf("cast-%d-%d.ass", track, int(math.Round(delay*1000))))
	cmd := proc.Hide(exec.CommandContext(ctx, s.cfg.Tool("ffmpeg"), "-hide_banner", "-loglevel", "error", "-y",
		"-itsoffset", fmt.Sprintf("%.3f", delay), "-i", "pipe:0", "-c:s", "ass", file))
	cmd.Stdin = bytes.NewReader(rec.Body.Bytes())
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("subtitles: %v: %s", err, bytes.TrimSpace(out))
	}
	local := s.readable(ctx, session)
	fonts := ""
	if s.waitFonts(ctx, session) {
		fonts = filepath.Join(session.Dir(), "fonts")
	}
	return s.streams.OpenBurned(ctx, id, session.Source, local, transcode.Burn{Subtitles: file, Fonts: fonts}, want)
}

func (s *Server) waitFonts(ctx context.Context, session *transcode.Session) bool {
	started := false
	for {
		s.fontsMu.Lock()
		_, done := s.fonts[session.Dir()]
		_, running := s.fontJobs[session.Dir()]
		s.fontsMu.Unlock()
		switch {
		case done:
			return true
		case !running && started:
			return false
		case !running:
			s.extractFontsAsync(session, "/api/stream/"+session.ID)
			started = true
			continue
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *Server) castFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Range")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	g, ok := s.openGrant(r.PathValue("grant"), time.Now())
	queued, episode, lang, isQueued := parseCastEpisode(g.Source)
	if !ok || !strings.HasPrefix(g.Source, castPrefix) && !isQueued {
		http.Error(w, "this cast link has expired; cast again from Yoru", http.StatusForbidden)
		return
	}
	defer s.serving()()
	id := strings.TrimPrefix(g.Source, castPrefix)
	if isQueued {
		var err error
		if id, err = s.queuedSession(r.Context(), queued, episode); err != nil {
			s.log.Warn("cast queue", "anime", queued, "episode", episode, "err", err)
			http.Error(w, "this episode couldn't be opened for the TV", http.StatusBadGateway)
			return
		}
	}
	r.SetPathValue("id", id)
	file := r.PathValue("file")
	switch {
	case file == "playlist.m3u8":
		s.streamPlaylist(w, r)
	case file == "init.mp4":
		s.streamInit(w, r)
	case file == "sub-auto.vtt":
		session, live := s.streams.Get(id)
		if !live {
			http.NotFound(w, r)
			return
		}
		track, found := autoTrack(session, lang)
		if !found {
			w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
			w.Write([]byte("WEBVTT\n"))
			return
		}
		r.SetPathValue("track", strconv.Itoa(track))
		s.castSubtitle(w, r, -1)
	case file == "master.m3u8" || strings.HasPrefix(file, "subs-") && strings.HasSuffix(file, ".m3u8"):
		session, live := s.streams.Get(id)
		if !live {
			http.NotFound(w, r)
			return
		}
		body := airplayMaster(session, r.URL.Query().Get("sub"))
		if file != "master.m3u8" {
			track, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(file, "subs-"), ".m3u8"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			body = airplaySubtitles(session, track)
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte(body))
	case strings.HasPrefix(file, "subs-") && strings.HasSuffix(file, ".vtt"):
		track, piece, _ := strings.Cut(strings.TrimSuffix(strings.TrimPrefix(file, "subs-"), ".vtt"), "-")
		n, err := strconv.Atoi(piece)
		if err != nil || n < 0 {
			http.NotFound(w, r)
			return
		}
		r.SetPathValue("track", track)
		s.castSubtitle(w, r, n)
	case strings.HasPrefix(file, "sub-") && strings.HasSuffix(file, ".vtt"):
		r.SetPathValue("track", strings.TrimSuffix(strings.TrimPrefix(file, "sub-"), ".vtt"))
		s.castSubtitle(w, r, -1)
	case strings.HasSuffix(file, ".mp4"):
		r.SetPathValue("segment", file)
		if session, live := s.streams.Get(id); live {
			if n, err := strconv.Atoi(strings.TrimSuffix(file, ".mp4")); err == nil {
				s.noteTV(session, n)
			}
		}
		s.streamSegment(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) castSubtitle(w http.ResponseWriter, r *http.Request, piece int) {
	rec := httptest.NewRecorder()
	s.streamSubtitle(rec, r)
	if rec.Code != http.StatusOK {
		http.Error(w, "subtitles unavailable", rec.Code)
		return
	}
	vtt, err := s.castVTT(r.Context(), rec.Body.Bytes())
	if err != nil {
		s.log.Warn("cast subtitles", "err", err)
		http.Error(w, "subtitles could not be converted", http.StatusInternalServerError)
		return
	}
	if piece >= 0 {
		vtt = airplayCues(vtt, piece)
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(vtt)
}

func (s *Server) castVTT(ctx context.Context, source []byte) ([]byte, error) {
	sum := sha256.Sum256(source)
	key := hex.EncodeToString(sum[:])
	s.castSubsMu.Lock()
	vtt, ok := s.castSubs[key]
	s.castSubsMu.Unlock()
	if ok {
		return vtt, nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := proc.Hide(exec.CommandContext(ctx, s.cfg.Tool("ffmpeg"), "-hide_banner", "-loglevel", "error",
		"-i", "pipe:0", "-f", "webvtt", "pipe:1"))
	cmd.Stdin = bytes.NewReader(source)
	vtt, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	s.castSubsMu.Lock()
	if s.castSubs == nil || len(s.castSubs) >= 8 {
		s.castSubs = map[string][]byte{}
	}
	s.castSubs[key] = vtt
	s.castSubsMu.Unlock()
	return vtt, nil
}
