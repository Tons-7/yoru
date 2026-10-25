package server

import (
	"context"
	"maps"
	"time"
)

// readyLead is how near the end the next episode is readied: room for a slow swarm, short enough to be wanted.
const readyLead = 3 * time.Minute

// maybeReadyNext readies the next episode once, in this one's last minutes, and only once this one's file is
// whole: readying must never take speed from what is on screen.
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
