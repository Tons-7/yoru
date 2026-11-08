package library

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"yoru/internal/db"
	"yoru/internal/score"
	"yoru/internal/store"
)

func prefetchStore(t *testing.T) *store.Store {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.Migrate(); err != nil {
		t.Fatal(err)
	}
	return store.New(conn)
}

func TestPrepareIsOnByDefaultAndNotTheDownloadSwitch(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	p := NewPrefetcher(st, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if !p.prepareWanted() {
		t.Error("resolving the next episode should be on out of the box")
	}
	if n, _ := p.ahead(); n != 0 {
		t.Error("downloading the next episode should stay opt-in")
	}

	if err := st.SetSetting(ctx, "cache.prefetch_next", "false"); err != nil {
		t.Fatal(err)
	}
	if !p.prepareWanted() {
		t.Error("the download switch still governs the resolve")
	}

	if err := st.SetSetting(ctx, "playback.prepare_next", "false"); err != nil {
		t.Fatal(err)
	}
	if p.prepareWanted() {
		t.Error("its own switch does not turn it off")
	}
}

func TestDownloadAheadCount(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	p := NewPrefetcher(st, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := st.SetSetting(ctx, "cache.prefetch_next", "true"); err != nil {
		t.Fatal(err)
	}
	if n, sequels := p.ahead(); n != 1 || sequels {
		t.Errorf("on with no count = %d (sequels %v), want the next one within the season", n, sequels)
	}
	for set, want := range map[string]int{"2": 2, "5": 2, "0": 1} {
		if err := st.SetSetting(ctx, "cache.prefetch_count", set); err != nil {
			t.Fatal(err)
		}
		if n, _ := p.ahead(); n != want {
			t.Errorf("count %s = %d, want %d", set, n, want)
		}
	}
	if err := st.SetSetting(ctx, "cache.prefetch_sequel", "true"); err != nil {
		t.Fatal(err)
	}
	if _, sequels := p.ahead(); !sequels {
		t.Error("its switch does not let it carry on into the next season")
	}
	if err := st.SetSetting(ctx, "cache.prefetch_next", "false"); err != nil {
		t.Fatal(err)
	}
	if n, _ := p.ahead(); n != 0 {
		t.Errorf("off = %d", n)
	}
}

func TestDownloadAheadStopsAtTheFinale(t *testing.T) {
	pick := func(anime, ep int) store.Pick { return store.Pick{AnimeID: anime, Episode: ep} }
	cases := []struct {
		name     string
		picks    []store.Pick
		fromList int
		want     int
	}{
		{"mid-season", []store.Pick{pick(1, 6), pick(1, 7)}, 0, 2},
		{"the finale's next is the sequel", []store.Pick{pick(2, 1)}, 0, 0},
		{"the last but one stops before the sequel", []store.Pick{pick(1, 12), pick(2, 1)}, 0, 1},
		{"Up next is the viewer's own choice", []store.Pick{pick(7, 3)}, 1, 1},
		{"after Up next, its show's own finale", []store.Pick{pick(7, 3), pick(7, 4), pick(8, 1)}, 1, 2},
	}
	for _, c := range cases {
		if got := withinShow(c.picks, c.fromList, 1); len(got) != c.want {
			t.Errorf("%s: kept %v, want %d", c.name, got, c.want)
		}
	}
}

func TestPreparedReleaseOutlastsAnEpisode(t *testing.T) {
	p := NewPrefetcher(prefetchStore(t), nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.storePrepared(5, 2, score.Result{})
	p.mu.Lock()
	entry := p.prepared[prepareKey(5, 2)]
	entry.at = time.Now().Add(-30 * time.Minute)
	p.prepared[prepareKey(5, 2)] = entry
	p.mu.Unlock()

	if _, ok := p.TakePrepared(5, 2); !ok {
		t.Fatal("a release resolved half an hour ago was discarded before the episode ended")
	}
	if _, ok := p.TakePrepared(5, 2); ok {
		t.Fatal("taking it must forget it")
	}
}

func TestPrepareSkipsAnEpisodeAlreadyInTheLibrary(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	if err := st.EnsureAnime(ctx, 42); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "Show - 07.mkv")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveLocalFiles(ctx, time.Now().Unix(), []store.LocalFile{{
		Path: path, Size: 1, AnimeID: 42, Episode: 7, Confidence: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	if f, err := st.LocalEpisode(ctx, 42, 7); err != nil || f.ID == 0 {
		t.Fatalf("the library file was not recorded: %+v %v", f, err)
	}

	p := NewPrefetcher(st, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := p.prepare(ctx, 42, 7, 1, score.DefaultPreferences()); err != nil {
		t.Errorf("preparing an episode already on disk should be a no-op, got %v", err)
	}
}

func TestAwaitPrepareWaitsForAResolveInFlight(t *testing.T) {
	p := NewPrefetcher(prefetchStore(t), nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	done := make(chan struct{})
	p.mu.Lock()
	p.running[prepareKey(7, 3)] = done
	p.mu.Unlock()

	var waited sync.WaitGroup
	waited.Add(1)
	released := make(chan time.Time, 1)
	go func() {
		defer waited.Done()
		p.AwaitPrepare(context.Background(), 7, 3)
		released <- time.Now()
	}()

	select {
	case <-released:
		t.Fatal("returned while the resolve was still running")
	case <-time.After(150 * time.Millisecond):
	}

	closed := time.Now()
	close(done)
	waited.Wait()

	if at := <-released; at.Before(closed) {
		t.Error("released before the resolve finished")
	}

	start := time.Now()
	p.AwaitPrepare(context.Background(), 7, 4)
	if time.Since(start) > time.Second {
		t.Error("waited on an episode nothing was preparing")
	}
}

func TestAwaitPrepareGivesUpWithTheRequest(t *testing.T) {
	p := NewPrefetcher(prefetchStore(t), nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	p.mu.Lock()
	p.running[prepareKey(1, 1)] = make(chan struct{})
	p.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	p.AwaitPrepare(ctx, 1, 1)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("held for %s after the request was cancelled", elapsed)
	}
}

func TestPrepareBacksOffAfterAFailure(t *testing.T) {
	st := prefetchStore(t)
	p := NewPrefetcher(st, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	prefs := score.DefaultPreferences()

	p.mu.Lock()
	p.attempted[prepareKey(5, 2)] = time.Now()
	p.mu.Unlock()

	p.PrepareTarget(5, 2, 1, prefs)
	p.mu.Lock()
	_, running := p.running[prepareKey(5, 2)]
	p.mu.Unlock()
	if running {
		t.Error("retried a failed resolve immediately")
	}

	p.mu.Lock()
	p.attempted[prepareKey(5, 2)] = time.Now().Add(-prepareRetry - time.Minute)
	stale := time.Since(p.attempted[prepareKey(5, 2)]) >= prepareRetry
	p.mu.Unlock()
	if !stale {
		t.Error("the pause never expires")
	}
}
