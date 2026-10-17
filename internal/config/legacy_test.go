package config

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// put writes each file under dir, folders included.
func put(t *testing.T, dir string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// tree lists the files under dir, relative and slash-separated, or nil when dir is gone.
func tree(dir string) []string {
	var out []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(out)
	return out
}

// kuro's folder becomes Yoru's on the first start: whole when new, its database and log renamed with it.
func TestLegacyDataMovesWhole(t *testing.T) {
	base := t.TempDir()
	oldDir, newDir := filepath.Join(base, "kuro"), filepath.Join(base, "yoru")
	put(t, oldDir, "kuro.db", "kuro.db-wal", "kuro.log", "window/Preferences")

	if got := moveLegacyData(newDir, oldDir); got != newDir {
		t.Fatalf("data in %s, want %s", got, newDir)
	}
	if got := tree(newDir); !slices.Equal(got, []string{"window/Preferences", "yoru.db", "yoru.db-wal", "yoru.log"}) {
		t.Errorf("new folder holds %v", got)
	}
	if dirExists(oldDir) {
		t.Errorf("the old folder is still there: %v", tree(oldDir))
	}
	// Moved, so the next start leaves it be.
	if got := moveLegacyData(newDir, oldDir); got != newDir || len(tree(newDir)) != 4 {
		t.Errorf("a second start moved things again: %s %v", got, tree(newDir))
	}
}

// The log opens before the config loads, so the new folder can exist first; the data still comes over.
func TestLegacyDataMergesIntoAFolderTheLogMade(t *testing.T) {
	base := t.TempDir()
	oldDir, newDir := filepath.Join(base, "kuro"), filepath.Join(base, "yoru")
	put(t, oldDir, "kuro.db", "kuro.db-shm", "kuro.log", "window/Preferences")
	put(t, newDir, "yoru.log")

	if got := moveLegacyData(newDir, oldDir); got != newDir {
		t.Fatalf("data in %s, want %s", got, newDir)
	}
	// The log this run wrote wins over the old one, which stays under its old name rather than replace it.
	want := []string{"kuro.log", "window/Preferences", "yoru.db", "yoru.db-shm", "yoru.log"}
	if got := tree(newDir); !slices.Equal(got, want) {
		t.Errorf("new folder holds %v, want %v", got, want)
	}
	if dirExists(oldDir) {
		t.Errorf("the old folder is still there: %v", tree(oldDir))
	}
}

// Two databases: one is a fresh start made before, and nothing may replace either.
func TestLegacyDataNeverReplacesADatabase(t *testing.T) {
	base := t.TempDir()
	oldDir, newDir := filepath.Join(base, "kuro"), filepath.Join(base, "yoru")
	put(t, oldDir, "kuro.db")
	put(t, newDir, "yoru.db")

	if got := moveLegacyData(newDir, oldDir); got != newDir {
		t.Fatalf("data in %s, want %s", got, newDir)
	}
	if got := tree(oldDir); !slices.Equal(got, []string{"kuro.db"}) {
		t.Errorf("old folder now holds %v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(newDir, "yoru.db")); string(b) != "yoru.db" {
		t.Errorf("the new database was replaced: %q", b)
	}
}

// Nothing to carry over: a fresh install neither moves nor makes anything.
func TestLegacyDataFreshInstall(t *testing.T) {
	base := t.TempDir()
	oldDir, newDir := filepath.Join(base, "kuro"), filepath.Join(base, "yoru")
	if got := moveLegacyData(newDir, oldDir); got != newDir || dirExists(newDir) || dirExists(oldDir) {
		t.Errorf("fresh install: %s, new made %v, old made %v", got, dirExists(newDir), dirExists(oldDir))
	}
}

// A database held open stays where it is, under its old name, journal and all; the next start finishes the move.
func TestLegacyDataHeldStaysWhole(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows refuses to move a file another program holds")
	}
	base := t.TempDir()
	oldDir, newDir := filepath.Join(base, "kuro"), filepath.Join(base, "yoru")
	put(t, oldDir, "kuro.db", "kuro.db-wal")
	put(t, newDir, "yoru.log")
	held, err := os.Open(filepath.Join(oldDir, "kuro.db"))
	if err != nil {
		t.Fatal(err)
	}

	got := moveLegacyData(newDir, oldDir)
	if got != oldDir {
		t.Errorf("data in %s while its database was held, want %s", got, oldDir)
	}
	if got := tree(oldDir); !slices.Equal(got, []string{"kuro.db", "kuro.db-wal"}) {
		t.Errorf("held: old folder holds %v, want the database and its journal together", got)
	}
	cfg := Config{dataDir: got}
	if p := cfg.DatabasePath(); p != filepath.Join(oldDir, "kuro.db") {
		t.Errorf("database path %s, want the old name it still has", p)
	}

	held.Close()
	if got := moveLegacyData(newDir, oldDir); got != newDir || !slices.Contains(tree(newDir), "yoru.db-wal") {
		t.Errorf("once let go: %s %v", got, tree(newDir))
	}
}

// A chosen data folder keeps its place; only the files inside take the new name.
func TestLegacyFilesInAChosenFolder(t *testing.T) {
	dir := inTempDir(t)
	put(t, filepath.Join(dir, "data"), "kuro.db", "kuro.db-wal")
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("data_dir = 'data'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.DatabasePath() != filepath.Join(dir, "data", "yoru.db") || !slices.Contains(tree(filepath.Join(dir, "data")), "yoru.db-wal") {
		t.Errorf("database %s, folder %v", got.DatabasePath(), tree(filepath.Join(dir, "data")))
	}
}

// Scripts and services set KURO_ROOT before the rename; it still points Yoru at its folder.
func TestLegacyRootVariable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YORU_ROOT", "")
	t.Setenv("KURO_ROOT", dir)
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "appdata"))
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigPath() != filepath.Join(dir, "config.toml") {
		t.Errorf("config at %s, want under KURO_ROOT %s", cfg.ConfigPath(), dir)
	}
}
