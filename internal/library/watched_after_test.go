package library

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yoru/internal/db"
	"yoru/internal/store"
)

func TestMain(m *testing.M) {
	adoptAfter = 0
	os.Exit(m.Run())
}

func stampedCache(t *testing.T) (*Cache, *store.Store, func(animeID, ep int, at int64)) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.Migrate(); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	watchedAt := func(animeID, ep int, at int64) {
		t.Helper()
		if err := st.EnsureAnime(context.Background(), animeID); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.W.Exec(`
			INSERT INTO playback (anime_id, ep_key, position_s, duration_s, played_s, watched, play_count, last_played_at)
			VALUES (?, ?, 1400, 1400, 1400, 1, 1, ?)`, animeID, ep, at); err != nil {
			t.Fatal(err)
		}
	}
	return NewCache(st, nil, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil))), st, watchedAt
}

func TestKeep2DeletesAFinishedShowsLastEpisodesOnceTwoMoreAreWatched(t *testing.T) {
	c, st, watchedAt := stampedCache(t)
	ctx := context.Background()
	if err := st.SetSetting(ctx, "cache.autodelete", "keep2"); err != nil {
		t.Fatal(err)
	}
	episode(t, st, "ep11", 1, 7, 11)
	episode(t, st, "ep12", 2, 7, 12)
	watchedAt(7, 11, 100)
	watchedAt(7, 12, 200)

	if n, _ := c.AutoDelete(ctx, 7); n != 0 {
		t.Fatalf("removed %d with nothing watched after the finale", n)
	}

	watchedAt(8, 1, 300)
	if n, _ := c.AutoDelete(ctx, 8); n != 1 {
		t.Fatalf("removed %d after one more episode of another show, want episode 11", n)
	}
	if left := hashes(t, st); left["ep11"] || !left["ep12"] {
		t.Fatalf("left = %v", left)
	}

	watchedAt(8, 2, 400)
	if n, _ := c.AutoDelete(ctx, 8); n != 1 {
		t.Fatalf("removed %d after two more, want episode 12", n)
	}
	if left := hashes(t, st); left["ep12"] {
		t.Fatalf("left = %v", left)
	}
}

func TestAdoptWaitsForAFreshTorrentsOwnerToRecordIt(t *testing.T) {
	c, st, engine := ownedFixture(t)
	engine.ids[1] = goodHash
	was := adoptAfter
	adoptAfter = time.Hour
	t.Cleanup(func() { adoptAfter = was })

	if err := c.adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := remaining(t, st); n != 0 {
		t.Fatalf("adopted %d download(s) seen for the first time just now", n)
	}

	adoptAfter = 0
	if err := c.adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := remaining(t, st); n != 1 {
		t.Fatalf("the download still unclaimed later was not adopted: %d", n)
	}
}
