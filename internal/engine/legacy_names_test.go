package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A download from before safe names is found under its old name and moved; on Windows that includes data
// kept in another file's hidden stream, which goes without taking the host file's other streams.
func TestLegacyNamesMove(t *testing.T) {
	cache := t.TempDir()
	best := "[G] Show. "
	if runtime.GOOS == "windows" {
		best = "[G] Re:Zero - 01.mkv"
	}
	top := safeName(best)
	if got := legacyPath(cache, top, best, false, nil); got != filepath.Join(cache, best) {
		t.Fatalf("legacy path %q, want the torrent's own name", got)
	}
	if got := legacyPath(cache, "Show [abcd1234]", "Show", false, nil); got != filepath.Join(cache, "Show [abcd1234]") {
		t.Errorf("a tagged top kept from the session moved: %q", got)
	}
	if runtime.GOOS != "windows" {
		return
	}

	old := filepath.Join(cache, best)
	other := filepath.Join(cache, "[G] Re:Zero - 02.mkv")
	for _, p := range []string{old, other} {
		if err := os.WriteFile(p, []byte("episode data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	now := filepath.Join(cache, top)
	if err := moveFile(old, now); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(now); err != nil || string(body) != "episode data" {
		t.Fatalf("moved file: %q, %v", body, err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the hidden stream is still there")
	}
	if body, err := os.ReadFile(other); err != nil || string(body) != "episode data" {
		t.Errorf("another episode's stream in the same host went too: %q, %v", body, err)
	}
}
