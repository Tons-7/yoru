package store

import (
	"context"
	"testing"
)

func TestDerivedEpisodesDropNumbersPastTheEntry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 213657, "RELEASING", 6)

	if _, err := s.SaveDerivedEpisodes(ctx, 213657, []int{1, 2, 3, 14}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDerivedEpisodes(ctx, 213657, []int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	eps, err := s.Episodes(ctx, 213657)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 6 {
		t.Fatalf("got %d episodes, want 1-6", len(eps))
	}
	for i, e := range eps {
		if e.Number != i+1 {
			t.Errorf("episode %d is numbered %d", i+1, e.Number)
		}
	}
}

func TestDerivedEpisodesPruneEvenWhenNothingIsFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 500, "RELEASING", 6)

	if _, err := s.SaveDerivedEpisodes(ctx, 500, []int{1, 14}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDerivedEpisodes(ctx, 500, nil); err != nil {
		t.Fatal(err)
	}
	eps, err := s.Episodes(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range eps {
		if e.Number > 6 {
			t.Errorf("episode %d is past the entry's 6", e.Number)
		}
	}
}

func TestDerivedPruneKeepsHeldLocalAndWatchedEpisodes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 213657, "RELEASING", 6)
	if _, err := s.SaveDerivedEpisodes(ctx, 213657, []int{1, 14, 15, 16, 17}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordTorrent(ctx, TorrentRecord{InfoHash: "held", EngineID: 1, Name: "held", AnimeID: 213657, EpKey: "14",
		FilePath: "14.mkv", TotalSize: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveLocalFiles(ctx, 213657, []LocalFile{{Path: t.TempDir() + "/15.mkv", Size: 10, AnimeID: 213657,
		Episode: 15, Season: 1, Confidence: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`INSERT INTO playback (anime_id, ep_key, position_s, watched, last_played_at)
		VALUES (213657, '16', 1400, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDerivedEpisodes(ctx, 213657, []int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	eps, err := s.Episodes(ctx, 213657)
	if err != nil {
		t.Fatal(err)
	}
	have := map[int]bool{}
	for _, e := range eps {
		have[e.Number] = true
	}
	for n, why := range map[int]string{14: "a held download", 15: "a file on disk", 16: "watch history"} {
		if !have[n] {
			t.Errorf("episode %d went though it has %s", n, why)
		}
	}
	if have[17] {
		t.Error("episode 17, with nothing behind it, was kept past the entry's end")
	}
}
