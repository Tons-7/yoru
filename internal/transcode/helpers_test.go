package transcode

import (
	"io"
	"os"
	"testing"
)

func readSegment(s *Session, n int) ([]byte, error) {
	r, err := s.OpenSegment(n)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func segmentBytes(t *testing.T, s *Session, n int) []byte {
	t.Helper()
	b, err := readSegment(s, n)
	if err != nil {
		t.Fatalf("segment %d: %v", n, err)
	}
	return b
}

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
