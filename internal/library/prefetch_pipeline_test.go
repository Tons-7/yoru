package library

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"yoru/internal/indexer"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

const (
	ep1Hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ep2Hash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	ep2Also = "cccccccccccccccccccccccccccccccccccccccc"
)

func frieren(hash string, episode int) indexer.Torrent {
	return release(hash, "[Best] Sousou no Frieren - 0"+string(rune('0'+episode))+" [1080p].mkv", 900)
}

func pipeline(t *testing.T, engine *fakeEngine, results ...indexer.Torrent) (*Playback, *Prefetcher, *askedIndexer) {
	t.Helper()
	srv := httptest.NewServer(engine.handler())
	t.Cleanup(srv.Close)

	st := prefetchStore(t)
	episodes := 28
	if _, err := st.ImportList(context.Background(),
		[]store.Anime{{ID: 1, Romaji: "Sousou no Frieren", Synonyms: "[]", Genres: "[]", Episodes: &episodes}},
		nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	idx := &askedIndexer{results: results}
	finder := NewFinder(st, idx, discard())
	client := torrent.NewClient(srv.URL)
	pref := NewPrefetcher(st, finder, client, discard())
	play := NewPlayback(st, finder, client, nil, t.TempDir(), discard()).WithPrefetcher(pref)
	return play, pref, idx
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (a *askedIndexer) searchesFor(episode int) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, q := range a.asked {
		if strings.HasSuffix(q, fmt.Sprintf(" %02d", episode)) {
			n++
		}
	}
	return n
}

func torrentsAdded(engine *fakeEngine) int {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return len(engine.added)
}

func fetchInBackground(pref *Prefetcher, keep bool) func() (int, bool, error) {
	var (
		id      int
		started bool
		err     error
		wg      sync.WaitGroup
	)
	wg.Go(func() {
		id, started, err = pref.fetch(context.Background(), 1, 2, 1, score.DefaultPreferences(), keep)
	})
	return func() (int, bool, error) { wg.Wait(); return id, started, err }
}

func TestPlayTakesOverTheEpisodeBeingFetchedAhead(t *testing.T) {
	engine := newFakeEngine()
	engine.slow[ep2Hash] = 1500 * time.Millisecond
	play, pref, idx := pipeline(t, engine, frieren(ep2Hash, 2))
	ctx := context.Background()

	outcome := fetchInBackground(pref, false)
	eventually(t, "the fetch to add its torrent", func() bool { _, ok := pref.Fetching(1, 2); return ok })
	searched := idx.searchesFor(2)

	session, err := play.Start(ctx, PlayRequest{AnimeID: 1, Episode: 2, Season: 1, Prefs: score.DefaultPreferences()})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if session.InfoHash != ep2Hash {
		t.Fatalf("played %s, want the torrent being fetched ahead", session.InfoHash)
	}
	if n := idx.searchesFor(2); n != searched {
		t.Errorf("play searched %d more times beside the fetch", n-searched)
	}
	if n := torrentsAdded(engine); n != 1 {
		t.Errorf("added %d torrents, want the one", n)
	}
	if err := play.store.PinCache(ctx, ep2Hash, 0, true); err != nil {
		t.Fatal(err)
	}

	if _, started, err := outcome(); err != nil || !started {
		t.Fatalf("fetch: started=%v err=%v", started, err)
	}
	if rec, ok, _ := play.store.TorrentForEpisode(ctx, 1, "2"); !ok || rec.InfoHash != ep2Hash {
		t.Fatalf("recorded %+v, want the shared torrent", rec)
	}
	entries, _ := play.store.CacheEntries(ctx)
	if len(entries) != 1 || !entries[0].Pinned {
		t.Fatalf("entries = %+v; the fetch's record unpinned the episode playing", entries)
	}
	if _, ok := pref.Fetching(1, 2); ok {
		t.Error("the fetch is still listed as in flight after it ended")
	}
}

func TestPlayWaitsForAFetchToAddItsTorrent(t *testing.T) {
	_, pref, _ := pipeline(t, newFakeEngine())
	ctx := context.Background()

	flight, ok := pref.beginFetch(1, 2)
	if !ok {
		t.Fatal("could not begin a fetch")
	}
	rec := store.TorrentRecord{InfoHash: ep2Hash, AnimeID: 1, EpKey: "2", Name: "[Best] Sousou no Frieren - 02 [1080p].mkv"}
	go func() {
		time.Sleep(300 * time.Millisecond)
		pref.setTorrent(flight, rec)
	}()

	began := time.Now()
	pref.AwaitPrepare(ctx, 1, 2)
	if waited := time.Since(began); waited < 250*time.Millisecond || waited > 5*time.Second {
		t.Fatalf("waited %v, want until the torrent was added", waited)
	}
	if got, ok := pref.Fetching(1, 2); !ok || got.InfoHash != ep2Hash {
		t.Fatalf("Fetching = %+v, %v", got, ok)
	}

	pref.endFetch(1, 2, flight)
	if _, ok := pref.Fetching(1, 2); ok {
		t.Error("still in flight after the fetch ended")
	}
	began = time.Now()
	pref.AwaitPrepare(ctx, 1, 2)
	if time.Since(began) > time.Second {
		t.Error("waited with nothing in flight")
	}
}

func TestTwoFetchesOfOneEpisodeAddOneTorrent(t *testing.T) {
	engine := newFakeEngine()
	engine.slow[ep2Hash] = 500 * time.Millisecond
	_, pref, idx := pipeline(t, engine, frieren(ep2Hash, 2))

	first := fetchInBackground(pref, false)
	eventually(t, "the first fetch to add its torrent", func() bool { _, ok := pref.Fetching(1, 2); return ok })
	searched := idx.searches()
	second := fetchInBackground(pref, false)

	_, startedA, errA := first()
	_, startedB, errB := second()
	if !(startedA && errA == nil && !startedB && errors.Is(errB, errOvertaken)) {
		t.Fatalf("first: started=%v err=%v; second: started=%v err=%v", startedA, errA, startedB, errB)
	}
	if n := torrentsAdded(engine); n != 1 {
		t.Errorf("added %d torrents, want one", n)
	}
	if n := idx.searches(); n != searched {
		t.Errorf("the second fetch searched %d more times", n-searched)
	}
}

func TestDownloadKeepsTheCopyAPrefetchIsFetching(t *testing.T) {
	engine := newFakeEngine()
	engine.slow[ep2Hash] = 800 * time.Millisecond
	engine.finished[ep2Hash] = true
	play, pref, _ := pipeline(t, engine, frieren(ep2Hash, 2))
	ctx := context.Background()

	outcome := fetchInBackground(pref, false)
	eventually(t, "the fetch to be under way", func() bool { return pref.inFlight(1, 2) != nil })

	if err := pref.Download(ctx, 1, 2, 1, score.DefaultPreferences(), time.Second); err != nil {
		t.Fatalf("download: %v", err)
	}
	if _, started, err := outcome(); err != nil || !started {
		t.Fatalf("prefetch: started=%v err=%v", started, err)
	}
	if n := torrentsAdded(engine); n != 1 {
		t.Errorf("added %d torrents, want the prefetch's alone", n)
	}
	entries, _ := play.store.CacheEntries(ctx)
	if len(entries) != 1 || entries[0].InfoHash != ep2Hash || !entries[0].Kept {
		t.Fatalf("entries = %+v, want the prefetched copy kept", entries)
	}
}

func TestFetchYieldsToAReleasePlayRecordedMeanwhile(t *testing.T) {
	engine := newFakeEngine()
	engine.slow[ep2Hash] = 1000 * time.Millisecond
	play, pref, _ := pipeline(t, engine, frieren(ep2Hash, 2))
	ctx := context.Background()

	outcome := fetchInBackground(pref, false)
	eventually(t, "the fetch to add its torrent", func() bool { _, ok := pref.Fetching(1, 2); return ok })
	if err := play.store.RecordTorrent(ctx, store.TorrentRecord{
		InfoHash: ep2Also, Name: "[Other] Sousou no Frieren - 02 [1080p].mkv", AnimeID: 1, EpKey: "2",
		FilePath: "other.mkv", TotalSize: 100,
	}); err != nil {
		t.Fatal(err)
	}

	if _, started, err := outcome(); started || !errors.Is(err, errOvertaken) {
		t.Fatalf("fetch: started=%v err=%v, want it to yield", started, err)
	}
	if rec, ok, _ := play.store.TorrentForEpisode(ctx, 1, "2"); !ok || rec.InfoHash != ep2Also {
		t.Fatalf("recorded %+v, want play's release left alone", rec)
	}
	if !engine.wasDeleted(ep2Hash) {
		t.Error("the yielded torrent is still in the engine")
	}
	if _, ok := pref.Fetching(1, 2); ok {
		t.Error("the yielded fetch is still listed as in flight")
	}
}

func TestPrepareSkipsAnEpisodeBeingFetched(t *testing.T) {
	_, pref, idx := pipeline(t, newFakeEngine(), frieren(ep2Hash, 2))
	flight, _ := pref.beginFetch(1, 2)
	defer pref.endFetch(1, 2, flight)

	if err := pref.prepare(context.Background(), 1, 2, 1, score.DefaultPreferences()); err != nil {
		t.Fatal(err)
	}
	if n := idx.searches(); n != 0 {
		t.Errorf("searched %d times for an episode being fetched", n)
	}
}

func TestPressingNextResumesTheEpisodeDownloadedAhead(t *testing.T) {
	engine := newFakeEngine()
	play, pref, idx := pipeline(t, engine, frieren(ep1Hash, 1), frieren(ep2Hash, 2))
	ctx := context.Background()
	if err := play.store.SetSetting(ctx, "cache.prefetch_next", "true"); err != nil {
		t.Fatal(err)
	}
	prefs := score.DefaultPreferences()

	session, err := play.Start(ctx, PlayRequest{AnimeID: 1, Episode: 1, Season: 1, Prefs: prefs})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if session.InfoHash != ep1Hash {
		t.Fatalf("played %s for episode 1", session.InfoHash)
	}
	eventually(t, "episode 2 to be downloaded ahead", func() bool {
		rec, ok, _ := play.store.TorrentForEpisode(ctx, 1, "2")
		return ok && rec.InfoHash == ep2Hash
	})
	searched := idx.searchesFor(2)

	pref.PrepareTarget(1, 2, 1, prefs)
	pref.AwaitPrepare(ctx, 1, 2)

	session, err = play.Start(ctx, PlayRequest{AnimeID: 1, Episode: 2, Season: 1, Prefs: prefs})
	if err != nil {
		t.Fatalf("start episode 2: %v", err)
	}
	if session.InfoHash != ep2Hash {
		t.Fatalf("played %s, want the torrent downloaded ahead", session.InfoHash)
	}
	if n := idx.searchesFor(2); n != searched {
		t.Errorf("searched %d more times for an episode already held", n-searched)
	}
	if n := torrentsAdded(engine); n != 2 {
		t.Errorf("added %d torrents, want one per episode", n)
	}
	entries, _ := play.store.CacheEntries(ctx)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want one per episode", entries)
	}
}

func TestAFailedTakeoverHandsTheTorrentBack(t *testing.T) {
	engine := newFakeEngine()
	engine.slow[ep2Hash] = 1500 * time.Millisecond
	play, pref, _ := pipeline(t, engine, frieren(ep2Hash, 2))
	ctx := context.Background()

	outcome := fetchInBackground(pref, false)
	eventually(t, "the fetch to add its torrent", func() bool { _, ok := pref.Fetching(1, 2); return ok })

	prefs := score.DefaultPreferences()
	prefs.Audio = "dub"
	if session, err := play.Start(ctx, PlayRequest{AnimeID: 1, Episode: 2, Season: 1, Prefs: prefs, AudioChosen: true}); err == nil && session.InfoHash == ep2Hash {
		t.Fatal("play took a release its own audio choice rules out")
	}
	if engine.wasDeleted(ep2Hash) {
		t.Fatal("the fetch-ahead torrent was deleted when play let it go")
	}
	if _, started, err := outcome(); err != nil || !started {
		t.Fatalf("the fetch did not finish after play handed it back: started=%v err=%v", started, err)
	}
}
