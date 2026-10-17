package library

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"yoru/internal/db"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

// With an Up next list, what gets ready during an episode is the list's next, not the show's.
func TestPrepareFollowsTheUpNextList(t *testing.T) {
	eng := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"torrents":[]}`)
	}))
	t.Cleanup(eng.Close)
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.Migrate()
	st := store.New(conn)
	ctx := context.Background()

	twelve := 12
	st.ImportList(ctx, []store.Anime{
		{ID: 1, Romaji: "Show", Episodes: &twelve},
		{ID: 2, Romaji: "Elsewhere", Episodes: &twelve},
	}, nil, store.ImportMerge)
	st.AddUpNext(ctx, 2, 7)

	idx := &countingIndexer{}
	p := NewPrefetcher(st, NewFinder(st, idx, discard()), torrent.NewClient(eng.URL), discard())
	p.Prepare(1, 1, 1, score.DefaultPreferences())
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		busy := len(p.running) > 0
		p.mu.Unlock()
		if !busy && idx.count() > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !idx.searched("07") || idx.searched("02") {
		t.Fatalf("searched %v, want the list's episode 7 and not the show's 2", idx.queries)
	}
}
