package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPendingRestoreSwapsTheDatabaseAndKeepsTheOld(t *testing.T) {
	dir := t.TempDir()
	live := filepath.Join(dir, "yoru.db")
	backup := filepath.Join(dir, "backup.db")
	for _, p := range []string{live, backup} {
		d, err := Open(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.Migrate(); err != nil {
			t.Fatal(err)
		}
		d.Close()
	}
	d, err := Open(backup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`INSERT INTO setting (key, value) VALUES ('marker', 'from-backup')`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	d.Close()

	if err := CheckBackup(backup); err != nil {
		t.Fatalf("a real database should pass: %v", err)
	}
	if err := os.Rename(backup, RestorePath(live)); err != nil {
		t.Fatal(err)
	}
	for _, side := range []string{"-wal", "-shm"} {
		os.Remove(backup + side)
	}
	restored, err := ApplyPendingRestore(live)
	if err != nil || !restored {
		t.Fatalf("restore = %v, %v", restored, err)
	}
	if _, err := os.Stat(live + ".before-restore"); err != nil {
		t.Errorf("the old database should be kept: %v", err)
	}
	d, err = Open(live)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var v string
	if err := d.R.QueryRow(`SELECT value FROM setting WHERE key = 'marker'`).Scan(&v); err != nil || v != "from-backup" {
		t.Errorf("marker = %q, %v; want the backup's data", v, err)
	}
}

func TestCheckBackupRefusesOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.db")
	if err := os.WriteFile(path, []byte("not a database at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckBackup(path); err == nil {
		t.Fatal("a text file passed as a backup")
	}
}
