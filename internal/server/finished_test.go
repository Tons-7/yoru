package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/config"
)

func TestShowsOpenFromTheirSavedAnswerAndRefreshBehind(t *testing.T) {
	h := newHarness(t, config.Config{}, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				IDs []int `json:"ids"`
			} `json:"variables"`
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &req)
		var media []string
		for _, id := range req.Variables.IDs {
			if id == 11 {
				continue
			}
			status := "FINISHED"
			if id == 6 {
				status = "RELEASING"
			}
			media = append(media, fmt.Sprintf(`{"id":%d,"status":%q,"title":{"romaji":"Live %d"},
				"studios":{"nodes":[{"id":7,"name":"Madhouse"}]}}`, id, status, id))
		}
		fmt.Fprintf(w, `{"data":{"Page":{"media":[%s]}}}`, strings.Join(media, ","))
	})
	ctx := context.Background()
	str := func(s string) *string { return &s }
	show := func(id int, status string) anilist.Media {
		m := anilist.Media{ID: id, Status: str(status), Title: anilist.Title{Romaji: str(fmt.Sprintf("Saved %d", id))}}
		m.Studios.Nodes = append(m.Studios.Nodes, struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}{ID: 7, Name: "Madhouse"})
		return m
	}
	if _, err := h.server.importer.Save(ctx, []anilist.Media{show(5, "FINISHED"), show(6, "RELEASING"), show(8, "FINISHED")}); err != nil {
		t.Fatal(err)
	}

	open := func(id int) (title string, refreshing bool, m map[string]any) {
		t.Helper()
		res, body := h.do(t, "GET", fmt.Sprintf("/api/anime/%d", id))
		if res.StatusCode != http.StatusOK {
			t.Fatalf("anime %d: HTTP %d %v", id, res.StatusCode, body)
		}
		m = body["anime"].(map[string]any)
		title, _ = m["title"].(map[string]any)["romaji"].(string)
		return title, res.Header.Get(headerRefreshing) != "", m
	}
	age := func(id int, by time.Duration) {
		t.Helper()
		if _, err := h.db.W.ExecContext(ctx, `UPDATE anime SET synced_at = synced_at - ? WHERE id = ?`, int64(by.Seconds()), id); err != nil {
			t.Fatal(err)
		}
	}
	refreshed := func(id int) {
		t.Helper()
		want := fmt.Sprintf("Live %d", id)
		for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			if title, refreshing, _ := open(id); title == want && !refreshing {
				return
			}
		}
		t.Fatalf("show %d never changed to AniList's new answer", id)
	}

	for _, id := range []int{5, 6} {
		title, refreshing, m := open(id)
		studio := m["studios"].(map[string]any)["nodes"].([]any)[0].(map[string]any)
		if title != fmt.Sprintf("Saved %d", id) || refreshing || studio["id"] != float64(7) {
			t.Fatalf("show %d just saved: %q, refreshing %v; want the saved answer with its studio id, and no refresh", id, title, refreshing)
		}
	}
	if h.calls != 0 {
		t.Fatalf("shows saved moments ago asked AniList %d times", h.calls)
	}

	age(5, 6*24*time.Hour)
	if _, refreshing, _ := open(5); refreshing || h.calls != 0 {
		t.Fatalf("a finished show six days old: refreshing %v, %d calls; want it to stand", refreshing, h.calls)
	}

	age(6, 15*time.Minute)
	if title, refreshing, _ := open(6); title != "Saved 6" || !refreshing {
		t.Fatalf("an airing show 15 minutes old: %q, refreshing %v; want the saved answer at once, marked as refreshing", title, refreshing)
	}
	refreshed(6)
	age(6, 15*time.Minute)
	if _, refreshing, _ := open(6); !refreshing {
		t.Fatal("an airing show 15 minutes old again: not marked as refreshing")
	}
	refreshed(6)

	age(8, 8*24*time.Hour)
	if title, refreshing, _ := open(8); title != "Saved 8" || !refreshing {
		t.Fatalf("a finished show eight days old: %q, refreshing %v; want the saved answer at once, marked as refreshing", title, refreshing)
	}
	refreshed(8)

	if title, refreshing, _ := open(9); title != "Live 9" || refreshing {
		t.Fatalf("a show never seen: %q, refreshing %v; want AniList's answer, waited for", title, refreshing)
	}

	if _, err := h.server.importer.Save(ctx, []anilist.Media{show(11, "FINISHED")}); err != nil {
		t.Fatal(err)
	}
	age(11, 8*24*time.Hour)
	if title, refreshing, _ := open(11); title != "Saved 11" || !refreshing {
		t.Fatalf("an old show AniList no longer returns: %q, refreshing %v", title, refreshing)
	}
	gaveUp := false
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end) && !gaveUp; time.Sleep(20 * time.Millisecond) {
		_, refreshing, _ := open(11)
		gaveUp = !refreshing
	}
	if !gaveUp {
		t.Fatal("a show AniList no longer returns stays marked as refreshing on every view")
	}
	asked := h.calls
	for range 5 {
		if title, refreshing, _ := open(11); title != "Saved 11" || refreshing {
			t.Fatalf("after giving up: %q, refreshing %v; want the saved answer, quietly", title, refreshing)
		}
	}
	if h.calls != asked {
		t.Errorf("AniList was asked %d more times for a show it just failed to return", h.calls-asked)
	}
}
