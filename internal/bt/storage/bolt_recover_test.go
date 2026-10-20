//go:build !noboltdb && !wasm

package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// A file bbolt can't read is moved aside for a fresh one; one held by another opener is refused, untouched.
func TestBoltCompletionRecoversOnlyFromDamage(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, ".torrent.bolt.db")
	if err := os.WriteFile(db, []byte("not a bolt database, not at all, just text long enough to read"), 0o644); err != nil {
		t.Fatal(err)
	}
	pc, reset, err := NewBoltPieceCompletionRecovering(dir)
	if err != nil || !reset {
		t.Fatalf("damaged db: reset %v, err %v; want it moved aside and a fresh one opened", reset, err)
	}
	pc.Close()

	held, err := NewBoltPieceCompletion(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if _, reset, err := NewBoltPieceCompletionRecovering(dir); err == nil || reset {
		t.Fatalf("held db: reset %v, err %v; want an error and nothing moved", reset, err)
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatalf("the held db was moved: %v", err)
	}
}
