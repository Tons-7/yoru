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

type fakeCatalogue struct {
	mu     sync.Mutex
	status map[int]string
	edges  map[int][][2]any
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

func TestEveryShowKeepsItsAnswerAndKnowsWhenItIsOld(t *testing.T) {
	f := &fakeCatalogue{status: map[int]string{1: "FINISHED", 4: "RELEASING", 5: "NOT_YET_RELEASED"}}
	st, conn, al := catalogueStore(t, f)
	ctx := context.Background()
	imp := NewImporter(st, al, discard())
	if _, err := imp.Hydrate(ctx, []int{1, 4, 5}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 4, 5} {
		if m, _, fresh, ok := imp.Kept(ctx, id); !ok || !fresh || m.ID != id {
			t.Errorf("show %d just fetched: kept %v, fresh %v", id, ok, fresh)
		}
	}
	if _, _, _, ok := imp.Kept(ctx, 9); ok {
		t.Error("a show never fetched has a saved answer")
	}

	age := func(id int, by time.Duration) {
		t.Helper()
		if _, err := conn.W.ExecContext(ctx, `UPDATE anime SET synced_at = synced_at - ? WHERE id = ?`,
			int64(by.Seconds()), id); err != nil {
			t.Fatal(err)
		}
	}
	age(1, 6*24*time.Hour)
	age(4, 5*time.Minute)
	age(5, 5*time.Hour)
	for _, id := range []int{1, 4, 5} {
		if _, _, fresh, ok := imp.Kept(ctx, id); !ok || !fresh {
			t.Errorf("show %d within its time: kept %v, fresh %v", id, ok, fresh)
		}
	}
	age(1, 2*24*time.Hour)
	age(4, 10*time.Minute)
	age(5, 2*time.Hour)
	for _, id := range []int{1, 4, 5} {
		if m, at, fresh, ok := imp.Kept(ctx, id); !ok || fresh || m.ID != id || at.IsZero() {
			t.Errorf("show %d past its time: kept %v, fresh %v; want it kept to show, and marked old", id, ok, fresh)
		}
	}

	now := time.Now().Unix()
	if _, err := conn.W.ExecContext(ctx, `UPDATE anime SET synced_at = ?, next_airing_at = ? WHERE id = 4`, now-120, now+600); err != nil {
		t.Fatal(err)
	}
	if _, _, fresh, _ := imp.Kept(ctx, 4); !fresh {
		t.Error("an airing show fetched two minutes ago, its episode still ahead, counts as old")
	}
	if _, err := conn.W.ExecContext(ctx, `UPDATE anime SET next_airing_at = ? WHERE id = 4`, now-60); err != nil {
		t.Fatal(err)
	}
	if _, _, fresh, _ := imp.Kept(ctx, 4); fresh {
		t.Error("an airing show whose episode has aired since it was fetched still counts as fresh")
	}
}

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

func TestASavedAiringShowNeverShowsAPastBroadcastOrSkipsItsCheck(t *testing.T) {
	st, conn, al := catalogueStore(t, &fakeCatalogue{})
	ctx := context.Background()
	imp := NewImporter(st, al, discard())
	status := "RELEASING"
	now := int(time.Now().Unix())
	show := anilist.Media{ID: 3, Status: &status, NextAiring: &anilist.Airing{Episode: 7, AiringAt: now + 3600}}
	if _, err := imp.Save(ctx, []anilist.Media{show}); err != nil {
		t.Fatal(err)
	}
	if m, _, fresh, ok := imp.Kept(ctx, 3); !ok || !fresh || m.NextAiring == nil || m.NextAiring.TimeUntilAiring < 3500 {
		t.Fatalf("an episode an hour ahead: kept %v, fresh %v, next %+v", ok, fresh, m.NextAiring)
	}

	if _, err := conn.W.ExecContext(ctx, `UPDATE anime SET synced_at = ?, next_airing_at = ?,
		media = replace(media, ?, ?) WHERE id = 3`, now-600, now-60, fmt.Sprint(now+3600), fmt.Sprint(now-60)); err != nil {
		t.Fatal(err)
	}
	m, _, fresh, ok := imp.Kept(ctx, 3)
	if !ok || fresh || m.NextAiring != nil {
		t.Fatalf("its episode aired a minute ago: kept %v, fresh %v, next %+v; want it old, with no past broadcast shown", ok, fresh, m.NextAiring)
	}

	var before, after int64
	conn.R.QueryRowContext(ctx, `SELECT synced_at FROM anime WHERE id = 3`).Scan(&before)
	imp.checked(ctx, []store.Anime{{ID: 3}})
	conn.R.QueryRowContext(ctx, `SELECT synced_at FROM anime WHERE id = 3`).Scan(&after)
	if after != before {
		t.Fatalf("a show due its after-airing check was stamped as checked (%d -> %d); the airing job would skip it for hours", before, after)
	}
	due, err := st.StaleAiring(ctx, time.Now(), 10)
	if err != nil || len(due) != 1 || due[0] != 3 {
		t.Fatalf("due for a check: %v %v; want show 3", due, err)
	}
}
