package server

import (
	"context"
	"math"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"yoru/internal/player"
	"yoru/internal/proc"
	"yoru/internal/skipfind"
	"yoru/internal/transcode"
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
	if !skipSpan(body.Start, body.End) {
		send(w, http.StatusBadRequest, map[string]any{"error": "an opening or ending runs 5 seconds to 5 minutes"})
		return
	}
	mark := player.SkipRange{Kind: body.Kind, Start: body.Start, End: body.End, Source: "marked"}
	// "chapters" or "sound": what the mark was lined up with, which the player tells the viewer.
	snapped := s.snapMark(r.Context(), body.AnimeID, body.Episode, &mark)
	if err := s.store.SaveOwnSkip(r.Context(), body.AnimeID, body.Episode, mark); err != nil {
		s.fail(w, "mark skip", err)
		return
	}
	go s.afterMark(body.AnimeID, body.Episode, mark, body.Duration)
	send(w, http.StatusOK, map[string]any{"ok": true, "start": mark.Start, "end": mark.End, "snapped": snapped})
}

// snapMark lines a mark up, reading the file or the stream playing it (what was just watched is fetched).
func (s *Server) snapMark(ctx context.Context, animeID, episode int, mark *player.SkipRange) string {
	// Short: a slow swarm can hold the bytes past the playhead, and the mark is saved as given after.
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	path, ok := s.episodeOnDisk(ctx, animeID, episode)
	var chapters []transcode.Chapter
	live := false
	if s.streams != nil {
		if session, found := s.streams.Episode(sessionID(animeID, episode)); found {
			live = true
			if !ok {
				path = session.Source
			}
			if session.Info != nil {
				chapters = session.Info.Chapters
			}
		}
	}
	// No stream open: the file's chapters from the file, as the sound search reads them.
	if !live && ok {
		if info, err := transcode.NewProber(s.cfg.Tool("ffprobe")).Probe(ctx, path); err == nil {
			chapters = info.Chapters
		}
	}
	return s.snapEdges(ctx, path, chapters, mark)
}

// snapEdges moves a range's ends to a near chapter start, else the song's sound edge, and says which ("" none).
func (s *Server) snapEdges(ctx context.Context, path string, chapters []transcode.Chapter, r *player.SkipRange) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ffmpeg := s.cfg.Tool("ffmpeg")
	raw := *r
	edges := [2]*float64{&r.Start, &r.End}
	var moved [2]string
	var wg sync.WaitGroup
	for i, at := range edges {
		if t, near := nearChapter(chapters, *at); near {
			*at, moved[i] = t, "chapters"
			continue
		}
		if path == "" {
			continue
		}
		wg.Go(func() {
			from, length := skipfind.SnapWindow(*at)
			quiet, err := skipfind.Silences(ctx, ffmpeg, path, from, length)
			if err != nil {
				s.log.Debug("snap skip edge", "err", err)
				return
			}
			if t, found := skipfind.Snap(quiet, *at, i == 0, from, from+length); found {
				*at, moved[i] = t, "sound"
			}
		})
	}
	wg.Wait()
	if !skipSpan(r.Start, r.End) {
		r.Start, r.End = raw.Start, raw.End
		return ""
	}
	if moved[0] == "chapters" || moved[1] == "chapters" {
		return "chapters"
	}
	return max(moved[0], moved[1])
}

// nearChapter is the chapter start nearest at within reach; the first, at 0, starts no part.
func nearChapter(chapters []transcode.Chapter, at float64) (float64, bool) {
	best, found := at, false
	for _, c := range chapters {
		if t := c.Start; t > 0 && math.Abs(t-at) <= skipfind.SnapReach && (!found || math.Abs(t-at) < math.Abs(best-at)) {
			best, found = t, true
		}
	}
	return best, found
}

func skipSpan(start, end float64) bool {
	span := end - start
	return start >= 0 && span >= 5 && span <= 300
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
