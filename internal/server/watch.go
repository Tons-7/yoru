package server

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"yoru/internal/player"
	"yoru/internal/store"
)

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(out); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return false
	}
	return true
}

// Must match the episode catalogue's key, which is the bare number.
func epKey(episode int) string { return fmt.Sprint(episode) }

// playerNotes are what the browser player may log: an episode's end is decided in the page, so only it can say.
var playerNotes = map[string]bool{"ended": true, "end-stuck": true, "countdown-cleared": true, "next": true}

// playerNote logs one end-of-episode event from the page, a few short fields at most.
func (s *Server) playerNote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int            `json:"animeId"`
		Episode int            `json:"episode"`
		Event   string         `json:"event"`
		Detail  map[string]any `json:"detail"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil || !playerNotes[body.Event] {
		send(w, http.StatusBadRequest, map[string]any{"error": "unknown note"})
		return
	}
	args := []any{"anime", body.AnimeID, "episode", body.Episode, "event", body.Event}
	keys := slices.Sorted(maps.Keys(body.Detail))
	for _, k := range keys[:min(len(keys), 12)] {
		v := fmt.Sprint(body.Detail[k])
		args = append(args, k[:min(len(k), 24)], v[:min(len(v), 80)])
	}
	s.log.Info("player", args...)
	w.WriteHeader(http.StatusNoContent)
}

// progress takes the browser player's reports, which resuming and the trackers depend on.
func (s *Server) progress(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID  int     `json:"animeId"`
		Episode  int     `json:"episode"`
		Position float64 `json:"position"`
		Duration float64 `json:"duration"`
		Played   float64 `json:"played"`
		// Paused, and final (the episode ended or the player closed), are for the Discord card.
		Paused bool `json:"paused"`
		Final  bool `json:"final"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 || body.Episode <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId and episode are required"})
		return
	}

	ctx := r.Context()
	// The store decides what counts as watched, for this and the mpv path alike, so a client cannot claim it.
	watched, err := s.store.SavePlayback(ctx, store.PlaybackState{
		AnimeID:  body.AnimeID,
		EpKey:    epKey(body.Episode),
		Position: body.Position,
		Duration: body.Duration,
		Played:   body.Played,
		Ending:   s.endingFor(ctx, body.AnimeID, body.Episode),
	})
	if err != nil {
		s.fail(w, "save playback", err)
		return
	}
	if !body.Final {
		s.maybeReadyNext(ctx, body.AnimeID, body.Episode, body.Position, body.Duration)
	}
	host := fromHost(r)
	s.presence.Report(ctx, body.AnimeID, body.Episode, body.Position, body.Duration, body.Paused, body.Final, host)
	if !host {
		s.noteWatching(r, body.AnimeID, body.Episode, body.Final || body.Paused)
	}

	if watched && s.syncer != nil {
		if err := s.syncer.Watched(ctx, body.AnimeID, body.Episode); err != nil {
			s.log.Warn("sync watched", "anime", body.AnimeID, "err", err)
		}
	}

	send(w, http.StatusOK, map[string]any{"saved": true, "watched": watched})
}

