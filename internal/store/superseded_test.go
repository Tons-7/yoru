package store

import (
	"context"
	"testing"
)

// A replaced release goes when it can cost nothing: never while playing, and a finished one only once
// the episode's new release is finished too.
func TestSupersededReadyWaitsForTheReplacement(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	for _, hash := range []string{"old", "new"} {
		if err := s.RecordTorrent(ctx, TorrentRecord{
			InfoHash: hash, EngineID: 1, Name: hash, AnimeID: 1, EpKey: "5", FilePath: hash + ".mkv", TotalSize: 100,
		}); err != nil {
			t.Fatal(err)
		}
	}
	ready := func() bool {
		t.Helper()
		ok, err := s.SupersededReady(ctx, 1, "5", "old")
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !ready() {
		t.Error("an unfinished replaced release is worth nothing to keep, yet it waits")
	}
	// Kept by hand: with nothing on disk it costs nothing to drop; with data it waits as a finished one does.
	if _, err := s.KeepDownload(ctx, "old", true); err != nil {
		t.Fatal(err)
	}
	if !ready() {
		t.Error("a kept copy with no data waits for its replacement")
	}
	s.SetCacheBytes(ctx, "old", 0, 50, false)
	if ready() {
		t.Error("a kept copy would go while its replacement has nothing")
	}
	if _, err := s.KeepDownload(ctx, "old", false); err != nil {
		t.Fatal(err)
	}
	s.SetCacheBytes(ctx, "old", 0, 100, true)
	if ready() {
		t.Error("a finished copy would go while its replacement has nothing")
	}
	s.SetCacheBytes(ctx, "new", 0, 100, true)
	if !ready() {
		t.Error("the replacement is whole, yet the old copy stays")
	}
	s.PinCache(ctx, "old", 0, true)
	if ready() {
		t.Error("a copy playing right now would be deleted")
	}
	if eps, err := s.SupersededEpisodes(ctx); err != nil || len(eps) != 1 || eps[0] != (EpisodeRef{1, "5"}) {
		t.Errorf("superseded episodes = %v, %v", eps, err)
	}
}
