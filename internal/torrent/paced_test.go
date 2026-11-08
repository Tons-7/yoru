package torrent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func progressing(t *testing.T, perSecond int64, total int64) *Client {
	t.Helper()
	start := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := min(total, int64(time.Since(start).Seconds()*float64(perSecond)))
		json.NewEncoder(w).Encode(map[string]any{
			"state": "live", "progress_bytes": got, "total_bytes": total, "finished": got >= total,
		})
	}))
	t.Cleanup(srv.Close)
	old := AwaitPoll
	AwaitPoll = 20 * time.Millisecond
	t.Cleanup(func() { AwaitPoll = old })
	return NewClient(srv.URL)
}

func TestACrawlingDownloadIsReportedSlow(t *testing.T) {
	c := progressing(t, 10<<10, 1<<30)
	err := c.AwaitPaced(context.Background(), 1, time.Minute, Pace{MinRate: 100 << 10, Window: 200 * time.Millisecond})
	var slow *SlowError
	if !errors.As(err, &slow) {
		t.Fatalf("got %v, want a slow download reported", err)
	}
	if slow.Rate > 50<<10 {
		t.Errorf("rate %.0f B/s, want about 10 KB/s", slow.Rate)
	}
}

func TestADownloadThatKeepsUpIsLeftToFinish(t *testing.T) {
	c := progressing(t, 50<<20, 10<<20)
	if err := c.AwaitPaced(context.Background(), 1, time.Minute, Pace{MinRate: 100 << 10, Window: 100 * time.Millisecond}); err != nil {
		t.Fatalf("a fast download was cut short: %v", err)
	}
}

func TestNoPaceMeansNoSlowError(t *testing.T) {
	c := progressing(t, 10<<10, 4<<10)
	if err := c.AwaitPaced(context.Background(), 1, time.Minute, Pace{}); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestAnEpisodeIsDoneWhenItsOwnFileIs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"state": "live", "progress_bytes": 150, "total_bytes": 300, "finished": false,
			"file_progress": []int64{100, 50, 0}, "file_included": []bool{true, true, false},
		})
	}))
	t.Cleanup(srv.Close)
	old := AwaitPoll
	AwaitPoll = 10 * time.Millisecond
	t.Cleanup(func() { AwaitPoll = old })
	c := NewClient(srv.URL)

	if err := c.AwaitPaced(context.Background(), 1, time.Minute, Pace{File: 0, FileSize: 100}); err != nil {
		t.Fatalf("a whole file waited on the rest of its torrent: %v", err)
	}
	err := c.AwaitPaced(context.Background(), 1, 50*time.Millisecond, Pace{File: 1, FileSize: 100})
	var stall *StallError
	if !errors.As(err, &stall) || stall.Percent != 50 {
		t.Fatalf("got %v, want the half-done file to stall at 50%%", err)
	}
}
