package config

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"yoru/internal/proc"
)

// Name is the app's name in folders and files; Legacy is the one it had until October 2026.
const (
	Name   = "yoru"
	Legacy = "kuro"
)

// Env reads YORU_<key>, else KURO_<key>, the name scripts and services used before the rename.
func Env(key string) string { return cmp.Or(os.Getenv("YORU_"+key), os.Getenv("KURO_"+key)) }

// dataFiles move together: SQLite finds the journal and shared memory by the database's name.
var dataFiles = [][]string{{".db-wal", ".db-shm", ".db"}, {".log"}, {".log.1"}, {".lock"}}

// PrepareDataDir carries kuro's data over to the default folder before anything writes there; it returns the
// folder to use, the old one while something holds it.
func PrepareDataDir() string { return moveLegacyData(dataDir(), legacyDataDir()) }

// moveLegacyData moves oldDir to newDir once: whole when newDir is new, else entry by entry, database first.
// A held database leaves this run on oldDir; the next start tries again.
func moveLegacyData(newDir, oldDir string) string {
	if hasDatabase(newDir) || !hasDatabase(oldDir) {
		renameLegacyFiles(newDir)
		return newDir
	}
	// A running kuro holds its lock: Unix would let the folder move under it, leaving it writing to nothing.
	if lockHeld(oldDir) {
		return oldDir
	}
	if !dirExists(newDir) {
		if os.Rename(oldDir, newDir) == nil {
			moved = append(moved, [2]string{oldDir, newDir})
			renameLegacyFiles(newDir)
			return newDir
		}
	} else if mergeInto(oldDir, newDir) {
		renameLegacyFiles(newDir)
		return newDir
	}
	renameLegacyFiles(oldDir)
	return oldDir
}

// mergeInto moves oldDir's database, then whatever newDir lacks, into newDir; true once the database is there.
func mergeInto(oldDir, newDir string) bool {
	for _, name := range []string{Legacy, Name} {
		var moves [][2]string
		for _, ext := range dataFiles[0] {
			if from := filepath.Join(oldDir, name+ext); exists(from) {
				moves = append(moves, [2]string{from, filepath.Join(newDir, name+ext)})
			}
		}
		if !renameAll(moves) {
			return false
		}
	}
	entries, _ := os.ReadDir(oldDir)
	for _, e := range entries {
		to := filepath.Join(newDir, e.Name())
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if from := filepath.Join(oldDir, e.Name()); os.Rename(from, to) == nil {
			moved = append(moved, [2]string{from, to})
		}
	}
	// Only once empty: a file still held keeps the old folder until a later start.
	os.Remove(oldDir)
	return hasDatabase(newDir)
}

// renameLegacyFiles gives kuro.db and company their new names, each group all or nothing, never over a file.
func renameLegacyFiles(dir string) {
	for _, group := range dataFiles {
		var moves [][2]string
		for _, ext := range group {
			if from := filepath.Join(dir, Legacy+ext); exists(from) {
				moves = append(moves, [2]string{from, filepath.Join(dir, Name+ext)})
			}
		}
		renameAll(moves)
	}
}

// renameAll does every move or, putting back what it did, none; it never replaces a file.
func renameAll(moves [][2]string) bool {
	for i, m := range moves {
		if _, err := os.Lstat(m[1]); err == nil || os.Rename(m[0], m[1]) != nil {
			for _, done := range moves[:i] {
				os.Rename(done[1], done[0])
			}
			return false
		}
	}
	moved = append(moved, moves...)
	return true
}

// moved is this run's renames from kuro's folder and names, in order.
var moved [][2]string

// UndoLegacyMove puts this run's moves back, last first, for a failed update handing back to kuro.exe: kuro
// reads only its own folder and names, and would start on an empty library.
func UndoLegacyMove() error {
	var errs []error
	for _, m := range slices.Backward(moved) {
		from, to := m[0], m[1]
		// A merge removed the emptied old folder.
		os.MkdirAll(filepath.Dir(from), 0o755)
		if err := os.Rename(to, from); err != nil {
			errs = append(errs, err)
		}
	}
	moved = nil
	return errors.Join(errs...)
}

// lockHeld: another process holds dir's lock file, under either name.
func lockHeld(dir string) bool {
	for _, name := range []string{Legacy, Name} {
		p := filepath.Join(dir, name+".lock")
		if !exists(p) {
			continue
		}
		unlock, err := proc.Lock(p)
		if err != nil {
			return true
		}
		unlock()
	}
	return false
}

func hasDatabase(dir string) bool {
	return exists(filepath.Join(dir, Name+".db")) || exists(filepath.Join(dir, Legacy+".db"))
}

// dataFile is yoru<ext> in the data folder, or kuro<ext> while a held file kept its old name.
func (c Config) dataFile(ext string) string {
	p := filepath.Join(c.dataDir, Name+ext)
	if old := filepath.Join(c.dataDir, Legacy+ext); !exists(p) && exists(old) {
		return old
	}
	return p
}

// LockPath sits in the data folder beside the database.
func (c Config) LockPath() string { return c.dataFile(".lock") }

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
