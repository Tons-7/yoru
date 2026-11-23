package deps

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoru/internal/config"
)

func serveBytes(t *testing.T, body []byte) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(body)
	return srv, hex.EncodeToString(sum[:])
}

func TestABareBinaryInstallsUnderItsToolName(t *testing.T) {
	body := []byte("cloudflared binary")
	srv, digest := serveBytes(t, body)
	bin := t.TempDir()
	m := New(bin, slog.New(slog.NewTextHandler(io.Discard, nil)))
	exe := config.ExeName("cloudflared")
	spec := component{name: "cloudflared", files: []string{exe}, resolve: func(context.Context, *Manager) (Release, error) {
		return Release{Version: "2026.1", URL: srv.URL + "/cloudflared-windows-amd64.exe", Digest: digest}, nil
	}}
	m.running["cloudflared"] = &Progress{Component: "cloudflared"}
	if err := m.install(context.Background(), &spec); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(bin, exe)); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("%s holds %q, %v", exe, got, err)
	}
	if raw, _ := os.ReadFile(filepath.Join(bin, "versions.json")); !strings.Contains(string(raw), `"cloudflared": "2026.1"`) {
		t.Errorf("versions.json = %s", raw)
	}
}

func TestAGlobInstallsOnlyMatchingFiles(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range []string{"glsl/Upscale/A.glsl", "glsl/Restore/B.glsl", "README.md"} {
		w, _ := zw.Create(name)
		w.Write([]byte(name))
	}
	zw.Close()
	srv, digest := serveBytes(t, buf.Bytes())
	bin := t.TempDir()
	m := New(bin, slog.New(slog.NewTextHandler(io.Discard, nil)))
	spec := component{name: "anime4k", glob: "*.glsl", into: "shaders", resolve: func(context.Context, *Manager) (Release, error) {
		return Release{Version: "4", URL: srv.URL + "/anime4k.zip", Archive: true, Digest: digest}, nil
	}}
	m.running["anime4k"] = &Progress{Component: "anime4k"}
	if err := m.install(context.Background(), &spec); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(bin, "shaders"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "A.glsl,B.glsl" {
		t.Fatalf("shaders folder holds %v, want only the two shaders", names)
	}
}
