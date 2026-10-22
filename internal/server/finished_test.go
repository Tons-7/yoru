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

// A finished show checked within the day opens from its saved answer, unasked and with its studio links;
// an airing one, or a finished one unchecked for over a day, is asked live.
func TestFinishedShowsOpenFromTheirSavedAnswer(t *testing.T) {
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
			media = append(media, fmt.Sprintf(`{"id":%d,"status":"FINISHED","title":{"romaji":"Live %d"},
				"studios":{"nodes":[{"id":7,"name":"Madhouse"}]}}`, id, id))
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

	open := func(id int) map[string]any {
		t.Helper()
		res, body := h.do(t, "GET", fmt.Sprintf("/api/anime/%d", id))
		if res.StatusCode != http.StatusOK {
			t.Fatalf("anime %d: HTTP %d %v", id, res.StatusCode, body)
		}
		return body["anime"].(map[string]any)
	}

	m := open(5)
	if h.calls != 0 {
		t.Fatalf("a finished show checked today asked AniList %d times", h.calls)
	}
	studio := m["studios"].(map[string]any)["nodes"].([]any)[0].(map[string]any)
	if m["title"].(map[string]any)["romaji"] != "Saved 5" || studio["id"] != float64(7) {
		t.Fatalf("served %v, want the saved answer with its studio id", m)
	}

	open(6)
	if h.calls != 1 {
		t.Fatalf("an airing show was not asked live: %d calls", h.calls)
	}

	// Unchecked for over a day: asked again, and the live answer is what shows.
	if _, err := h.db.W.ExecContext(ctx, `UPDATE anime SET synced_at = synced_at - ? WHERE id = 8`,
		int64((25 * time.Hour).Seconds())); err != nil {
		t.Fatal(err)
	}
	if m := open(8); h.calls != 2 || m["title"].(map[string]any)["romaji"] != "Live 8" {
		t.Fatalf("a day-old finished show: %d calls, title %v; want it asked live", h.calls, m["title"])
	}
}
