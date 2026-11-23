package store

import (
	"context"
	"testing"
)

func TestAFailedLookupCountsAsHeld(t *testing.T) {
	s := newTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	const hash = "1111111111111111111111111111111111111111"
	if !s.TorrentClaimed(ctx, hash) {
		t.Error("a failed lookup let a torrent count as unclaimed")
	}
	if !s.TorrentFinishedOrKept(ctx, hash) {
		t.Error("a failed lookup let a torrent count as unfinished")
	}
	if s.TorrentClaimed(context.Background(), hash) || s.TorrentFinishedOrKept(context.Background(), hash) {
		t.Error("an unknown torrent counts as held")
	}
}
