package library

import (
	"context"
	"testing"
)

func TestClearKeepsEveryFileOfAPinnedTorrent(t *testing.T) {
	c, st := newCache(t)
	ctx := context.Background()

	cache(t, st, "pack", 3, 1<<30, true)
	cache(t, st, "pack", 7, 1<<30, false)

	removed, freed, err := c.Clear(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 || freed != 0 {
		t.Fatalf("cleared %d entries (%d bytes) of a torrent that is playing", removed, freed)
	}

	entries, err := st.CacheEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("%d cache entries left, want both files of the pack", len(entries))
	}
}

func TestClearRemovesIdleTorrents(t *testing.T) {
	c, st := newCache(t)
	ctx := context.Background()

	cache(t, st, "idle", 1, 1<<30, false)
	cache(t, st, "idle", 2, 1<<30, false)

	removed, _, err := c.Clear(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed %d, want the one torrent counted once", removed)
	}
}

func TestClearSparesADownloadBeingFetched(t *testing.T) {
	c, st := newCache(t)
	ctx := context.Background()
	cacheState(t, st, "ahead", 1, 1<<30, false, false)
	cacheState(t, st, "idle", 2, 1<<30, false, false)
	c.WithClaimed(func(h string) bool { return h == "ahead" })

	if _, _, err := c.Clear(ctx, false); err != nil {
		t.Fatal(err)
	}
	entries, err := st.CacheEntries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].InfoHash != "ahead" {
		t.Fatalf("left %+v, want only the download being fetched", entries)
	}
}

func TestSweepSparesADownloadBeingFetched(t *testing.T) {
	c, st := newCache(t)
	ctx := context.Background()
	cache(t, st, "ahead", 1, 3<<30, false)
	cache(t, st, "newer", 2, 3<<30, false)
	c.WithClaimed(func(h string) bool { return h == "ahead" })
	if err := st.SetSetting(ctx, "cache.budget_bytes", "1073741824"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	entries, _ := st.CacheEntries(ctx)
	for _, e := range entries {
		if e.InfoHash == "ahead" {
			return
		}
	}
	t.Fatal("the oldest entry was evicted while it was being fetched")
}
