package store

import (
	"context"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"yoru/internal/db"
)

func TestDownloadStatusUnderConcurrency(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.Migrate(); err != nil {
		t.Fatal(err)
	}
	s := New(conn)

	if err := s.RecordTorrent(context.Background(), TorrentRecord{
		InfoHash: "abc", Name: "Show", FileIndex: 1, TotalSize: 1 << 30,
	}); err != nil {
		t.Fatal(err)
	}

	callers := max(4, runtime.NumCPU()) * 4
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Go(func() {
			if _, err := s.DownloadStatus(ctx); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)

	var failed int
	for err := range errs {
		if failed == 0 {
			t.Errorf("first failure: %v", err)
		}
		failed++
	}
	if failed > 0 {
		t.Fatalf("%d of %d concurrent calls failed", failed, callers)
	}
}
