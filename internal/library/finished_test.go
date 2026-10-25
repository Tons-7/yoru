package library

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/corpus"
	"yoru/internal/db"
	"yoru/internal/store"
)

// fakeCatalogue answers by-id and relations queries from statuses and edges, recording the ids asked.
type fakeCatalogue struct {
	mu     sync.Mutex
	status map[int]string
	edges  map[int][][2]any // id -> {relationType, node id}
	asked  []int
}

func (f *fakeCatalogue) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string `json:"query"`
		Variables struct {
			IDs []int `json:"ids"`
		} `json:"variables"`
	}
	body, _ := io.ReadAll(r.Body)
	json.Unmarshal(body, &req)
	f.mu.Lock()
	defer f.mu.Unlock()
	var media []string
	for _, id := range req.Variables.IDs {
		status, ok := f.status[id]
		if !ok {
			continue
		}
		if !strings.Contains(req.Query, "relations {") {
			f.asked = append(f.asked, id)
			media = append(media, fmt.Sprintf(`{"id":%d,"status":%q,"title":{"romaji":"Show %d"},
				"startDate":{"year":%d}}`, id, status, id, time.Now().Year()))
			continue
		}
		var edges []string
		for _, e := range f.edges[id] {
			edges = append(edges, fmt.Sprintf(`{"relationType":%q,"node":{"id":%d,"type":"ANIME","format":"TV"}}`, e[0], e[1]))
		}
		media = append(media, fmt.Sprintf(`{"id":%d,"relations":{"edges":[%s]}}`, id, strings.Join(edges, ",")))
	}
	fmt.Fprintf(w, `{"data":{"Page":{"media":[%s]}}}`, strings.Join(media, ","))
}

func catalogueStore(t *testing.T, f *fakeCatalogue) (*store.Store, *db.DB, *anilist.Client) {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.Migrate(); err != nil {
		t.Fatal(err)
	}
	return store.New(conn), conn, anilist.New(discard(), anilist.WithEndpoint(srv.URL), anilist.WithoutRateLimit())
}

// Only a finished show keeps AniList's answer: an airing one's countdown would rewrite its row on every fetch.
func TestOnlyFinishedShowsKeepTheirAnswer(t *testing.T) {
	f := &fakeCatalogue{status: map[int]string{1: "FINISHED", 4: "RELEASING"}}
	st, conn, al := catalogueStore(t, f)
	ctx := context.Background()
	imp := NewImporter(st, al, discard())
	if _, err := imp.Hydrate(ctx, []int{1, 4}); err != nil {
		t.Fatal(err)
	}
	if m, ok := imp.Finished(ctx, 1); !ok || m.ID != 1 {
		t.Error("a finished show doesn't open from its saved answer")
	}
	if _, ok := imp.Finished(ctx, 4); ok {
		t.Error("an airing show was served from its saved answer")
	}
	var kept int
	if err := conn.R.QueryRowContext(ctx, `SELECT count(*) FROM anime WHERE id = 4 AND media IS NOT NULL`).Scan(&kept); err != nil || kept != 0 {
		t.Errorf("an airing show kept its answer (%d, %v)", kept, err)
	}

	// A day on, the answer no longer stands.
	if _, err := conn.W.ExecContext(ctx, `UPDATE anime SET synced_at = synced_at - ? WHERE id = 1`,
		int64((25 * time.Hour).Seconds())); err != nil {
		t.Fatal(err)
	}
	if _, ok := imp.Finished(ctx, 1); ok {
		t.Error("a day-old answer was served")
	}
}

