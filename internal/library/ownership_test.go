package library

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"yoru/internal/db"
	"yoru/internal/indexer"
	"yoru/internal/parse"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

func ownedFixture(t *testing.T) (*Cache, *store.Store, *fakeEngine) {
	t.Helper()
	engine := newFakeEngine()
	srv := httptest.NewServer(engine.handler())
	t.Cleanup(srv.Close)

	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.Migrate(); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	c := NewCache(st, torrent.NewClient(srv.URL), t.TempDir(), discard())
	engine.folder = c.dir
	return c, st, engine
}

// Another program's downloads in a shared engine must not be adopted and swept.
func TestAdoptSkipsDownloadsOutsideTheCache(t *testing.T) {
	c, st, engine := ownedFixture(t)
	engine.ids[1] = "foreign"
	engine.folder = t.TempDir()

	if err := c.adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := remaining(t, st); n != 0 {
		t.Fatalf("adopted %d download(s) from another folder", n)
	}

	engine.folder = c.dir
	if err := c.adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := remaining(t, st); n != 1 {
		t.Fatalf("our own untracked download was not adopted: %d", n)
	}
}

func TestEvictNeverDeletesOutsideTheCache(t *testing.T) {
	c, st, engine := ownedFixture(t)
	record(t, st, "abc", "abc", 0)
	engine.ids[1] = "abc"
	engine.folder = t.TempDir()

	if err := c.evict(context.Background(), store.CacheEntry{InfoHash: "abc"}); err != nil {
		t.Fatal(err)
	}
	if engine.wasDeleted("abc") {
		t.Error("deleted a download outside Yoru's cache folder")
	}
}

// Ids are per engine session: the stored one can name another torrent by now.
func TestEvictFindsTheTorrentByHash(t *testing.T) {
	c, st, engine := ownedFixture(t)
	record(t, st, "abc", "abc", 0) // recorded as id 7
	engine.ids[3] = "abc"
	engine.ids[7] = "other"

	if err := c.evict(context.Background(), store.CacheEntry{InfoHash: "abc"}); err != nil {
		t.Fatal(err)
	}
	if !engine.wasDeleted("abc") || engine.wasDeleted("other") {
		t.Errorf("deleted %v; want abc only", engine.deleted)
	}
}

