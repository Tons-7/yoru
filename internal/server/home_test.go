package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"yoru/internal/config"
	"yoru/internal/store"
)

// fakeShows answers the by-ids query with those asked for of these shows, each with a status.
func fakeShows(w http.ResponseWriter, body []byte, shows map[int]string) {
	var asked struct {
		Variables struct {
			IDs []int `json:"ids"`
		} `json:"variables"`
	}
	json.Unmarshal(body, &asked)
	var media []string
	for _, id := range asked.Variables.IDs {
		status, known := shows[id]
		if !known {
			continue
		}
		n := strconv.Itoa(id)
		media = append(media, `{"id":`+n+`,"status":"`+status+`","title":{"romaji":"Show `+n+`"}}`)
	}
	io.WriteString(w, `{"data":{"Page":{"pageInfo":{"hasNextPage":false},"media":[`+strings.Join(media, ",")+`]}}}`)
}

func seedList(t *testing.T, h *harness, entries map[int][2]any) {
	t.Helper()
	var shows []store.Anime
	var list []store.Entry
	for id, e := range entries {
		status, score := e[0].(string), e[1].(int)
		shows = append(shows, store.Anime{ID: id, Romaji: "Show"})
		list = append(list, store.Entry{ID: id, AnimeID: id, Status: &status, Score: score})
	}
	if _, err := h.store.ImportList(context.Background(), shows, list, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
}

// "For you" sums the picks after the best-liked shows, the favourite's in full: one after both comes first;
// none on the list or adult, and AniList asked once a day.
func TestForYouWeighsTheFavouritesRecommendations(t *testing.T) {
	asked := 0
	h := newHarness(t, config.Config{}, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "recommendations(") {
			asked++
			node := func(id, rating int) string {
				return `{"rating":` + strconv.Itoa(rating) + `,"mediaRecommendation":{"id":` + strconv.Itoa(id) + `,"type":"ANIME"}}`
			}
			adult := `{"rating":99,"mediaRecommendation":{"id":13,"type":"ANIME","isAdult":true}}`
			io.WriteString(w, `{"data":{"m0":{"id":1,"recommendations":{"nodes":[`+node(10, 50)+`,`+node(11, 30)+`,`+node(2, 99)+`,`+adult+`]}},`+
				`"m1":{"id":2,"recommendations":{"nodes":[`+node(11, 40)+`,`+node(12, 10)+`]}}}}`)
			return
		}
		fakeShows(w, body, map[int]string{10: "FINISHED", 11: "FINISHED", 12: "RELEASING", 13: "FINISHED"})
	})
	seedList(t, h, map[int][2]any{1: {"COMPLETED", 90}, 2: {"COMPLETED", 70}})

	for range 2 {
		code, body := h.call(t, "GET", "/api/home/for-you", "")
		items, _ := body["items"].([]any)
		var ids []float64
		for _, it := range items {
			ids = append(ids, it.(map[string]any)["id"].(float64))
		}
		if code != http.StatusOK || len(ids) != 3 || ids[0] != 11 || ids[1] != 10 || ids[2] != 12 {
			t.Fatalf("for you: %d %v, want 11 (after both), 10, 12; never 2, on the list, or 13, adult", code, ids)
		}
	}
	if asked != 1 {
		t.Errorf("AniList asked for recommendations %d times, want once a day", asked)
	}

	// Asked for in Settings, the adult one comes too, first by its rating.
	if code, _ := h.call(t, "POST", "/api/prefs", `{"key":"home.adult_picks","value":"true"}`); code != http.StatusOK {
		t.Fatalf("allowing adult titles: %d", code)
	}
	shown := map[int]bool{}
	_, body := h.call(t, "GET", "/api/home/for-you", "")
	for _, it := range body["items"].([]any) {
		shown[int(it.(map[string]any)["id"].(float64))] = true
	}
	if !shown[13] {
		t.Errorf("with adult titles allowed: %v, want 13 among them", body["items"])
	}
}

// One best-liked show AniList no longer has fails the whole aliased ask: the rest still give "For you".
func TestForYouLeavesOutAShowAniListLost(t *testing.T) {
	h := newHarness(t, config.Config{}, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if q := string(body); strings.Contains(q, "recommendations(") {
			if strings.Contains(q, "Media(id: 2,") {
				w.WriteHeader(http.StatusNotFound)
				io.WriteString(w, `{"data":{"m0":null},"errors":[{"message":"Not Found.","status":404}]}`)
				return
			}
			io.WriteString(w, `{"data":{"m0":{"id":1,"recommendations":{"nodes":[{"rating":5,"mediaRecommendation":{"id":10,"type":"ANIME"}}]}}}}`)
			return
		}
		fakeShows(w, body, map[int]string{10: "FINISHED"})
	})
	seedList(t, h, map[int][2]any{1: {"COMPLETED", 90}, 2: {"COMPLETED", 70}})
	code, body := h.call(t, "GET", "/api/home/for-you", "")
	items, _ := body["items"].([]any)
	if code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["id"] != float64(10) {
		t.Fatalf("for you: %d %v, want 10 from the show still there", code, body)
	}
}

// A sequel AniList now says isn't out yet stays off "Sequels you missed", whatever was saved.
func TestSequelsMissedAreOutNow(t *testing.T) {
	h := newHarness(t, config.Config{}, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fakeShows(w, body, map[int]string{2: "FINISHED", 3: "NOT_YET_RELEASED"})
	})
	seedList(t, h, map[int][2]any{1: {"COMPLETED", 0}})
	if err := h.store.SaveRelations(context.Background(), []int{1},
		[]store.Relation{{AnimeID: 1, RelatedID: 2, Kind: "SEQUEL"}, {AnimeID: 1, RelatedID: 3, Kind: "SEQUEL"}}); err != nil {
		t.Fatal(err)
	}
	code, body := h.call(t, "GET", "/api/home/sequels", "")
	items, _ := body["items"].([]any)
	if code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["id"] != float64(2) {
		t.Fatalf("sequels: %d %v, want only 2", code, items)
	}
}
