package engine

import (
	"testing"

	"yoru/internal/bt/metainfo"
)

// After an unclean stop each torrent is rechecked once; a rebind's restore must not hash it again.
func TestCrashRecheckOncePerTorrent(t *testing.T) {
	e := &Engine{unclean: true}
	a, b := metainfo.Hash{1}, metainfo.Hash{2}
	if !e.firstSinceCrashLocked(a) || !e.firstSinceCrashLocked(b) {
		t.Fatal("a torrent was not rechecked after the crash")
	}
	if e.firstSinceCrashLocked(a) {
		t.Error("the second restore rechecked a torrent again")
	}
	if (&Engine{}).firstSinceCrashLocked(a) {
		t.Error("a clean start rechecked a torrent")
	}
}
