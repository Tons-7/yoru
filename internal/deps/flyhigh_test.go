package deps

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
)

func flyhighArchive(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	put := func(name, body string) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	for _, f := range []string{"flyhigh_web.js", "flyhigh_web_bg.wasm"} {
		put("flyhigh-v9-web/pkg/"+f, f)
	}
	for _, f := range []string{"flyhighfg.bin", "flyhighfg.json", "graph.json"} {
		put("flyhigh-v9-web/models/"+f, f)
	}
	put("flyhigh-v9-web/example/graph.json", "an example's copy")
	put("flyhigh-v9-web/example/main.js", "not ours")
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestFlyHighInstallsItsFilesIntoItsOwnFolder(t *testing.T) {
	if !Known("flyhigh") {
		t.Fatal("flyhigh is not installable")
	}
	archive := flyhighArchive(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(archive) }))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(archive)

	bin := t.TempDir()
	m := New(bin, slog.New(slog.NewTextHandler(io.Discard, nil)))
	spec := component{name: "flyhigh", files: FlyHighFiles, from: []string{"pkg", "models"}, into: FlyHighDir, resolve: func(context.Context, *Manager) (Release, error) {
		return Release{Version: "9", URL: srv.URL + "/flyhigh-v9-web.tar.gz", Archive: true, Digest: hex.EncodeToString(sum[:])}, nil
	}}
	m.running["flyhigh"] = &Progress{Component: "flyhigh"}
	if err := m.install(context.Background(), &spec); err != nil {
		t.Fatal(err)
	}
	for _, f := range FlyHighFiles {
		if got, err := os.ReadFile(filepath.Join(bin, FlyHighDir, f)); err != nil || string(got) != f {
			t.Errorf("%s: %q %v", f, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(bin, FlyHighDir, "main.js")); err == nil {
		t.Error("the example's script was installed too")
	}
	if raw, _ := os.ReadFile(filepath.Join(bin, "versions.json")); !strings.Contains(string(raw), `"flyhigh": "9"`) {
		t.Errorf("versions.json = %s", raw)
	}

	spec.resolve = func(context.Context, *Manager) (Release, error) {
		return Release{Version: "9", URL: srv.URL + "/flyhigh-v9-web.tar.gz", Archive: true, Digest: strings.Repeat("0", 64)}, nil
	}
	if err := m.install(context.Background(), &spec); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("a swapped archive installed: %v", err)
	}
}

func TestInstallsFinishingTogetherKeepEachOthersVersions(t *testing.T) {
	bin := t.TempDir()
	m := New(bin, nil)
	names := []string{"ffmpeg", "mpv", "anime4k", "flyhigh", "a", "b", "c", "d"}
	done := make(chan struct{})
	for _, n := range names {
		go func() {
			m.record(n, "1")
			done <- struct{}{}
		}()
	}
	for range names {
		<-done
	}
	raw, _ := os.ReadFile(filepath.Join(bin, "versions.json"))
	for _, n := range names {
		if !strings.Contains(string(raw), `"`+n+`": "1"`) {
			t.Errorf("%s lost from versions.json:\n%s", n, raw)
		}
	}
}

func TestFlyHighIsFetchedOnEverySystem(t *testing.T) {
	if cmd := ManualCommand("flyhigh"); cmd != "" {
		t.Errorf("asked for a package manager: %q", cmd)
	}
}

func TestFlyHighIsPinnedToTheFork(t *testing.T) {
	rel, err := resolveFlyHigh(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != FlyHighVersion || len(rel.Digest) != 64 || !strings.HasPrefix(rel.URL, "https://github.com/Tons-7/flyhigh-engine/releases/download/v"+FlyHighVersion+"/") {
		t.Errorf("%+v", rel)
	}
}
