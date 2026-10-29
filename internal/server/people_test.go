package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"yoru/internal/config"
	"yoru/internal/library"
	"yoru/internal/metadata"
	"yoru/internal/store"
)

// peopleHarness answers the person and character pages from stand-ins, Jikan down, with a list holding one
// show (MAL 1575, Yoru's 100) the viewer completed.
func peopleHarness(t *testing.T, person, character string) *harness {
	t.Helper()
	answer := func(body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	meta := metadata.New()
	meta.SetURL("tenrai-person", answer(person))
	meta.SetURL("tenrai-character", answer(character))
	meta.SetURL("jikan-person", "http://127.0.0.1:9")
	meta.SetURL("jikan-character", "http://127.0.0.1:9")
	h := newHarness(t, config.Config{}, nil, func(d *Deps) {
		d.Enricher = library.NewEnricher(d.Store, meta, slog.New(slog.DiscardHandler))
	})
	mal, year, english, status := 1575, 2006, "Code Geass: Lelouch of the Rebellion", "COMPLETED"
	if _, err := h.store.ImportList(context.Background(),
		[]store.Anime{{ID: 100, Romaji: "Code Geass", English: &english, MalID: &mal, SeasonYear: &year}},
		[]store.Entry{{ID: 100, AnimeID: 100, Status: &status}}, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	return h
}

const personJSON = `{"data":{"mal_id":86,"name":"Fukuyama, Jun","given_name":"潤","family_name":"福山","about":"Born in Osaka.",
"birthday":"1978-11-26T00:00:00+00:00","favorites":9000,"images":{"jpg":{"image_url":"https://cdn/jun.jpg"}},
"voices":[
 {"role":"Supporting","anime":{"mal_id":999,"title":"Newer Show"},"character":{"mal_id":7,"name":"Someone"}},
 {"role":"Main","anime":{"mal_id":2904,"title":"Code Geass R2"},"character":{"mal_id":417,"name":"Lamperouge, Lelouch"}},
 {"role":"Main","anime":{"mal_id":1575,"title":"Code Geass: Hangyaku no Lelouch"},"character":{"mal_id":417,"name":"Lamperouge, Lelouch"}}],
"anime":[{"position":"Theme Song Performance","anime":{"mal_id":1575,"title":"Code Geass"}},
 {"position":"Inserted Song Performance","anime":{"mal_id":1575,"title":"Code Geass"}}]}}`

// A voice actor's page: one card a character however many shows they're in, the viewer's own show first and
// as Yoru names it; their credits one row a show.
func TestAVoiceActorsRolesComeByCharacter(t *testing.T) {
	h := peopleHarness(t, personJSON, `{}`)
	code, body := h.call(t, "GET", "/api/person/86", "")
	if code != http.StatusOK || body["native"] != "福山 潤" || body["birthday"] != "1978-11-26" {
		t.Fatalf("person: %d %v", code, body)
	}
	chars := body["characters"].([]any)
	if len(chars) != 2 {
		t.Fatalf("characters = %v, want Lelouch once and Someone", chars)
	}
	lelouch := chars[0].(map[string]any)
	shows := lelouch["shows"].([]any)
	first := shows[0].(map[string]any)
	if lelouch["character"].(map[string]any)["name"] != "Lamperouge, Lelouch" || len(shows) != 2 ||
		first["id"] != float64(100) || first["status"] != "COMPLETED" || first["title"] != "Code Geass: Lelouch of the Rebellion" {
		t.Errorf("first card = %v, want Lelouch, the completed show first in English", lelouch)
	}
	work := body["work"].([]any)
	if len(work) != 1 || len(work[0].(map[string]any)["positions"].([]any)) != 2 {
		t.Errorf("work = %v, want one row with both songs", work)
	}
}

// A character's page: the shows they're in (the viewer's first) and their voices, Japanese first.
func TestACharactersPage(t *testing.T) {
	h := peopleHarness(t, `{}`, `{"data":{"mal_id":417,"name":"Lamperouge, Lelouch","name_kanji":"ルルーシュ","about":"A prince.",
"anime":[{"role":"Main","anime":{"mal_id":2904,"title":"Code Geass R2"}},{"role":"Main","anime":{"mal_id":1575,"title":"Code Geass"}}],
"voices":[{"language":"English","person":{"mal_id":5,"name":"Johnny Yong Bosch"}},{"language":"Japanese","person":{"mal_id":86,"name":"Fukuyama, Jun"}}]}}`)
	code, body := h.call(t, "GET", "/api/character/417", "")
	if code != http.StatusOK || body["native"] != "ルルーシュ" {
		t.Fatalf("character: %d %v", code, body)
	}
	apps := body["appearances"].([]any)
	if first := apps[0].(map[string]any)["show"].(map[string]any); first["id"] != float64(100) {
		t.Errorf("appearances = %v, want the completed show first", apps)
	}
	if voices := body["voices"].([]any); voices[0].(map[string]any)["language"] != "Japanese" {
		t.Errorf("voices = %v, want Japanese first", voices)
	}
	if code, _ := h.call(t, "GET", "/api/character/abc", ""); code != http.StatusBadRequest {
		t.Errorf("a bad id answered %d", code)
	}
}
