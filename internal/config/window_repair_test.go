package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func profileWithStorage(t *testing.T, log string) (string, string) {
	t.Helper()
	profile := t.TempDir()
	storage := filepath.Join(profile, "Default", "Local Storage")
	if err := os.MkdirAll(filepath.Join(storage, "leveldb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storage, "leveldb", "LOG"), []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	return profile, storage
}

func asides(t *testing.T, profile string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(profile, "Default", storageAside+"*"))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func quiet(string, ...any) {}

func TestDamagedWindowStorageIsSetAside(t *testing.T) {
	profile, storage := profileWithStorage(t, "Recovering log #3\n(ignoring error) 000003.log: dropping 23750 bytes; Corruption: checksum mismatch\n")
	old := filepath.Join(profile, "Default", storageAside+"2026-01-01-000000")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}

	repairStorage(profile, quiet)

	if _, err := os.Stat(storage); !os.IsNotExist(err) {
		t.Fatalf("the damaged storage is still in place: %v", err)
	}
	left := asides(t, profile)
	if len(left) != 1 || left[0] == old {
		t.Fatalf("set aside = %v, want only the copy just made", left)
	}
}

func TestHealthyWindowStorageIsLeftAlone(t *testing.T) {
	profile, storage := profileWithStorage(t, "Reusing MANIFEST MANIFEST-000001\nRecovering log #3\nReusing old log 000003.log\n")

	repairStorage(profile, quiet)

	if _, err := os.Stat(storage); err != nil {
		t.Fatalf("healthy storage was moved: %v", err)
	}
	if left := asides(t, profile); len(left) != 0 {
		t.Fatalf("set aside = %v, want nothing", left)
	}
}

func TestStorageOfARunningBrowserIsLeftAlone(t *testing.T) {
	profile, storage := profileWithStorage(t, "Corruption: checksum mismatch\n")
	if runtime.GOOS == "windows" {
		held, err := os.Open(filepath.Join(storage, "leveldb", "LOG"))
		if err != nil {
			t.Fatal(err)
		}
		defer held.Close()
	} else if err := os.Symlink("host-123", filepath.Join(profile, "SingletonLock")); err != nil {
		t.Fatal(err)
	}

	repairStorage(profile, quiet)

	if _, err := os.Stat(storage); err != nil {
		t.Fatalf("storage a running browser holds was moved: %v", err)
	}
}
