package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"yoru/internal/config"
	"yoru/internal/store"
)

func upNextKeys(t *testing.T, h *harness) string {
	t.Helper()
	code, body := h.call(t, "GET", "/api/upnext", "")
	if code != 200 {
		t.Fatalf("GET /api/upnext: HTTP %d", code)
	}
	var keys []string
	for _, raw := range body["items"].([]any) {
		it := raw.(map[string]any)
		keys = append(keys, fmt.Sprintf("%v:%v", it["animeId"], it["episode"]))
	}
	return strings.Join(keys, " ")
}

// The list over the API: add, move, fill, what plays after, remove, clear.
func TestUpNextRoutes(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	ctx := context.Background()
	current, twelve := "CURRENT", 12
	if _, err := h.store.ImportList(ctx, []store.Anime{
		{ID: 1, Romaji: "One", Episodes: &twelve, Synonyms: "[]", Genres: "[]"},
		{ID: 2, Romaji: "Two", Episodes: &twelve, Synonyms: "[]", Genres: "[]"},
	}, []store.Entry{{ID: 10, AnimeID: 2, Status: &current, Progress: 3, UpdatedAt: 1}}, store.ImportMerge); err != nil {
		t.Fatal(err)
	}

	for _, body := range []string{`{"animeId":1,"episode":5}`, `{"animeId":1,"episode":6}`} {
		if code, out := h.call(t, "POST", "/api/upnext", body); code != 200 || out["added"] != true {
			t.Fatalf("add %s: HTTP %d %v", body, code, out)
		}
	}
	if code, _ := h.call(t, "POST", "/api/upnext", `{"animeId":1}`); code != 400 {
		t.Errorf("an add without an episode: HTTP %d", code)
	}
	if code, out := h.call(t, "POST", "/api/upnext/fill", ""); code != 200 || out["added"] != float64(1) {
		t.Fatalf("fill: HTTP %d %v", code, out)
	}
	if got := upNextKeys(t, h); got != "1:5 1:6 2:4" {
		t.Fatalf("after fill: %q", got)
	}
	if code, _ := h.call(t, "POST", "/api/upnext/move", `{"animeId":2,"to":0}`); code != 200 {
		t.Fatalf("move: HTTP %d", code)
	}
	if got := upNextKeys(t, h); got != "2:4 1:5 1:6" {
		t.Fatalf("after move: %q", got)
	}

	_, after := h.call(t, "GET", "/api/upnext/after?id=2&episode=4", "")
	next, _ := after["next"].(map[string]any)
	then, _ := after["then"].(map[string]any)
	if after["fromList"] != true || next["animeId"] != float64(1) || next["episode"] != float64(5) ||
		then["episode"] != float64(6) || next["title"] != "One" {
		t.Errorf("after 2:4: %v", after)
	}

	h.call(t, "DELETE", "/api/upnext/1?episode=5", "")
	if got := upNextKeys(t, h); got != "2:4 1:6" {
		t.Fatalf("after removing 1:5: %q", got)
	}
	h.call(t, "DELETE", "/api/upnext/2", "")
	if got := upNextKeys(t, h); got != "1:6" {
		t.Fatalf("after removing show 2: %q", got)
	}
	if _, after := h.call(t, "GET", "/api/upnext/after?id=1&episode=6", ""); after["fromList"] != false {
		t.Errorf("only the playing episode left, yet the list leads: %v", after)
	}
	h.call(t, "DELETE", "/api/upnext", "")
	if got := upNextKeys(t, h); got != "" {
		t.Fatalf("after clearing: %q", got)
	}
}
