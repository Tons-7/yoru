package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yoru/internal/config"
)

// A start that fails before the data folder is claimed still leaves why in yoru.log, added to what is there.
func TestEarlyLinesKeptOnAFailedStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.Name+".log")
	if err := os.WriteFile(path, []byte("the running one's line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &logSink{}
	s.Write([]byte("fatal: config.toml: bad\n"))
	s.keepEarly(dir)
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "the running one's line") || !strings.Contains(string(body), "config.toml: bad") {
		t.Errorf("yoru.log = %q; want the failure added after what was there", body)
	}
	s.keepEarly(dir)
	if again, _ := os.ReadFile(path); strings.Count(string(again), "config.toml: bad") != 1 {
		t.Error("the same lines were written twice")
	}
}
