package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/config"
)

func withArchive(d *Deps) { d.AniList.SetArchive(d.Store) }

func TestSavedAnswersAreNotCachedAsLive(t *testing.T) {
	h := newHarness(t, config.Config{}, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":{"Page":{"pageInfo":{"hasNextPage":false,"total":0},"media":[]}}}`)
	}, withArchive)

	if code, body := h.call(t, "GET", "/api/discover?sort="+anilist.SortWeek, ""); code != http.StatusOK {
		t.Fatalf("live discover answered %d: %v", code, body)
	}
	if _, ok := h.server.rising.get(anilist.SortWeek); !ok {
		t.Fatal("a live ranking was not kept")
	}

	settled := -1
	for range 50 {
		var n int
		if err := h.db.R.QueryRow(`SELECT count(*) FROM http_cache`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 && n == settled {
			break
		}
		settled = n
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := h.db.W.Exec(`UPDATE http_cache SET fetched_at = fetched_at - 30*86400`); err != nil {
		t.Fatal(err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(down.Close)
	fresh := anilist.New(slog.New(slog.DiscardHandler), anilist.WithEndpoint(down.URL), anilist.WithoutRateLimit())
	fresh.SetArchive(h.store)
	h.server.anilist = fresh
	h.server.rising = risingCache{}

	code, headers, _ := h.fetch(t, "GET", "/api/discover?sort="+anilist.SortWeek)
	if code != http.StatusOK {
		t.Fatalf("discover from the saved copy answered %d", code)
	}
	if headers.Get(headerSaved) == "" && headers.Get(headerRefreshing) == "" {
		t.Fatalf("the answer was not marked as saved: %v", headers)
	}
	if _, ok := h.server.rising.get(anilist.SortWeek); ok {
		t.Fatal("a ranking built from saved answers was kept as if it were live")
	}
}
