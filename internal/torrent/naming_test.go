package torrent

import (
	"runtime"
	"slices"
	"testing"
)

// The names a download can sit under: this torrent's tagged ones first, then its own, then older versions'.
func TestDiskNames(t *testing.T) {
	got := DiskNames("Show - 01.mkv", "ABCDEF0123")
	want := []string{"Show - 01 [abcdef01].mkv", "Show - 01.mkv [abcdef01]", "Show - 01.mkv"}
	if !slices.Equal(got, want) {
		t.Errorf("DiskNames = %q, want %q", got, want)
	}
	if runtime.GOOS == "windows" {
		if got := DiskNames("Re:Zero.mkv", "ab"); !slices.Contains(got, "Re：Zero.mkv") || !slices.Contains(got, "Re:Zero.mkv") {
			t.Errorf("an unsafe name lacks its safe or its old form: %q", got)
		}
		if host, ok := StreamHost("[G] Re:Zero.mkv"); !ok || host != "[G] Re" {
			t.Errorf("StreamHost = %q, %v", host, ok)
		}
	}
	if _, ok := StreamHost("Plain.mkv"); ok {
		t.Error("a name without a colon has no stream host")
	}
}
