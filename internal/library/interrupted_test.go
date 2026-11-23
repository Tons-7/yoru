package library

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"yoru/internal/indexer"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

func TestInterruptedDownloadIsStillTheEpisodes(t *testing.T) {
	engine := newFakeEngine()
	engine.stuck[goodHash] = true
	srv := httptest.NewServer(engine.handler())
	t.Cleanup(srv.Close)

	st := prefetchStore(t)
	ctx := context.Background()
	episodes, english := 28, "Sousou no Frieren"
	if _, err := st.ImportList(ctx,
		[]store.Anime{{ID: 1, Romaji: "Frieren", English: &english, Synonyms: "[]", Genres: "[]", Episodes: &episodes}},
		nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	finder := NewFinder(st, fixedIndexer{results: []indexer.Torrent{
		release(goodHash, "[Best] Sousou no Frieren - 01 [1080p].mkv", 900),
	}}, discard())
	p := NewPrefetcher(st, finder, torrent.NewClient(srv.URL), discard())

	stop, cancel := context.WithCancel(ctx)
	go func() { time.Sleep(400 * time.Millisecond); cancel() }()
	err := p.Download(stop, 1, 1, 1, score.DefaultPreferences(), time.Minute)
	if err == nil {
		t.Fatal("a download whose first bytes never came finished")
	}
	if stop.Err() == nil {
		t.Fatalf("the download gave up on its own: %v", err)
	}

	rec, ok, err := st.TorrentForEpisode(ctx, 1, "1")
	if err != nil || !ok || rec.InfoHash != goodHash {
		t.Fatalf("the interrupted download is not on record for episode 1: ok=%v rec=%+v err=%v", ok, rec, err)
	}
	if _, held := p.held(ctx, 1, 1); !held {
		t.Error("the retry would search again instead of resuming the torrent it holds")
	}
}

func TestYieldOnlyForAnEpisodeStillArriving(t *testing.T) {
	engine := newFakeEngine()
	p := newPlayback(t, engine, nil)
	ctx := context.Background()
	if err := p.store.EnsureAnime(ctx, 2); err != nil {
		t.Fatal(err)
	}
	engine.ids[5] = deadHash
	if err := p.store.RecordTorrent(ctx, store.TorrentRecord{
		InfoHash: deadHash, EngineID: 5, Name: "other", AnimeID: 2, EpKey: "1", FilePath: "other.mkv", TotalSize: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.store.PinCache(ctx, deadHash, 0, false); err != nil {
		t.Fatal(err)
	}
	engine.ids[6] = goodHash
	if err := p.store.RecordTorrent(ctx, store.TorrentRecord{
		InfoHash: goodHash, EngineID: 6, Name: "mine", AnimeID: 1, EpKey: "1", FilePath: "mine.mkv", TotalSize: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.store.SetCacheBytes(ctx, goodHash, 0, 100, true); err != nil {
		t.Fatal(err)
	}

	p.yield(ctx, 1, 1)
	if engine.isPaused(5) {
		t.Fatal("playing an episode already on disk paused another show's download")
	}

	p.yield(ctx, 1, 2)
	if !engine.isPaused(5) {
		t.Fatal("an episode still to arrive did not get the line")
	}

	engine.paused[5] = false
	if err := p.store.SetSetting(ctx, "downloads.while_watching", "continue"); err != nil {
		t.Fatal(err)
	}
	p.yield(ctx, 1, 2)
	if engine.isPaused(5) {
		t.Fatal("downloads paused under \"never pause\"")
	}
}
