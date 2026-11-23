package library

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"testing"

	"yoru/internal/store"
	"yoru/internal/torrent"
)

func TestStoppingLeavesAPackAnotherEpisodeDownloads(t *testing.T) {
	engine := newFakeEngine()
	srv := httptest.NewServer(engine.handler())
	t.Cleanup(srv.Close)
	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	engine.ids[1] = hash
	st := prefetchStore(t)
	p := NewPrefetcher(st, nil, torrent.NewClient(srv.URL), slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	release := p.claimWhile(hash)
	other := p.claimWhile(hash)
	p.stop(ctx, 1, -1, true, "did not start", errors.New("x"))
	if engine.isPaused(1) {
		t.Fatal("stopped a pack another episode still downloads from")
	}
	other()
	p.stop(ctx, 1, -1, true, "did not start", errors.New("x"))
	if !engine.isPaused(1) {
		t.Fatal("the pack kept running once nothing else wanted it")
	}
	release()
}

func TestDownloadNowKeepsTheRestOfAPausedPackPaused(t *testing.T) {
	engine := newFakeEngine()
	srv := httptest.NewServer(engine.handler())
	t.Cleanup(srv.Close)
	const hash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	engine.ids[5] = hash
	st := prefetchStore(t)
	ctx := context.Background()
	for i, ep := range []string{"2", "3"} {
		if err := st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: hash, EngineID: 5, Name: "pack", AnimeID: 1, EpKey: ep,
			FileIndex: i + 2, FilePath: ep + ".mkv", TotalSize: 100}); err != nil {
			t.Fatal(err)
		}
	}
	p := NewPrefetcher(st, nil, torrent.NewClient(srv.URL), slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.SetUserPaused(hash, true)

	p.narrowPause(ctx, hash, 2)

	if p.pausedByUser(FileKey(hash, 2)) {
		t.Error("the episode asked for now is still held paused")
	}
	if !p.pausedByUser(FileKey(hash, 3)) {
		t.Error("the rest of the pack is no longer paused")
	}
	keys, _ := st.HandPaused(ctx)
	if !slices.Contains(keys, FileKey(hash, 3)) || slices.Contains(keys, FileKey(hash, 2)) {
		t.Errorf("hand-paused keys = %v, want only file 3", keys)
	}
}
