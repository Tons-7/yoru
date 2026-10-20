package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yoru/internal/config"
	"yoru/internal/indexer"
)

const siteFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel>
<item>
<category>Anime</category>
<title>[Group] Show - 01 (1080p).mkv</title>
<description><![CDATA[<a href="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567">Magnet Link</a><br />Size: 1.2GB]]></description>
<guid><![CDATA[https://index.example/details.php?id=1]]></guid>
<pubDate>Tue, 01 Sep 2026 15:02:33 GMT</pubDate>
</item>
</channel></rss>`

func sendJSON(t *testing.T, h *harness, method, target string, body any) (*http.Response, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, target, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "127.0.0.1:4321"
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	res := rec.Result()
	var out map[string]any
	json.NewDecoder(res.Body).Decode(&out)
	return res, out
}

// config.toml's blocks are shown until a list is saved in the app; saving replaces them and searches at once.
func TestReleaseSitesSavedInTheApp(t *testing.T) {
	sites := indexer.NewSites()
	h := newHarness(t, config.Config{Indexers: []config.Indexer{{Type: "nyaa", URL: "https://from-config.example"}}}, nil,
		func(d *Deps) { d.Sites = sites })

	_, body := h.do(t, "GET", "/api/sites")
	listed, _ := body["sites"].([]any)
	if body["saved"] != false || len(listed) != 1 || listed[0].(map[string]any)["url"] != "https://from-config.example" {
		t.Fatalf("before saving: %v, want config.toml's one site", body)
	}

	res, body := sendJSON(t, h, "PUT", "/api/sites", map[string]any{"sites": []indexer.Site{
		{Type: "Nyaa", URL: " https://nyaa.example/ "}, {Type: "tokyotosho", URL: "https://tt.example"},
	}})
	if res.StatusCode != 200 || body["saved"] != true {
		t.Fatalf("save: HTTP %d %v", res.StatusCode, body)
	}
	if general, _ := sites.Counts(); general != 2 {
		t.Errorf("searching with %d sites after the save, want 2", general)
	}
	saved, fromApp, err := LoadSites(context.Background(), h.store, h.server.cfg)
	if err != nil || !fromApp || len(saved) != 2 || saved[0] != (indexer.Site{Type: "nyaa", URL: "https://nyaa.example"}) {
		t.Errorf("loaded %+v (saved %v, %v), want the trimmed list", saved, fromApp, err)
	}

	for name, list := range map[string][]indexer.Site{
		"unknown type": {{Type: "torznab", URL: "https://x.example"}},
		"twice":        {{Type: "nyaa", URL: "https://a.example"}, {Type: "nyaa", URL: "https://A.example"}},
		"not a url":    {{Type: "nyaa", URL: "nyaa"}},
	} {
		if res, _ := sendJSON(t, h, "PUT", "/api/sites", map[string]any{"sites": list}); res.StatusCode != 400 {
			t.Errorf("%s: HTTP %d, want 400", name, res.StatusCode)
		}
	}
	if general, _ := sites.Counts(); general != 2 {
		t.Errorf("a refused save changed the sites in use to %d", general)
	}
}

// Test searches the site as given and says what came back, before anything is saved.
func TestReleaseSiteTest(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	answering := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, siteFeed) }))
	t.Cleanup(answering.Close)
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<?xml version="1.0"?><rss version="2.0"><channel></channel></rss>`)
	}))
	t.Cleanup(empty.Close)

	res, body := sendJSON(t, h, "POST", "/api/sites/test", indexer.Site{Type: "tokyotosho", URL: answering.URL})
	if res.StatusCode != 200 || body["count"] != float64(1) {
		t.Fatalf("a working site: HTTP %d %v, want one result", res.StatusCode, body)
	}
	if res, body := sendJSON(t, h, "POST", "/api/sites/test", indexer.Site{Type: "tokyotosho", URL: empty.URL}); res.StatusCode != 502 ||
		!strings.Contains(body["error"].(string), "no results") {
		t.Errorf("an empty answer: HTTP %d %v, want it named", res.StatusCode, body)
	}
}
