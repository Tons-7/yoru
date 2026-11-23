package store

import (
	"context"
	"testing"
)

func TestAnUnfinishedReplacementDoesNotHideTheFinishedCopy(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	if err := s.RecordTorrent(ctx, TorrentRecord{
		InfoHash: "old", EngineID: 1, Name: "old", AnimeID: 1, EpKey: "5", FilePath: "5.mkv", TotalSize: 100,
	}); err != nil {
		t.Fatal(err)
	}
	s.SetCacheBytes(ctx, "old", 0, 100, true)
	if err := s.RecordTorrent(ctx, TorrentRecord{
		InfoHash: "new", EngineID: 2, Name: "new", AnimeID: 1, EpKey: "5", FilePath: "5.mkv", TotalSize: 100, Manual: true,
	}); err != nil {
		t.Fatal(err)
	}
	s.SetCacheBytes(ctx, "new", 0, 10, false)

	got, ok, err := s.PlayableForEpisode(ctx, 1, "5")
	if err != nil || !ok || got.InfoHash != "old" {
		t.Fatalf("plays %q (%v %v), want the finished old copy while the replacement is at 10%%", got.InfoHash, ok, err)
	}

	s.SetCacheBytes(ctx, "new", 0, 100, true)
	got, _, _ = s.PlayableForEpisode(ctx, 1, "5")
	if got.InfoHash != "new" {
		t.Fatalf("plays %q, want the replacement once it is whole", got.InfoHash)
	}
}
