package store

import (
	"context"
	"testing"
)

func TestAPackSharedByTwoEpisodesCountsBoth(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.EnsureAnime(ctx, 1); err != nil {
		t.Fatal(err)
	}
	const pack, single = "aaaa", "bbbb"
	for i, ep := range []string{"1", "2"} {
		if err := s.RecordTorrent(ctx, TorrentRecord{InfoHash: pack, Name: "Show (01-12)", AnimeID: 1, EpKey: ep,
			FileIndex: i, FilePath: "Show - 0" + ep + ".mkv", TotalSize: 100}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordTorrent(ctx, TorrentRecord{InfoHash: single, Name: "Show - 03", AnimeID: 1, EpKey: "3",
		FilePath: "Show - 03.mkv", TotalSize: 100}); err != nil {
		t.Fatal(err)
	}
	if n := s.TorrentEpisodeCount(ctx, pack); n != 2 {
		t.Errorf("pack counted %d episodes, want 2", n)
	}
	if n := s.TorrentEpisodeCount(ctx, "BBBB"); n != 1 {
		t.Errorf("single counted %d episodes, want 1", n)
	}
}
