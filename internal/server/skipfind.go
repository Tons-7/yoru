package server

import (
	"context"
	"maps"
	"strconv"
	"time"

	"yoru/internal/player"
	"yoru/internal/skipfind"
	"yoru/internal/transcode"
)

// When neither the file's chapters nor AniSkip know an episode's opening or ending, Yoru finds it in the sound
// it shares with another episode on disk, or with the show's kept print of it.

// Neighbours tried, nearest first: the same opening runs through a cour.
var neighbours = []int{-1, 1, -2, 2, -3, 3}

// findSkipsSoon searches for the missing parts, hourly per episode, one show at a time; true while one runs.
// Not counted as tried while it can't run: the episode still downloading, or a CPU conversion on screen.
func (s *Server) findSkipsSoon(animeID, episode int, parts []string) bool {
	if s.streams == nil || len(parts) == 0 || s.streams.Converting() || !s.canFind() {
		return false
	}
	if _, ok := s.episodeOnDisk(context.Background(), animeID, episode); !ok {
		return false
	}
	key := sessionID(animeID, episode)
	s.findMu.Lock()
	defer s.findMu.Unlock()
	if s.finding[animeID] {
		return true
	}
	if at, ok := s.findTried[key]; ok && time.Since(at) < time.Hour {
		return false
	}
	maps.DeleteFunc(s.findTried, func(_ string, at time.Time) bool { return time.Since(at) >= time.Hour })
	s.findTried[key] = time.Now()
	s.finding[animeID] = true
	go func() {
		defer func() {
			s.findMu.Lock()
			delete(s.finding, animeID)
			s.findMu.Unlock()
		}()
		ctx, cancel := detached(5 * time.Minute)
		defer cancel()
		s.findSkips(ctx, animeID, episode, parts)
	}()
	return true
}

func (s *Server) findSkips(ctx context.Context, animeID, episode int, parts []string) {
	path, ok := s.episodeOnDisk(ctx, animeID, episode)
	if !ok {
		return
	}
	ffmpeg := s.cfg.Tool("ffmpeg")
	prober := transcode.NewProber(s.cfg.Tool("ffprobe"))
	window := func(path, part string) (start float64, items []uint32, err error) {
		info, err := prober.Probe(ctx, path)
		if err != nil {
			return 0, nil, err
		}
		start, length := 0.0, min(skipfind.OpeningWindow, info.Duration*0.45)
		if part == "ed" {
			start, length = max(0, info.Duration-skipfind.EndingWindow), skipfind.EndingWindow
		}
		items, err = skipfind.Fingerprint(ctx, ffmpeg, path, start, length)
		return start, items, err
	}
	at := func(start float64, from, to int) (float64, float64) {
		return start + float64(from)*skipfind.Point, start + float64(to)*skipfind.Point
	}
	save := func(ep int, part string, from, to float64) {
		r := player.SkipRange{Kind: part, Start: from, End: to, Source: "found"}
		if err := s.store.SaveOwnSkip(ctx, animeID, ep, r); err != nil {
			s.log.Warn("save found skip", "anime", animeID, "episode", ep, "err", err)
		}
	}

	for _, part := range parts {
		start, items, err := window(path, part)
		if err != nil {
			s.log.Debug("fingerprint episode", "anime", animeID, "episode", episode, "err", err)
			return
		}
		if kept, ok := s.store.SkipPrint(ctx, animeID, part); ok {
			if m, ok := skipfind.Covers(kept, items); ok {
				from, to := at(start, m.BStart, m.BEnd)
				save(episode, part, from, to)
				s.log.Info("skip found from the show's print", "anime", animeID, "episode", episode, "part", part)
				continue
			}
		}
		for _, d := range neighbours {
			if episode+d < 1 {
				continue
			}
			other, ok := s.episodeOnDisk(ctx, animeID, episode+d)
			if !ok {
				continue
			}
			oStart, oItems, err := window(other, part)
			if err != nil {
				continue
			}
			m, ok := skipfind.Shared(items, oItems)
			if !ok {
				continue
			}
			from, to := at(start, m.AStart, m.AEnd)
			save(episode, part, from, to)
			from, to = at(oStart, m.BStart, m.BEnd)
			save(episode+d, part, from, to)
			if err := s.store.SaveSkipPrint(ctx, animeID, part, items[m.AStart:m.AEnd]); err != nil {
				s.log.Warn("keep skip print", "anime", animeID, "err", err)
			}
			s.log.Info("skip found in shared sound", "anime", animeID, "episode", episode, "with", episode+d, "part", part)
			break
		}
	}
}

// episodeOnDisk is the episode's whole file: a library one, or a download that has finished.
func (s *Server) episodeOnDisk(ctx context.Context, animeID, episode int) (string, bool) {
	if f, err := s.store.LocalEpisode(ctx, animeID, episode); err == nil && f.Path != "" {
		return f.Path, true
	}
	rec, ok, err := s.store.TorrentForEpisode(ctx, animeID, strconv.Itoa(episode))
	if err != nil || !ok {
		return "", false
	}
	if !s.store.FileComplete(ctx, rec.InfoHash, rec.FileIndex) && (s.cache == nil || !s.cache.Finished(ctx, rec.InfoHash)) {
		return "", false
	}
	path := s.cachePath(rec)
	return path, path != ""
}