func TestOrphansKeepsEverythingOnRecord(t *testing.T) {
	c, st, engine := ownedFixture(t)
	was := orphanSettle
	orphanSettle = 0
	t.Cleanup(func() { orphanSettle = was })

	record(t, st, "rec", "recorded.mkv", 0)
	engine.ids[1] = "live.mkv"
	for _, name := range []string{"recorded.mkv", "live.mkv", "stray.mkv"} {
		if err := os.WriteFile(filepath.Join(c.dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(c.dir, EngineSessionDirName), 0o755); err != nil {
		t.Fatal(err)
	}

	preview, err := c.Orphans(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 1 || preview[0].Name != "stray.mkv" {
		t.Fatalf("preview = %+v, want only stray.mkv", preview)
	}
	if _, err := os.Stat(filepath.Join(c.dir, "stray.mkv")); err != nil {
		t.Fatal("a preview deleted something")
	}

	if _, err := c.Orphans(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"recorded.mkv": true, "live.mkv": true, "stray.mkv": false, EngineSessionDirName: true} {
		_, err := os.Stat(filepath.Join(c.dir, name))
		if (err == nil) != want {
			t.Errorf("%s present = %v, want %v", name, err == nil, want)
		}
	}
}

// With the engine holding nothing, records still claim their files and the sweep goes ahead for the rest.
func TestOrphansWithAnEmptyEngineKeepsWhatIsOnRecord(t *testing.T) {
	c, st, _ := ownedFixture(t)
	was := orphanSettle
	orphanSettle = 0
	t.Cleanup(func() { orphanSettle = was })
	record(t, st, "rec", "recorded.mkv", 0)
	for _, name := range []string{"recorded.mkv", "stray.mkv"} {
		if err := os.WriteFile(filepath.Join(c.dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := c.Orphans(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.dir, "recorded.mkv")); err != nil {
		t.Error("a recorded download was deleted")
	}
	if _, err := os.Stat(filepath.Join(c.dir, "stray.mkv")); err == nil {
		t.Error("the stray file was kept")
	}
}

func TestClearKeepsKeptDownloads(t *testing.T) {
	c, st := newCache(t)
	ctx := context.Background()
	cache(t, st, "kept", 1, 1<<30, false)
	cache(t, st, "cached", 2, 1<<30, false)
	if _, err := st.KeepDownload(ctx, "kept", true); err != nil {
		t.Fatal(err)
	}

	for _, completedOnly := range []bool{true, false} {
		if _, _, err := c.Clear(ctx, completedOnly); err != nil {
			t.Fatal(err)
		}
	}
	left, _ := st.CacheEntries(ctx)
	if len(left) != 1 || left[0].InfoHash != "kept" {
		t.Fatalf("remaining = %+v, want only the kept download", left)
	}
}

// Clearing deletes whole torrents, so a pack with one file still arriving is not finished.
func TestClearFinishedOnlyJudgesTheWholeTorrent(t *testing.T) {
	c, st := newCache(t)
	ctx := context.Background()
	cacheState(t, st, "pack", 1, 1<<30, false, true)
	cacheState(t, st, "pack", 2, 1<<30, false, false)

	if _, _, err := c.Clear(ctx, true); err != nil {
		t.Fatal(err)
	}
	if n := remaining(t, st); n != 2 {
		t.Fatalf("a part-downloaded pack was cleared: %d rows left", n)
	}
}

// "Overlord IV" states no season in words; its S04 releases were all refused as season 1 asked for.
func TestNumeralSequelTakesItsSeason(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	eps := 13
	if _, err := st.ImportList(ctx, []store.Anime{{ID: 1, Romaji: "Overlord IV", Synonyms: "[]", Genres: "[]", Episodes: &eps}}, nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	f := NewFinder(st, fixedIndexer{}, discard())
	titles, _ := st.SearchTitles(ctx, 1)
	req := f.numbering(ctx, Request{AnimeID: 1, Episode: 5}, titles, "")
	if req.Season != 4 {
		t.Fatalf("season = %d, want 4", req.Season)
	}
	for _, name := range []string{"[Judas] Overlord IV - S04E05 [1080p]", "Overlord.S04E05.1080p.WEB.H264-SENPAI"} {
		if !verifies(parse.Parse(name), req) {
			t.Errorf("refused %s", name)
		}
	}
}

type downIndexer struct{}

func (downIndexer) Name() string { return "down" }
func (downIndexer) Search(context.Context, indexer.Query) ([]indexer.Torrent, error) {
	return nil, errors.New("dial tcp: no such host")
}

// Every site failing is an outage, not "no release", which may well exist.
func TestFindReportsAnOutage(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	eps := 12
	if _, err := st.ImportList(ctx, []store.Anime{{ID: 1, Romaji: "Show", Synonyms: "[]", Genres: "[]", Episodes: &eps}}, nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	f := NewFinder(st, downIndexer{}, discard())
	if _, err := f.Find(ctx, Request{AnimeID: 1, Episode: 1, Season: 1}); err == nil {
		t.Fatal("every search failed, yet Find reported an empty result")
	}
}

// A candidate that turns out to be a download already on record is someone's, whatever the race assumed.
func TestDiscardSparesADownloadOnRecord(t *testing.T) {
	engine := newFakeEngine()
	p := newPlayback(t, engine, nil)
	if err := p.store.RecordTorrent(context.Background(), store.TorrentRecord{
		InfoHash: goodHash, EngineID: 1, Name: "kept", FilePath: "kept.mkv", TotalSize: 1,
	}); err != nil {
		t.Fatal(err)
	}
	engine.ids[1] = goodHash
	engine.ids[2] = otherHash

	p.discard(context.Background(), 1, goodHash)
	p.discard(context.Background(), 2, otherHash)
	if engine.wasDeleted(goodHash) {
		t.Error("discarded a download on record")
	}
	if !engine.wasDeleted(otherHash) {
		t.Error("an abandoned candidate was left")
	}
}

// A record the engine lost is deleted by name; a torrent may name itself the cache root or a Yoru folder.
func TestRemoveByNameNeverDeletesTheCacheOrYorusFolders(t *testing.T) {
	c, st, _ := ownedFixture(t)
	ctx := context.Background()
	for _, dir := range []string{EngineSessionDirName, "hls"} {
		os.MkdirAll(filepath.Join(c.dir, dir), 0o755)
		os.WriteFile(filepath.Join(c.dir, dir, "keep"), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(c.dir, "someone-else.mkv"), []byte("x"), 0o644)
	for i, name := range []string{".", "x/..", EngineSessionDirName, "HLS", "../outside"} {
		hash := string(rune('a'+i)) + "bc"
		record(t, st, hash, name, 0)
		c.evict(ctx, store.CacheEntry{InfoHash: hash})
	}
	for _, path := range []string{c.dir, filepath.Join(c.dir, EngineSessionDirName, "keep"), filepath.Join(c.dir, "hls", "keep"), filepath.Join(c.dir, "someone-else.mkv")} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was deleted", path)
		}
	}
}

// A torrent the engine holds but nobody recorded is deleted only from Yoru's own cache folder.
func TestRemoveRefusesAnUntrackedDownloadOutsideTheCache(t *testing.T) {
	c, _, engine := ownedFixture(t)
	engine.ids[1] = "foreign"
	engine.folder = t.TempDir()
	if _, err := c.Remove(context.Background(), "foreign"); err == nil || engine.wasDeleted("foreign") {
		t.Fatalf("removed another program's download: err=%v", err)
	}
	engine.folder = c.dir
	if _, err := c.Remove(context.Background(), "foreign"); err != nil || !engine.wasDeleted("foreign") {
		t.Fatalf("our own untracked download stayed: err=%v", err)
	}
}

// Yoru's own folders inside the cache folder are never orphans.
func TestOrphansLeaveYorusOwnFoldersAlone(t *testing.T) {
	c, _, _ := ownedFixture(t)
	was := orphanSettle
	orphanSettle = 0
	t.Cleanup(func() { orphanSettle = was })
	bin, data := filepath.Join(c.dir, "bin"), filepath.Join(c.dir, "data")
	c.WithProtected(bin, data)
	os.MkdirAll(bin, 0o755)
	os.MkdirAll(data, 0o755)
	os.WriteFile(filepath.Join(c.dir, "stray.mkv"), []byte("x"), 0o644)

	if _, err := c.Orphans(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	for _, kept := range []string{bin, data} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s was deleted", kept)
		}
	}
	if _, err := os.Stat(filepath.Join(c.dir, "stray.mkv")); err == nil {
		t.Error("the stray file was kept")
	}
}

// A deletion takes only the reviewed orphans: one that turned up or was claimed since stays.
func TestRemoveOrphansDeletesOnlyTheReviewedNames(t *testing.T) {
	c, st, _ := ownedFixture(t)
	was := orphanSettle
	orphanSettle = 0
	t.Cleanup(func() { orphanSettle = was })

	for _, name := range []string{"reviewed.mkv", "new-since.mkv", "claimed.mkv"} {
		if err := os.WriteFile(filepath.Join(c.dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Claimed after the preview: a download recorded it.
	record(t, st, "clm", "claimed.mkv", 0)

	gone, err := c.RemoveOrphans(context.Background(), []string{"reviewed.mkv", "claimed.mkv"})
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 1 || gone[0].Name != "reviewed.mkv" {
		t.Fatalf("removed %+v, want only reviewed.mkv", gone)
	}
	for name, want := range map[string]bool{"reviewed.mkv": false, "new-since.mkv": true, "claimed.mkv": true} {
		if _, err := os.Stat(filepath.Join(c.dir, name)); (err == nil) != want {
			t.Errorf("%s present = %v, want %v", name, err == nil, want)
		}
	}
}

// Records hold a torrent's own name; on disk the engine may have made it safe for Windows or tagged it with the
// hash, and the sweep and the delete must both find it there.
func TestCacheFindsTheEnginesNamesOnDisk(t *testing.T) {
	c, st, _ := ownedFixture(t)
	ctx := context.Background()
	was := orphanSettle
	orphanSettle = 0
	t.Cleanup(func() { orphanSettle = was })
	const colon, clash, old = "aaaaaaaa11", "bbbbbbbb22", "cccccccc33"
	record(t, st, colon, "Show: Part 1.mkv", 0)
	record(t, st, clash, "Other.mkv", 0)
	record(t, st, old, "Old.mkv", 0)
	// The tag after the extension is how a clash was named before safe names.
	safe, tagged, oldTagged := torrent.SafeName("Show: Part 1.mkv"), torrent.Tagged("Other.mkv", clash), "Old.mkv [cccccccc]"
	for _, name := range []string{safe, tagged, oldTagged, "stray.mkv"} {
		if err := os.WriteFile(filepath.Join(c.dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// On Windows the older versions wrote "Show: Part 1.mkv" into a stream of "Show", listed as an empty file.
	keeps := map[string]bool{safe: true, tagged: true, oldTagged: true, "stray.mkv": false}
	if host, ok := torrent.StreamHost("Show: Part 1.mkv"); ok {
		record(t, st, "dddddddd44", "Show: Part 1.mkv", 0)
		if err := os.WriteFile(filepath.Join(c.dir, "Show: Part 1.mkv"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		keeps[host] = true
	}

	if _, err := c.Orphans(ctx, false); err != nil {
		t.Fatal(err)
	}
	for name, want := range keeps {
		if _, err := os.Stat(filepath.Join(c.dir, name)); (err == nil) != want {
			t.Errorf("after the sweep %s present = %v, want %v", name, err == nil, want)
		}
	}

	// Evicting the old clash takes its own tagged file, not the plain name another torrent may hold.
	if err := os.WriteFile(filepath.Join(c.dir, "Old.mkv"), []byte("another torrent's"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{colon, clash, old} {
		if err := c.evict(ctx, store.CacheEntry{InfoHash: hash}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{safe, tagged, oldTagged} {
		if _, err := os.Stat(filepath.Join(c.dir, name)); err == nil {
			t.Errorf("%s stayed after its record was evicted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(c.dir, "Old.mkv")); err != nil {
		t.Error("evicting the tagged copy deleted the plain name's file")
	}
}
