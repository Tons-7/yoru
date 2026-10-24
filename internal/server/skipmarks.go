package server

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"yoru/internal/player"
	"yoru/internal/proc"
	"yoru/internal/skipfind"
)

// markSkip keeps the viewer's own opening or ending for an episode, which beats every other source.
func (s *Server) markSkip(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID  int     `json:"animeId"`
		Episode  int     `json:"episode"`
		Kind     string  `json:"kind"`
		Start    float64 `json:"start"`
		End      float64 `json:"end"`
		Duration float64 `json:"duration"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 || body.Episode <= 0 || body.Kind != "op" && body.Kind != "ed" {
		send(w, http.StatusBadRequest, map[string]any{"error": "a show, an episode and op or ed are required"})
		return
	}
	if span := body.End - body.Start; body.Start < 0 || span < 5 || span > 300 {
		send(w, http.StatusBadRequest, map[string]any{"error": "an opening or ending runs 5 seconds to 5 minutes"})
		return
	}
	mark := player.SkipRange{Kind: body.Kind, Start: body.Start, End: body.End, Source: "marked"}
	if err := s.store.SaveOwnSkip(r.Context(), body.AnimeID, body.Episode, mark); err != nil {
		s.fail(w, "mark skip", err)
		return
	}
	go s.afterMark(body.AnimeID, body.Episode, mark, body.Duration)
	send(w, http.StatusOK, map[string]any{"ok": true})
}

// afterMark keeps the marked song's sound as the show's print, for later episodes, and gives it to AniSkip.
func (s *Server) afterMark(animeID, episode int, mark player.SkipRange, duration float64) {
	ctx, cancel := detached(2 * time.Minute)
	defer cancel()
	if s.enricher != nil {
		if err := s.enricher.ShareSkip(ctx, animeID, episode, mark, duration); err != nil {
			s.log.Debug("share marked skip", "anime", animeID, "err", err)
		}
	}
	path, ok := s.episodeOnDisk(ctx, animeID, episode)
	if !ok || !s.canFind() {
		return
	}
	items, err := skipfind.Fingerprint(ctx, s.cfg.Tool("ffmpeg"), path, mark.Start, mark.End-mark.Start)
	if err == nil && len(items) > 0 {
		err = s.store.SaveSkipPrint(ctx, animeID, mark.Kind, items)
	}
	if err != nil {
		s.log.Debug("keep marked skip's print", "anime", animeID, "err", err)
	}
}

// forgetSkipMarks drops the viewer's marks on an episode, so its other sources show again.
func (s *Server) forgetSkipMarks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int `json:"animeId"`
		Episode int `json:"episode"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.store.ForgetMarks(r.Context(), body.AnimeID, body.Episode); err != nil {
		s.fail(w, "forget skip marks", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"ok": true})
}

// voteSkip passes the player's verdict on an AniSkip time on: kept through the skip, or undone as wrong.
func (s *Server) voteSkip(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID string `json:"id"`
		Up bool   `json:"up"`
	}
	if !decode(w, r, &body) {
		return
	}
	if s.enricher != nil {
		go func() {
			ctx, cancel := detached(time.Minute)
			defer cancel()
			if err := s.enricher.VoteSkip(ctx, body.ID, body.Up); err != nil {
				s.log.Debug("vote on skip", "id", body.ID, "err", err)
			}
		}()
	}
	w.WriteHeader(http.StatusNoContent)
}

// skipFinder says whether this ffmpeg can find openings itself; a build without chromaprint can't.
func (s *Server) skipFinder(w http.ResponseWriter, _ *http.Request) {
	send(w, http.StatusOK, map[string]any{"available": s.canFind()})
}

var finderCheck struct {
	sync.Mutex
	tool string
	ok   bool
	at   time.Time
}

// canFind asks the ffmpeg in use once; a "no" is asked again later, as Setup can install another.
func (s *Server) canFind() bool {
	tool := s.cfg.Tool("ffmpeg")
	finderCheck.Lock()
	defer finderCheck.Unlock()
	if finderCheck.tool != tool || !finderCheck.ok && time.Since(finderCheck.at) > 10*time.Minute {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		out, err := proc.Hide(exec.CommandContext(ctx, tool, "-hide_banner", "-muxers")).Output()
		cancel()
		finderCheck.tool, finderCheck.at = tool, time.Now()
		finderCheck.ok = err == nil && strings.Contains(string(out), "chromaprint")
	}
	return finderCheck.ok
}
