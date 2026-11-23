//go:build !noboltdb

package storage

import (
	"encoding/binary"
	"hash/fnv"
	"os"
	"path/filepath"
	"testing"

	"yoru/internal/bt/metainfo"
)

func boltWithRecords(t *testing.T, dir string) []byte {
	t.Helper()
	pc, err := NewBoltPieceCompletion(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ih := range []metainfo.Hash{{1}, {2}} {
		for i := range 50 {
			if err := pc.Set(metainfo.PieceKey{InfoHash: ih, Index: i}, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	pc.Close()
	b, err := os.ReadFile(filepath.Join(dir, ".torrent.bolt.db"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func boltPageSize(b []byte) int { return int(binary.LittleEndian.Uint32(b[24:28])) }

func expectRecovered(t *testing.T, dir string, b []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".torrent.bolt.db"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	pc, reset, err := NewBoltPieceCompletionRecovering(dir)
	if err != nil || !reset {
		t.Fatalf("reset %v, err %v; want the damaged db moved aside and a fresh one opened", reset, err)
	}
	defer pc.Close()
	if c, err := pc.Get(metainfo.PieceKey{InfoHash: metainfo.Hash{1}, Index: 0}); err != nil || c.Ok {
		t.Fatalf("fresh db answers %+v, %v", c, err)
	}
}

func TestBoltRecoversFromABadFreelistPage(t *testing.T) {
	dir, err := os.MkdirTemp("", "bolt-freelist")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	b := boltWithRecords(t, dir)
	ps := boltPageSize(b)
	for _, at := range []int{0, ps} {
		meta := b[at+16 : at+80]
		copy(meta[32:40], meta[16:24])
		h := fnv.New64a()
		h.Write(meta[:56])
		binary.LittleEndian.PutUint64(meta[56:64], h.Sum64())
	}
	expectRecovered(t, dir, b)
}

func TestBoltRecoversFromZeroedDataPages(t *testing.T) {
	dir, err := os.MkdirTemp("", "bolt-zeroed")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	b := boltWithRecords(t, dir)
	ps := boltPageSize(b)
	freelist := int(binary.LittleEndian.Uint64(b[48:56]))
	for page := 2; (page+1)*ps <= len(b); page++ {
		if page != freelist {
			clear(b[page*ps : (page+1)*ps])
		}
	}
	expectRecovered(t, dir, b)
}

func TestBoltForgetKeepsOtherTorrents(t *testing.T) {
	pc, err := NewBoltPieceCompletion(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	a, b := metainfo.Hash{0xa}, metainfo.Hash{0xb}
	for i := range 3 {
		pc.Set(metainfo.PieceKey{InfoHash: a, Index: i}, true)
		pc.Set(metainfo.PieceKey{InfoHash: b, Index: i}, true)
	}
	forget := pc.(interface{ Forget(metainfo.Hash) error })
	if err := forget.Forget(a); err != nil {
		t.Fatal(err)
	}
	if err := forget.Forget(metainfo.Hash{0xc}); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if c, _ := pc.Get(metainfo.PieceKey{InfoHash: a, Index: i}); c.Ok {
			t.Fatalf("forgotten piece %d still known", i)
		}
		if c, _ := pc.Get(metainfo.PieceKey{InfoHash: b, Index: i}); !c.Ok || !c.Complete {
			t.Fatalf("other torrent's piece %d lost: %+v", i, c)
		}
	}
}

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
