package library

import (
	"context"
	"errors"
	"slices"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/store"
)

// Near an episode's end the next one's torrent is started and read at both ends; held paused by download
// ahead or not held at all, Auto next decides whether it's fetched now.
func TestReadyNextGetsTheNextEpisodesStart(t *testing.T) {
	ctx := context.Background()

	withPrefetch := func(p *Playback) *Playback {
		return p.WithPrefetcher(NewPrefetcher(p.store, p.finder, p.torrent, discard()))
	}
	engine := newFakeEngine()
	p := withPrefetch(newPlayback(t, engine, nil))
	engine.ids[7] = goodHash
	engine.paused[7] = true
	if err := p.store.RecordTorrent(ctx, store.TorrentRecord{
		InfoHash: goodHash, EngineID: 7, Name: "[Best] Sousou no Frieren - 02 [1080p].mkv",
		AnimeID: 1, EpKey: "2", FilePath: "Sousou no Frieren - 02.mkv", TotalSize: 1 << 30,
	}); err != nil {
		t.Fatal(err)
	}
	next, err := p.ReadyNext(ctx, 1, 1)
	if err != nil || next != (store.Pick{AnimeID: 1, Episode: 2}) {
		t.Fatalf("ready next = %+v, %v; want episode 2", next, err)
	}
	if engine.isPaused(7) {
		t.Error("the held download of episode 2 was left paused")
	}

	// Nothing held: only Auto next, which will play it, has it fetched now.
	engine = newFakeEngine()
	p = withPrefetch(newPlayback(t, engine, []indexer.Torrent{release(otherHash, "[Best] Sousou no Frieren - 02 [1080p].mkv", 80)}))
	if _, err := p.ReadyNext(ctx, 1, 1); !errors.Is(err, errNotReadied) || len(engine.added) != 0 {
		t.Fatalf("with Auto next off: err %v, added %v; want nothing fetched", err, engine.added)
	}
	if err := p.store.SetSetting(ctx, "playback.autonext", "true"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReadyNext(ctx, 1, 1); err != nil {
		t.Fatalf("with Auto next on: %v", err)
	}
	if !slices.Contains(engine.added, otherHash) {
		t.Errorf("episode 2 wasn't fetched: added %v", engine.added)
	}
}
