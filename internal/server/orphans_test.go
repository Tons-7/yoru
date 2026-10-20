package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"yoru/internal/config"
	"yoru/internal/library"
	"yoru/internal/torrent"
)

// Looking never deletes, and a delete takes only the names the page showed: a request without them, or with
// none, leaves every file where it is.
func TestCleanOrphansOnlyDeletesNamedFiles(t *testing.T) {
	dir := t.TempDir()
	stray := filepath.Join(dir, "stray.mkv")
	if err := os.WriteFile(stray, []byte("left behind"), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /torrents", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"torrents": []any{}})
	})
	engine := httptest.NewServer(mux)
	t.Cleanup(engine.Close)
	h := newHarness(t, config.Config{}, nil)
	h.server.cache = library.NewCache(h.store, torrent.NewClient(engine.URL), dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	onDisk := func() bool {
		_, err := os.Stat(stray)
		return err == nil
	}

	res, body := h.do(t, "GET", "/api/cache/orphans")
	if res.StatusCode != http.StatusOK || body["removed"] != float64(1) {
		t.Fatalf("GET: HTTP %d %v, want the stray file listed", res.StatusCode, body)
	}
	if !onDisk() {
		t.Fatal("GET deleted the file it listed")
	}

	if res, _ := h.do(t, "POST", "/api/cache/orphans"); res.StatusCode != http.StatusBadRequest || !onDisk() {
		t.Fatalf("POST without names: HTTP %d, on disk %v; want 400 and the file kept", res.StatusCode, onDisk())
	}
	if res, body := h.postJSON(t, "/api/cache/orphans", map[string]any{"names": []string{}}); res.StatusCode != http.StatusOK ||
		body["removed"] != float64(0) || !onDisk() {
		t.Fatalf("POST with no names: HTTP %d %v, on disk %v; want nothing removed", res.StatusCode, body, onDisk())
	}

	res, body = h.postJSON(t, "/api/cache/orphans", map[string]any{"names": []string{"stray.mkv", "claimed.mkv"}})
	if res.StatusCode != http.StatusOK || body["removed"] != float64(1) || onDisk() {
		t.Fatalf("POST naming it: HTTP %d %v, on disk %v; want it removed", res.StatusCode, body, onDisk())
	}
	// A name shown but no longer an orphan (or gone) is reported kept, not silently dropped.
	if kept, _ := body["kept"].([]any); len(kept) != 1 || kept[0] != "claimed.mkv" {
		t.Fatalf("kept = %v, want the name that was not removed", body["kept"])
	}
}
