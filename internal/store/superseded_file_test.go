package store

import (
	"context"
	"testing"
)

func TestSupersededFileReadyWaitsWhileThePackPlays(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	for i, ep := range []string{"2", "3"} {
		if err := s.RecordTorrent(ctx, TorrentRecord{InfoHash: "pack", EngineID: 1, Name: "pack", AnimeID: 1, EpKey: ep,
			FileIndex: i + 2, FilePath: ep + ".mkv", TotalSize: 100}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordTorrent(ctx, TorrentRecord{InfoHash: "single", EngineID: 2, Name: "single", AnimeID: 1, EpKey: "2",
		FilePath: "2.mkv", TotalSize: 100}); err != nil {
		t.Fatal(err)
	}
	file2 := FileRef{InfoHash: "pack", FileIndex: 2}
	ready := func() bool {
		t.Helper()
		ok, err := s.SupersededFileReady(ctx, 1, "2", file2)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}

	if !ready() {
		t.Fatal("a pack file with no bytes is not ready to go")
	}
	s.SetCacheBytes(ctx, "pack", 2, 40, false)
	s.SetCacheBytes(ctx, "single", 0, 10, false)
	if ready() {
		t.Fatal("file 2 went while its replacement is unfinished")
	}
	s.SetCacheBytes(ctx, "single", 0, 100, true)
	if !ready() {
		t.Fatal("a finished replacement does not free file 2")
	}
	if err := s.PinPlaying(ctx, 1, "3", "pack", 3); err != nil {
		t.Fatal(err)
	}
	if ready() {
		t.Fatal("file 2 went while episode 3 of the same pack plays")
	}
	if err := s.UnpinAll(ctx); err != nil {
		t.Fatal(err)
	}
	if !ready() {
		t.Fatal("still held after the pack stopped playing")
	}
}