// prepare resolves an episode's release before play is asked, so the search is done by then.
func (s *Server) prepare(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int `json:"animeId"`
		Episode int `json:"episode"`
		Season  int `json:"season"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 || body.Episode <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId and episode are required"})
		return
	}
	if s.prefetch == nil {
		send(w, http.StatusOK, map[string]any{"preparing": false})
		return
	}
	// Already on screen, or starting: playback is resolving this very episode.
	if _, starting := s.playback.Stage(body.AnimeID, body.Episode); starting {
		send(w, http.StatusOK, map[string]any{"preparing": false})
		return
	}
	if s.streams != nil {
		if _, live := s.streams.Episode(sessionID(body.AnimeID, body.Episode)); live {
			send(w, http.StatusOK, map[string]any{"preparing": false})
			return
		}
	}

	// Season 0 lets the finder fill from the show's own title; forcing 1 here
	// searched a second season's episodes as if they were season one.
	s.prefetch.PrepareTarget(body.AnimeID, body.Episode, body.Season, s.preferences(r.Context(), body.AnimeID))
	send(w, http.StatusAccepted, map[string]any{"preparing": true})
}

// skips serves the browser player's skip times by the order of trust below, and looks for what none knows.
func (s *Server) skips(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	animeID, _ := strconv.Atoi(q.Get("anime"))
	episode, _ := strconv.Atoi(q.Get("episode"))
	if animeID == 0 || episode <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "anime and episode are required"})
		return
	}
	duration, _ := strconv.Atoi(q.Get("duration"))

	ctx := r.Context()
	prefs, _ := s.store.Prefs(ctx, animeID)
	own, err := s.store.OwnSkips(ctx, animeID, episode)
	if err != nil {
		s.fail(w, "skip ranges", err)
		return
	}

	// Each part from the most trusted source that knows it: the viewer's marks, the file's own chapters (they
	// fit this exact release), AniSkip, then what Yoru found in the sound.
	chapters := s.chapterSkips(animeID, episode, player.SkipNames(prefs.String("playback.skip_chapters")))
	ranges := player.Merge(player.From(own, "marked"), chapters)
	// Sharing asks AniSkip either way: what it lacks is what the chapters can give it.
	sharing := prefs.Bool("skips.share")
	if sharing || !player.HasPart(ranges, "op") || !player.HasPart(ranges, "ed") {
		if s.enricher != nil {
			if err := s.enricher.Skips(ctx, animeID, episode, duration); err != nil {
				s.log.Debug("fetch skip times", "anime", animeID, "err", err)
			}
		}
		stored, err := s.store.SkipRanges(ctx, animeID, episode)
		if err != nil {
			s.fail(w, "skip ranges", err)
			return
		}
		ranges = player.Merge(ranges, stored)
		if sharing && s.enricher != nil && duration > 0 {
			for _, c := range chapters {
				if !player.HasPart(stored, c.Part()) {
					go s.shareSkip(animeID, episode, c, float64(duration))
				}
			}
		}
	}
	ranges = player.Merge(ranges, player.From(own, "found"))

	var missing []string
	for _, part := range []string{"op", "ed"} {
		if !player.HasPart(ranges, part) {
			missing = append(missing, part)
		}
	}
	send(w, http.StatusOK, map[string]any{
		"ranges":  ranges,
		"finding": s.findSkipsSoon(animeID, episode, missing),
		"share":   sharing,
	})
}

// endingFor is where the ending starts by the skip times' order of trust, as for mpv; 0 unless that rule is on.
func (s *Server) endingFor(ctx context.Context, animeID, episode int) float64 {
	prefs, err := s.store.Prefs(ctx, animeID)
	if err != nil || prefs.String("sync.progress_at") != store.EndingThreshold {
		return 0
	}
	own, _ := s.store.OwnSkips(ctx, animeID, episode)
	stored, _ := s.store.SkipRanges(ctx, animeID, episode)
	return player.EndingStart(player.Merge(player.From(own, "marked"), s.chapterSkips(animeID, episode, nil), stored,
		player.From(own, "found")))
}

func (s *Server) shareSkip(animeID, episode int, r player.SkipRange, length float64) {
	ctx, cancel := detached(time.Minute)
	defer cancel()
	if err := s.enricher.ShareSkip(ctx, animeID, episode, r, length); err != nil {
		s.log.Debug("share skip time", "anime", animeID, "episode", episode, "err", err)
	}
}

// chapterSkips reads the open file's skippable chapters, names too; not stored, as each release has its own.
func (s *Server) chapterSkips(animeID, episode int, names []string) []player.SkipRange {
	if s.streams == nil {
		return nil
	}
	session, ok := s.streams.Episode(sessionID(animeID, episode))
	if !ok || session.Info == nil {
		return nil
	}

	var out []player.SkipRange
	for _, c := range session.Info.Chapters {
		if kind := player.ChapterKind(c.Kind, c.Title, names); kind != "" {
			out = append(out, player.SkipRange{Kind: kind, Start: c.Start, End: c.End, Source: "chapters"})
		}
	}
	return out
}

// setWatched marks an episode watched or not by hand. Unwatching rewinds
// progress rather than deleting it, which is what the trackers understand.
func (s *Server) setWatched(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int   `json:"animeId"`
		Episode int   `json:"episode"`
		Watched *bool `json:"watched"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 || body.Episode <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId and episode are required"})
		return
	}

	ctx := r.Context()
	watched := body.Watched == nil || *body.Watched

	// Saved here and pushed after the response, so a tracker that's down fails nothing.
	var err error
	if watched {
		_, err = s.store.MarkWatched(ctx, body.AnimeID, body.Episode)
	} else {
		err = s.store.Unwatch(ctx, body.AnimeID, body.Episode)
	}
	if err != nil {
		s.fail(w, "set watched", err)
		return
	}
	s.pushSoon(body.AnimeID)
	if watched && s.cache != nil {
		if _, err := s.cache.AutoDelete(ctx, body.AnimeID); err != nil {
			s.log.Warn("auto-delete watched", "anime", body.AnimeID, "err", err)
		}
	}
	send(w, http.StatusOK, map[string]any{"animeId": body.AnimeID, "episode": body.Episode, "watched": watched})
}

