package server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"yoru/internal/config"
)

// A MyAnimeList page opens its show: by the offline catalogue, else AniList's answer, else it says so.
func TestAMyAnimeListLinkFindsItsShow(t *testing.T) {
	h := newHarness(t, config.Config{}, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.Contains(string(body), `"mal":777`):
			io.WriteString(w, `{"data":{"Media":{"id":555}}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"errors":[{"message":"Not Found.","status":404}],"data":{"Media":null}}`)
		}
	})
	if _, err := h.db.W.Exec(`INSERT INTO corpus_anime (anime_id, mal_id) VALUES (99, 1234)`); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		mal  string
		code int
		id   float64
	}{{"1234", http.StatusOK, 99}, {"777", http.StatusOK, 555}, {"888", http.StatusNotFound, 0}} {
		code, body := h.call(t, "GET", "/api/anime/from-mal?id="+tt.mal, "")
		if code != tt.code || (tt.id != 0 && body["id"] != tt.id) {
			t.Errorf("MAL %s: %d %v, want %d with id %v", tt.mal, code, body, tt.code, tt.id)
		}
	}
	if calls := h.calls; calls != 2 {
		t.Errorf("AniList was asked %d times, want 2: the catalogue answers 1234 itself", calls)
	}
}
