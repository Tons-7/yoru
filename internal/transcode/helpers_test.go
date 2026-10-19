package transcode

import (
	"io"
	"os"
	"testing"
)

// readSegment is segment n as the server sends it, whichever way the session stores it.
func readSegment(s *Session, n int) ([]byte, error) {
	r, err := s.OpenSegment(n)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// segmentBytes is readSegment failing the test on an error.
func segmentBytes(t *testing.T, s *Session, n int) []byte {
	t.Helper()
	b, err := readSegment(s, n)
	if err != nil {
		t.Fatalf("segment %d: %v", n, err)
	}
	return b
}

// forgetSegment drops segment n alone, as if no pass had made it yet.
func forgetSegment(s *Session, n int) {
	if !s.sliced() {
		os.Remove(s.SegmentPath(n))
		return
	}
	s.mu.Lock()
	seg := s.segs[n]
	delete(s.segs, n)
	s.mu.Unlock()
	removeFiles(seg.parts)
}
