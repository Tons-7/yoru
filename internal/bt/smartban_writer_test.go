package bt

import (
	"net/netip"
	"testing"

	"github.com/cespare/xxhash"
)

func smartBanWriter(chunk int) (*blockCheckingWriter, *smartBanCache) {
	cache := &smartBanCache{Hash: func(b []byte) uint64 { return xxhash.Sum64(b) }}
	cache.Init()
	return &blockCheckingWriter{cache: cache, chunkBuffer: make([]byte, chunk)}, cache
}

func TestBlockCheckingWriterAlignment(t *testing.T) {
	const chunk = 16
	good, bad := netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("10.0.0.2")
	piece := make([]byte, chunk*3+chunk/2)
	for i := range piece {
		piece[i] = byte(i*7 + 3)
	}
	blocks := func() [][]byte {
		var out [][]byte
		for off := 0; off < len(piece); off += chunk {
			out = append(out, piece[off:min(off+chunk, len(piece))])
		}
		return out
	}()

	for _, c := range []struct {
		name     string
		badBlock int
	}{{"a middle block", 1}, {"the short last block", len(blocks) - 1}} {
		w, cache := smartBanWriter(chunk)
		for i, b := range blocks {
			cache.RecordBlock(good, RequestIndex(i), b)
			if i == c.badBlock {
				flipped := append([]byte(nil), b...)
				flipped[0] ^= 0xff
				cache.RecordBlock(bad, RequestIndex(i), flipped)
			}
		}
		for _, part := range [][]byte{piece[:1], piece[1 : 1+chunk+3], piece[1+chunk+3:]} {
			if n, err := w.Write(part); err != nil || n != len(part) {
				t.Fatalf("%s: write %d of %d, %v", c.name, n, len(part), err)
			}
		}
		w.Flush()
		if _, isBad := w.badPeers[bad]; !isBad || len(w.badPeers) != 1 {
			t.Errorf("%s: bad peers %v, want only the peer that sent the wrong block", c.name, w.badPeers)
		}
	}
}
