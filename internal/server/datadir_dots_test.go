package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoru/internal/config"
)

func TestSetDataDirRefusesDotDot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("YORU_ROOT", root)
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "appdata"))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(cfg.ConfigPath())
	h := newHarness(t, cfg, nil)

	escape := filepath.ToSlash(filepath.Join(root, "..", "elsewhere"))
	req := httptest.NewRequest(http.MethodPost, "/api/setup/data-dir", strings.NewReader(`{"path":"`+escape+`/../x"}`))
	req.RemoteAddr = "127.0.0.1:1"
	req.Host = "127.0.0.1:4321"
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	after, _ := os.ReadFile(cfg.ConfigPath())
	if string(after) != string(before) {
		t.Error("config.toml changed after a refused folder")
	}
}
