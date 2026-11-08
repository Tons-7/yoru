package torrent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func slowEngine(t *testing.T, delay time.Duration) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"details":{"name":"x","files":[]}}`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL)
	c.http = &http.Client{Timeout: 50 * time.Millisecond}
	return c
}

func TestAFileListOutlastsTheShortCutoffOtherCallsKeep(t *testing.T) {
	c := slowEngine(t, 200*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.listFiles(ctx, "magnet:?xt=urn:btih:abc"); err != nil {
		t.Fatalf("the file list was cut at the short timeout: %v", err)
	}
	if _, err := c.add(ctx, c.http, "magnet:?xt=urn:btih:abc", nil); err == nil {
		t.Error("an ordinary engine call lost its short timeout")
	}
}

func TestAFileListThatNeverComesSaysSo(t *testing.T) {
	c := slowEngine(t, 400*time.Millisecond)
	parent := context.Background()
	ctx, cancel := context.WithTimeout(parent, 100*time.Millisecond)
	defer cancel()
	_, err := c.listFiles(ctx, "magnet:?xt=urn:btih:abc")
	if got := timedOut(ctx, parent, err); !errors.Is(got, ErrNoFileList) {
		t.Fatalf("got %v, want the file list named as what never came", got)
	}

	gone, stop := context.WithCancel(parent)
	stop()
	if got := timedOut(gone, gone, context.Canceled); errors.Is(got, ErrNoFileList) {
		t.Error("a cancelled request was reported as a missing file list")
	}
}

func TestARememberedFailureKeepsItsOwnReason(t *testing.T) {
	c, _ := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad magnet", http.StatusBadRequest)
	})
	const magnet = "magnet:?xt=urn:btih:broken"
	if _, err := c.Inspect(context.Background(), magnet); err == nil {
		t.Fatal("expected the engine's refusal")
	}
	_, err := c.Inspect(context.Background(), magnet)
	if err == nil || errors.Is(err, ErrNoFileList) || !strings.Contains(err.Error(), "bad magnet") {
		t.Errorf("remembered as %v; want the engine's own reason, not a missing file list", err)
	}
}