// pushSoon sends one anime to the trackers behind the response.
func (s *Server) pushSoon(animeID int) {
	if s.syncer == nil {
		return
	}
	go func() {
		ctx, cancel := detached(2 * time.Minute)
		defer cancel()
		if err := s.syncer.PushOne(ctx, animeID); err != nil {
			s.log.Warn("push change", "anime", animeID, "err", err)
		}
	}()
}

// setScore rates a show 0-100; 0 clears it. Both trackers score per show.
func (s *Server) setScore(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int `json:"animeId"`
		Score   int `json:"score"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId is required"})
		return
	}
	if body.Score < 0 || body.Score > 100 {
		send(w, http.StatusBadRequest, map[string]any{"error": "score must be 0-100"})
		return
	}
	if err := s.store.SetScore(r.Context(), body.AnimeID, body.Score); err != nil {
		s.fail(w, "set score", err)
		return
	}
	s.pushSoon(body.AnimeID)
	send(w, http.StatusOK, map[string]any{"animeId": body.AnimeID, "score": body.Score})
}

// setStatus backs the list tags: watching, completed, on hold, dropped, planning, rewatching.
func (s *Server) setStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int    `json:"animeId"`
		Status  string `json:"status"`
		Score   *int   `json:"score"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId is required"})
		return
	}
	if !store.ValidStatus(body.Status) {
		send(w, http.StatusBadRequest, map[string]any{
			"error": "unknown status", "allowed": store.ListStatuses,
		})
		return
	}

	// Zero means unrated, so "leave it alone" has its own value.
	score := -1
	if body.Score != nil {
		score = *body.Score
		if score < 0 || score > 100 {
			send(w, http.StatusBadRequest, map[string]any{"error": "score must be 0-100"})
			return
		}
	}

	ctx := r.Context()
	if err := s.store.SetListStatus(ctx, body.AnimeID, body.Status, score); err != nil {
		s.fail(w, "set status", err)
		return
	}

	// Only a show being watched now subscribes to release notifications; following
	// on any status announced every episode of merely-planned shows.
	if err := s.store.SetFollow(ctx, body.AnimeID,
		body.Status == store.StatusCurrent || body.Status == store.StatusRepeating); err != nil {
		s.log.Warn("follow on status change", "anime", body.AnimeID, "err", err)
	}

	s.pushSoon(body.AnimeID)
	send(w, http.StatusOK, map[string]any{"animeId": body.AnimeID, "status": body.Status})
}

// removeStatus takes a show off the list, trackers first: one failing leaves the show listed everywhere.
func (s *Server) removeStatus(w http.ResponseWriter, r *http.Request) {
	animeID, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || animeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "anime id is required"})
		return
	}
	if s.syncer != nil {
		err = s.syncer.Remove(r.Context(), animeID)
	} else {
		err = s.store.RemoveListEntry(r.Context(), animeID)
	}
	if err != nil {
		s.log.Warn("remove from list", "anime", animeID, "err", err)
		send(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	send(w, http.StatusOK, map[string]any{"animeId": animeID, "removed": true})
}

// dismissResume removes an episode from continue-watching without pretending it was finished.
func (s *Server) dismissResume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int `json:"animeId"`
		Episode int `json:"episode"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 || body.Episode <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId and episode are required"})
		return
	}

	if err := s.store.DismissResume(r.Context(), body.AnimeID, epKey(body.Episode)); err != nil {
		s.fail(w, "dismiss resume", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"dismissed": true})
}
