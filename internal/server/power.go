package server

import (
	"context"
	"time"

	"yoru/internal/power"
)

const (
	awakeCheck  = 30 * time.Second
	awakeLinger = 2 * time.Minute
)

func (s *Server) KeepAwake(ctx context.Context, k *power.Keeper) {
	tick := time.NewTicker(awakeCheck)
	defer tick.Stop()
	var busyAt time.Time
	var why string
	for {
		if reason := s.awakeReason(ctx); reason != "" {
			busyAt, why = time.Now(), reason
		}
		if time.Since(busyAt) < awakeLinger {
			k.Set(why)
		} else {
			k.Set("")
		}
		select {
		case <-ctx.Done():
			k.Set("")
			return
		case <-tick.C:
		}
	}
}

func (s *Server) awakeReason(ctx context.Context) string {
	prefs, err := s.store.Prefs(ctx, 0)
	if err != nil {
		return ""
	}
	if prefs.Bool("power.awake_watching") && s.othersWatching() {
		return "another device is watching"
	}
	if prefs.Bool("power.awake_downloading") && s.downloadsMoving(ctx) {
		return "downloads are running"
	}
	return ""
}

func (s *Server) serving() func() {
	s.linkReads.Add(1)
	return func() {
		s.linkReads.Add(-1)
		s.linkReadAt.Store(time.Now().Unix())
	}
}

func (s *Server) othersWatching() bool {
	if s.linkReads.Load() > 0 || time.Since(time.Unix(s.linkReadAt.Load(), 0)) < watchingFor {
		return true
	}
	s.watchingMu.Lock()
	defer s.watchingMu.Unlock()
	for _, v := range s.watching {
		if time.Since(v.at) <= watchingFor {
			return true
		}
	}
	return false
}

func (s *Server) downloadsMoving(ctx context.Context) bool {
	if s.cache == nil {
		return false
	}
	progress, _ := s.cache.Progress(ctx)
	for _, p := range progress {
		if !p.Finished && !p.Paused && p.Mbps > 0 {
			return true
		}
	}
	return false
}
