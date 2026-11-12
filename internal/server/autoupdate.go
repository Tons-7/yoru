package server

import (
	"context"
	"time"

	"yoru/internal/update"
)

var (
	autoUpdateEvery = 15 * time.Second
	autoUpdateRetry = time.Hour
)

func (s *Server) busyWith(ctx context.Context) []string {
	s.togetherMu.Lock()
	rooms := len(s.rooms)
	s.togetherMu.Unlock()
	var out []string
	if s.streams != nil && len(s.streams.Sources()) > 0 {
		out = append(out, "The open episode stops")
	}
	if s.othersWatching() {
		out = append(out, "Another device stops playing")
	}
	if rooms > 0 {
		out = append(out, "Your watch-together room closes")
	}
	if s.downloadsMoving(ctx) {
		out = append(out, "Downloads pause, then carry on")
	}
	return out
}

func (s *Server) autoUpdates(ctx context.Context) bool {
	prefs, err := s.store.Prefs(ctx, 0)
	return err == nil && prefs.Bool("update.auto")
}

func (s *Server) AutoUpdate(ctx context.Context) {
	if s.updater == nil {
		return
	}
	tick := time.NewTicker(autoUpdateEvery)
	defer tick.Stop()
	tried, triedAt := "", time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		st := s.updater.Status()
		if !st.Available || st.Latest == nil || (st.Stage != update.StageIdle && st.Stage != update.StageFailed) {
			continue
		}
		if st.Latest.Version == tried && time.Since(triedAt) < autoUpdateRetry || !s.autoUpdates(ctx) {
			continue
		}
		tried, triedAt = st.Latest.Version, time.Now()
		s.log.Info("downloading an update in the background", "version", tried)
		if err := s.updater.Prepare(); err != nil {
			s.log.Warn("automatic update", "version", tried, "err", err)
		}
	}
}
