package library

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"yoru/internal/db"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// checkingEngine answers in memory, a torrent whose check never ends; synctest's clock moves only while
// every goroutine waits on it, which a real socket never counts as.
func checkingEngine(polls *atomic.Int32) http.RoundTripper {
	return roundTrip(func(r *http.Request) (*http.Response, error) {
		body := "{}"
		switch {
		case r.URL.Path == "/torrents":
			body = `{"torrents":[{"id":1,"info_hash":"AAAA","name":"a"}]}`
		case strings.HasSuffix(r.URL.Path, "/stats/v1"):
			polls.Add(1)
			body = `{"state":"initializing","finished":false,"progress_bytes":1,"total_bytes":10000}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})
}

func quietDownloader(t *testing.T, engine http.RoundTripper) *Downloader {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.Migrate()
	st := store.New(conn)
	tc := torrent.NewClient("http://engine.test").WithTransport(engine)
	return NewDownloader(st, NewPrefetcher(st, nil, tc, discard()), nil, discard())
}

// A torrent whose check never ends must not keep the startup pass polling for good: the first pass, then one
// a recheck apart until the deadline, then none.
func TestQuietGivesUpAfterTheDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var polls atomic.Int32
		wasEvery, wasFor := quietRecheck, quietFor
		quietRecheck, quietFor = 10*time.Millisecond, 60*time.Millisecond
		t.Cleanup(func() { quietRecheck, quietFor = wasEvery, wasFor })

		quietDownloader(t, checkingEngine(&polls)).Quiet(context.Background())
		time.Sleep(time.Second)
		synctest.Wait()
		if n := polls.Load(); n != 1+6 {
			t.Fatalf("%d passes, want the first and one every 10ms for 60ms", n)
		}
	})
}

func TestQuietStopsWhenCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var polls atomic.Int32
		was := quietRecheck
		quietRecheck = 10 * time.Millisecond
		t.Cleanup(func() { quietRecheck = was })

		ctx, cancel := context.WithCancel(context.Background())
		quietDownloader(t, checkingEngine(&polls)).Quiet(ctx)
		cancel()
		time.Sleep(time.Second)
		synctest.Wait()
		if n := polls.Load(); n != 1 {
			t.Fatalf("%d passes, want only the first: it was cancelled before any recheck", n)
		}
	})
}
