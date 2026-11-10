package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"yoru/internal/config"
	"yoru/internal/deps"
)

func TestSmoothMotionIsReadyOnlyWithThePinnedFiles(t *testing.T) {
	bin := t.TempDir()
	h := newHarness(t, config.Config{BinDir: bin}, nil)

	if _, body := h.call(t, "GET", "/api/smooth", ""); body["ready"] != false || body["wanted"] != deps.FlyHighVersion {
		t.Fatalf("nothing installed, yet %v", body)
	}

	dir := filepath.Join(bin, deps.FlyHighDir)
	os.MkdirAll(dir, 0o755)
	for _, f := range deps.FlyHighFiles {
		os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(bin, "versions.json"), []byte(`{"flyhigh": "0.0.1"}`), 0o644)
	if _, body := h.call(t, "GET", "/api/smooth", ""); body["ready"] != false {
		t.Errorf("an older engine than the player is written for counts as ready: %v", body)
	}

	os.WriteFile(filepath.Join(bin, "versions.json"), []byte(`{"flyhigh": "`+deps.FlyHighVersion+`"}`), 0o644)
	if _, body := h.call(t, "GET", "/api/smooth", ""); body["ready"] != true || body["canInstall"] != false {
		t.Errorf("pinned files in place: %v", body)
	}
}

func TestSmoothMotionServesOnlyItsOwnFiles(t *testing.T) {
	bin := t.TempDir()
	h := newHarness(t, config.Config{BinDir: bin}, nil)
	dir := filepath.Join(bin, deps.FlyHighDir)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "flyhigh_web_bg.wasm"), []byte("\x00asm"), 0o644)
	os.WriteFile(filepath.Join(bin, "versions.json"), []byte(`{"flyhigh": "`+deps.FlyHighVersion+`"}`), 0o644)

	get := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Host = "127.0.0.1:4321"
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := get("/api/smooth/flyhigh_web_bg.wasm"); rec.Code != http.StatusNotFound {
		t.Errorf("a half-installed engine was served: %d", rec.Code)
	}
	for _, f := range deps.FlyHighFiles {
		if f != "flyhigh_web_bg.wasm" {
			os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644)
		}
	}
	if rec := get("/api/smooth/flyhigh_web_bg.wasm"); rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/wasm" {
		t.Errorf("the engine answered %d as %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get("/api/smooth/flyhighfg.weights"); rec.Code != http.StatusOK ||
		rec.Header().Get("Content-Type") == "application/octet-stream" {
		t.Errorf("the model answered %d as %q; download managers grab .bin and octet-stream", rec.Code,
			rec.Header().Get("Content-Type"))
	}
	if rec := get("/api/smooth/versions.json"); rec.Code != http.StatusNotFound {
		t.Errorf("a file beside the engine was served: %d", rec.Code)
	}
	os.WriteFile(filepath.Join(bin, "versions.json"), []byte(`{"flyhigh": "0.0.1"}`), 0o644)
	if rec := get("/api/smooth/graph.json"); rec.Code != http.StatusNotFound {
		t.Errorf("files of another version were served: %d", rec.Code)
	}
}