// A later-found sequel of a show on the list is told once; not off the list, at first discovery, or alerts off.
func TestANewSeasonOfAShowOnTheListIsAnnounced(t *testing.T) {
	f := &fakeCatalogue{
		status: map[int]string{100: "FINISHED", 200: "FINISHED", 900: "NOT_YET_RELEASED", 901: "NOT_YET_RELEASED"},
		edges: map[int][][2]any{900: {{"PREQUEL", 100}}, 100: {{"SEQUEL", 900}},
			901: {{"PREQUEL", 200}}, 200: {{"SEQUEL", 901}}},
	}
	st, _, al := catalogueStore(t, f)
	ctx := context.Background()
	if _, err := NewImporter(st, al, discard()).Hydrate(ctx, []int{100, 200}); err != nil {
		t.Fatal(err)
	}
	completed := "COMPLETED"
	entries := []store.Entry{{ID: 1, AnimeID: 100, Status: &completed}, {ID: 2, AnimeID: 200, Status: &completed}}
	if _, err := st.ImportList(ctx, nil, entries, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	if err := st.SetAnimePref(ctx, 200, "notify.enabled", "false"); err != nil {
		t.Fatal(err)
	}

	served := "anilist\tmyanimelist\n100\t0\n200\t0\n"
	sources := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/animeapi" {
			io.WriteString(w, served)
			return
		}
		http.Error(w, "gone", http.StatusNotFound)
	}))
	t.Cleanup(sources.Close)
	fetch := corpus.NewFetcher()
	fetch.SetURL("manami", sources.URL+"/manami")
	fetch.SetURL("animeapi", sources.URL+"/animeapi")
	ing := NewIngester(st, fetch, al, discard())
	ing.Relations = NewRelations(st, al, discard())

	// The first discovery: nothing it finds is news.
	ing.Run(ctx, true)
	served += "900\t0\n901\t0\n"
	ing.Run(ctx, true)
	ing.Run(ctx, true)

	items, err := st.Notifications(ctx, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	var seasons []store.Notification
	for _, n := range items {
		if n.Kind == store.NotifySeason {
			seasons = append(seasons, n)
		}
	}
	if len(seasons) != 1 || *seasons[0].AnimeID != 900 || !strings.HasPrefix(seasons[0].Title, "Show 900") ||
		!strings.Contains(seasons[0].Body, "Follows Show 100") {
		t.Fatalf("season notifications = %+v, want one for the sequel of the show on the list", seasons)
	}
}

// A new season discovered today that continues a saved show joins that show's seasons the same day.
func TestANewSeasonJoinsTheSavedShowTheDayItIsFound(t *testing.T) {
	f := &fakeCatalogue{
		status: map[int]string{100: "FINISHED", 900: "NOT_YET_RELEASED"},
		edges:  map[int][][2]any{900: {{"PREQUEL", 100}}, 100: {{"SEQUEL", 900}}},
	}
	st, _, al := catalogueStore(t, f)
	ctx := context.Background()
	if _, err := NewImporter(st, al, discard()).Hydrate(ctx, []int{100}); err != nil {
		t.Fatal(err)
	}
	// Show 100 was walked before the sequel existed: a franchise of one.
	if err := st.MarkNoRelations(ctx, 100); err != nil {
		t.Fatal(err)
	}

	sources := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/animeapi" {
			io.WriteString(w, "anilist\tmyanimelist\n100\t0\n900\t0\n")
			return
		}
		http.Error(w, "gone", http.StatusNotFound)
	}))
	t.Cleanup(sources.Close)
	fetch := corpus.NewFetcher()
	fetch.SetURL("manami", sources.URL+"/manami")
	fetch.SetURL("animeapi", sources.URL+"/animeapi")
	ing := NewIngester(st, fetch, al, discard())
	ing.Relations = NewRelations(st, al, discard())

	rep, _ := ing.Run(ctx, false)
	if rep.Linked != 1 {
		t.Fatalf("linked %d, want the new season", rep.Linked)
	}
	fr, err := st.Franchise(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	var seasons []int
	for _, s := range fr.Seasons {
		seasons = append(seasons, s.ID)
	}
	if !slices.Contains(seasons, 900) {
		t.Fatalf("seasons of the saved show = %v, want the new one in", seasons)
	}
}
