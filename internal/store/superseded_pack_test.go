package store

import (
	"context"
	"testing"
)

func TestAPackWaitsForEveryEpisodesReplacement(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	for i, ep := range []string{"11", "12"} {
		if err := s.RecordTorrent(ctx, TorrentRecord{
			InfoHash: "pack", EngineID: 1, Name: "pack", AnimeID: 1, EpKey: ep, FileIndex: i, FilePath: ep + ".mkv", TotalSize: 100,
		}); err != nil {
			t.Fatal(err)
		}
		s.SetCacheBytes(ctx, "pack", i, 100, true)
	}
	for _, ep := range []string{"11", "12"} {
		if err := s.RecordTorrent(ctx, TorrentRecord{
			InfoHash: "new" + ep, EngineID: 2, Name: "new" + ep, AnimeID: 1, EpKey: ep, FilePath: ep + ".mkv", TotalSize: 100,
		}); err != nil {
			t.Fatal(err)
		}
	}
	s.SetCacheBytes(ctx, "new11", 0, 100, true)
	s.SetCacheBytes(ctx, "new12", 0, 5, false)
	if ok, err := s.SupersededReady(ctx, 1, "11", "pack"); err != nil || ok {
		t.Fatalf("the pack would go while episode 12's replacement is at 5%%: %v %v", ok, err)
	}
	s.SetCacheBytes(ctx, "new12", 0, 100, true)
	if ok, err := s.SupersededReady(ctx, 1, "11", "pack"); err != nil || !ok {
		t.Fatalf("every episode is replaced, yet the pack stays: %v %v", ok, err)
	}
}

func TestRemovingAReplacementGivesBackTheFinishedCopy(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	for _, hash := range []string{"old", "new"} {
		if err := s.RecordTorrent(ctx, TorrentRecord{
			InfoHash: hash, EngineID: 1, Name: hash, AnimeID: 1, EpKey: "5", FilePath: hash + ".mkv", TotalSize: 100,
		}); err != nil {
			t.Fatal(err)
		}
		if hash == "old" {
			s.SetCacheBytes(ctx, "old", 0, 100, true)
		}
	}
	if err := s.DropTorrentCache(ctx, "NEW"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.TorrentForEpisode(ctx, 1, "5")
	if err != nil || !ok || got.InfoHash != "old" {
		t.Fatalf("after the replacement went: %+v %v %v, want the finished copy back", got, ok, err)
	}
}
