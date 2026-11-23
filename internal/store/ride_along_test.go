package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func rideAlongQueue(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 100, "FINISHED", 12)
	for i, ep := range []string{"2", "3"} {
		if err := s.RecordTorrent(ctx, TorrentRecord{InfoHash: "pack", EngineID: 1, Name: "pack", AnimeID: 100, EpKey: ep,
			FileIndex: i, FilePath: ep + ".mkv", TotalSize: 100}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Enqueue(ctx, 100, 1, []int{2, 3}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.ClaimQueued(ctx, 100, "2"); err != nil || !ok {
		t.Fatalf("claim 2: %v %v", ok, err)
	}
	return s, ctx
}

func TestNextFromTorrentsWaitsOutARetry(t *testing.T) {
	s, ctx := rideAlongQueue(t)
	if _, ok, err := s.ClaimQueued(ctx, 100, "3"); err != nil || !ok {
		t.Fatalf("claim 3: %v %v", ok, err)
	}
	if err := s.Defer(ctx, 100, "3", errors.New("slow"), time.Now().Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if next, ok, _ := s.NextFromTorrents(ctx, []string{"PACK"}); ok {
		t.Fatalf("episode %d rode along before its retry time", next.Episode)
	}
	if _, err := s.w.Exec(`UPDATE download_queue SET retry_at = ? WHERE anime_id = 100 AND ep_key = '3'`,
		time.Now().Add(-time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if next, ok, err := s.NextFromTorrents(ctx, []string{"PACK"}); err != nil || !ok || next.Episode != 3 {
		t.Fatalf("after the retry time: %+v %v %v, want episode 3", next, ok, err)
	}
}

func TestNextFromTorrentsKeepsTheChosenRelease(t *testing.T) {
	s, ctx := rideAlongQueue(t)
	if _, err := s.SetQueuedRelease(ctx, 100, "3", "other", "other release"); err != nil {
		t.Fatal(err)
	}
	if next, ok, _ := s.NextFromTorrents(ctx, []string{"pack"}); ok {
		t.Fatalf("episode %d rode along on the pack though another release was chosen", next.Episode)
	}
}
