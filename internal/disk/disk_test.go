package disk

import (
	"runtime"
	"testing"
)

func TestFreeReadsTheDrive(t *testing.T) {
	dir := t.TempDir()
	free, drive, err := Free(dir)
	if err != nil || free <= 0 {
		t.Fatalf("free = %d, %v", free, err)
	}
	if runtime.GOOS == "windows" && len(drive) != 2 {
		t.Errorf("drive = %q, want a letter and a colon", drive)
	}
}
