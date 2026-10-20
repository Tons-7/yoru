package store

import (
	"context"
	"testing"
)

// Pinning a release clears its episode's other releases (one swapped in mid-episode) but no other episode's.
func TestPinPlayingClearsOnlyItsEpisode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	for _, r := range []struct{ hash, ep string }{{"old", "5"}, {"new", "5"}, {"other", "6"}} {
		if err := s.RecordTorrent(ctx, TorrentRecord{
			InfoHash: r.hash, EngineID: 1, Name: r.hash, AnimeID: 1, EpKey: r.ep, FilePath: r.hash + ".mkv", TotalSize: 100,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, hash := range []string{"old", "other"} {
		if err := s.PinCache(ctx, hash, 0, true); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.PinPlaying(ctx, 1, "5", "new", 0); err != nil {
		t.Fatal(err)
	}
	pinned := map[string]bool{}
	entries, err := s.CacheEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		pinned[e.InfoHash] = e.Pinned
	}
	if pinned["old"] || !pinned["new"] || !pinned["other"] {
		t.Errorf("pins = %v; want the replaced release cleared, the new one and the other episode's kept", pinned)
	}
}
