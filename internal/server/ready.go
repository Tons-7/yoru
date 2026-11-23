package server

import (
	"context"
	"maps"
	"time"
)

const readyLead = 3 * time.Minute

func (s *Server) maybeReadyNext(ctx context.Context, animeID, episode int, position, duration float64) {
	if s.playback == nil || s.streams == nil || duration <= 0 || duration-position > readyLead.Seconds() {
		return
	}
	current, ok := s.streams.Episode(sessionID(animeID, episode))
	if !ok || !s.sourceComplete(ctx, current) {
		return
	}
	key := sessionID(animeID, episode)
	s.readyMu.Lock()
	if at, tried := s.readying[key]; tried && time.Since(at) < time.Hour {
		s.readyMu.Unlock()
		return
	}
	maps.DeleteFunc(s.readying, func(_ string, at time.Time) bool { return time.Since(at) >= time.Hour })
	s.readying[key] = time.Now()
	s.readyMu.Unlock()

	go func() {
		ctx, cancel := detached(3 * time.Minute)
		defer cancel()
		began := time.Now()
		next, err := s.playback.ReadyNext(ctx, animeID, episode)
		if err != nil {
			s.log.Debug("next episode not readied", "anime", animeID, "episode", episode, "err", err)
			return
		}
		s.log.Info("next episode readied", "anime", next.AnimeID, "episode", next.Episode,
			"took", time.Since(began).Round(time.Millisecond))
	}()
}
