package store

import (
	"context"
	"testing"
)

func TestTorrentClaimedIgnoresABareAdoptedRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	if err := s.TrackTorrent(ctx, "ABCD", 1, "adopted"); err != nil {
		t.Fatal(err)
	}
	if s.TorrentClaimed(ctx, "abcd") {
		t.Fatal("an adopted torrent with no episode counts as claimed")
	}
	if err := s.RecordTorrent(ctx, TorrentRecord{InfoHash: "ABCD", EngineID: 1, Name: "adopted", AnimeID: 1, EpKey: "1", FilePath: "1.mkv", TotalSize: 10}); err != nil {
		t.Fatal(err)
	}
	if !s.TorrentClaimed(ctx, "abcd") {
		t.Fatal("a torrent recorded for an episode is not claimed (hash matched case-insensitively)")
	}
}

func TestTorrentFinishedOrKept(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.TrackTorrent(ctx, "done", 1, "done"); err != nil {
		t.Fatal(err)
	}
	if err := s.TrackTorrent(ctx, "kept", 2, "kept"); err != nil {
		t.Fatal(err)
	}
	if s.TorrentFinishedOrKept(ctx, "done") || s.TorrentFinishedOrKept(ctx, "kept") {
		t.Fatal("a fresh adopted row reads as finished or kept")
	}
	if err := s.SetCacheBytes(ctx, "done", WholeTorrent, 10, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.KeepDownload(ctx, "kept", true); err != nil {
		t.Fatal(err)
	}
	if !s.TorrentFinishedOrKept(ctx, "DONE") {
		t.Error("a complete adopted row is not finished")
	}
	if !s.TorrentFinishedOrKept(ctx, "kept") {
		t.Error("a kept adopted row is not kept")
	}
}
