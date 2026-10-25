package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"yoru/internal/config"
	"yoru/internal/library"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

// A player app's link plays one file on another device with no token or cookie, seeking included; tampering,
// age and a pairing reset end it, and it opens nothing else.
func TestPlayerAppLinks(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	h.server.token = "s3cret"
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "Show - 03.mkv")
	if err := os.WriteFile(file, []byte("0123456789abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp, _ := h.store.NextScanStamp(ctx)
	if _, err := h.store.SaveLocalFiles(ctx, stamp, []store.LocalFile{{Path: file, Size: 16, Modified: 1}}); err != nil {
		t.Fatal(err)
	}
	files, _ := h.store.LocalFiles(ctx, 0, true, store.Paging{Page: 1, PerPage: 10})
	if len(files.Items) != 1 {
		t.Fatalf("local files = %d", len(files.Items))
	}

	// Asked for by a paired phone, over the network.
	mint := func(source string) (int, map[string]any) {
		raw, _ := json.Marshal(map[string]any{"source": source})
		req := httptest.NewRequest("POST", "/api/stream/external", strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer s3cret")
		req.Host = "192.168.1.10:4321"
		req.RemoteAddr = "192.168.1.50:5555"
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		var out map[string]any
		json.NewDecoder(rec.Body).Decode(&out)
		return rec.Code, out
	}
	code, out := mint("/api/local/" + strconv.Itoa(files.Items[0].ID) + "/stream")
	if code != http.StatusOK {
		t.Fatalf("mint: HTTP %d %v", code, out)
	}
	link, _ := url.Parse(out["url"].(string))
	if link.Host != "192.168.1.10:4321" || !strings.HasSuffix(link.Path, "/Show - 03.mkv") {
		t.Errorf("link %s: want this device's address and the file's name", link)
	}

	// Opened by the video app: another client, no token, no cookie.
	app := func(target string, header http.Header) (int, string) {
		res := h.from(t, "192.168.1.60:5555", target, header)
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}
	if code, body := app(link.EscapedPath(), nil); code != http.StatusOK || body != "0123456789abcdef" {
		t.Errorf("whole file: HTTP %d %q", code, body)
	}
	if code, body := app(link.EscapedPath(), http.Header{"Range": {"bytes=4-7"}}); code != http.StatusPartialContent || body != "4567" {
		t.Errorf("seek: HTTP %d %q", code, body)
	}
	// Save to this device: the same link, as a file of the episode's name.
	saved := h.from(t, "192.168.1.60:5555", link.EscapedPath()+"?download=1", nil)
	if got := saved.Header.Get("Content-Disposition"); got != `attachment; filename="Show - 03.mkv"` {
		t.Errorf("saved: Content-Disposition %q", got)
	}
	if played := h.from(t, "192.168.1.60:5555", link.EscapedPath(), nil); played.Header.Get("Content-Disposition") != "" {
		t.Errorf("played: Content-Disposition %q, want none", played.Header.Get("Content-Disposition"))
	}
	grantPart := strings.Split(link.Path, "/")[2]
	tampered := strings.Replace(link.EscapedPath(), grantPart, grantPart[:len(grantPart)-2]+"xx", 1)
	if code, _ := app(tampered, nil); code != http.StatusForbidden {
		t.Errorf("tampered link: HTTP %d, want 403", code)
	}
	if _, ok := h.server.openGrant(grantPart, time.Now().Add(externalFor+time.Minute)); ok {
		t.Error("a link older than its life still opens")
	}
	if code, _ := app("/api/health", nil); code != http.StatusUnauthorized {
		t.Errorf("anything but the link without the token: HTTP %d, want 401", code)
	}
	if code, _ := mint("http://example.com/movie.mkv"); code != http.StatusBadRequest {
		t.Errorf("a link to somewhere else: HTTP %d, want 400", code)
	}
	h.server.token = "n3w"
	if code, _ := app(link.EscapedPath(), nil); code != http.StatusForbidden {
		t.Errorf("after a pairing reset: HTTP %d, want 403", code)
	}
}

// A torrent's link relays the engine's ranged stream, named after the file, without the app's browser headers.
func TestPlayerAppLinksRelayTheEngine(t *testing.T) {
	var leaked []string
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/torrents/1":
			json.NewEncoder(w).Encode(map[string]any{"files": []map[string]any{{"name": `Pack\Show - 04.mkv`}}})
		case "/torrents/1/stream/0":
			for _, h := range []string{"Origin", "Cookie", "Sec-Fetch-Mode"} {
				if r.Header.Get(h) != "" {
					leaked = append(leaked, h)
				}
			}
			http.ServeContent(w, r, "Show - 04.mkv", time.Time{}, strings.NewReader("episode four bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(engine.Close)
	tc := torrent.NewClient(engine.URL)
	h := newHarness(t, config.Config{}, nil, func(d *Deps) { d.Engine = tc })
	h.server.playback = library.NewPlayback(h.store, nil, tc, nil, t.TempDir(), slog.New(slog.DiscardHandler))

	_, out := h.postJSON(t, "/api/stream/external", map[string]any{"source": engine.URL + "/torrents/1/stream/0"})
	raw, _ := out["url"].(string)
	link, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(link.Path, "/Show - 04.mkv") {
		t.Fatalf("link %q %v: want the file's own name", raw, out)
	}
	res := h.from(t, "192.168.1.60:5555", link.EscapedPath(), http.Header{
		"Range": {"bytes=8-11"}, "Origin": {"https://elsewhere"}, "Cookie": {"yoru_access=x"}, "Sec-Fetch-Mode": {"navigate"},
	})
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusPartialContent || string(body) != "four" {
		t.Errorf("relayed seek: HTTP %d %q", res.StatusCode, body)
	}
	if len(leaked) > 0 {
		t.Errorf("the engine was sent %v", leaked)
	}
	saved := h.from(t, "192.168.1.60:5555", link.EscapedPath()+"?download=1", nil)
	if got := saved.Header.Get("Content-Disposition"); got != `attachment; filename="Show - 04.mkv"` {
		t.Errorf("saved from the engine: Content-Disposition %q", got)
	}
}
