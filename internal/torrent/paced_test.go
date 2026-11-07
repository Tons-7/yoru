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
	err := c.AwaitPaced(context.Background(), 1, time.Minute, 100<<10, 200*time.Millisecond, 0)
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
	if err := c.AwaitPaced(context.Background(), 1, time.Minute, 100<<10, 100*time.Millisecond, 0); err != nil {
		t.Fatalf("a fast download was cut short: %v", err)
	}
}

func TestNoPaceMeansNoSlowError(t *testing.T) {
	c := progressing(t, 10<<10, 4<<10)
	if err := c.AwaitPaced(context.Background(), 1, time.Minute, 0, 0, 0); err != nil {
		t.Fatalf("got %v", err)
	}
}
