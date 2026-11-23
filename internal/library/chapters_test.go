package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneChapterFilesTakesOnlyOldChapterFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.Add(-2 * chapterFileAge)
	for name, at := range map[string]time.Time{
		"chapters-1-2.ini": old,
		"chapters-1-3.ini": now,
		"chapters-x.ini":   old,
		"episode.mkv":      old,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	pruneChapterFiles(dir, now)
	for name, want := range map[string]bool{
		"chapters-1-2.ini": false,
		"chapters-1-3.ini": true,
		"chapters-x.ini":   true,
		"episode.mkv":      true,
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		if got := err == nil; got != want {
			t.Errorf("%s kept = %v, want %v", name, got, want)
		}
	}
}
