package transcode

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSegmentsAreOneFragmentAndJudgedWhole(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	source := testMKV(t, ffmpeg, 20)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", log)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s, err := m.OpenFor(ctx, "1-1", source, source, nil, Want{Soften: true})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(s.ID)
	if s.sliced() {
		t.Fatal("the session copies the picture, want a re-encode")
	}
	for n := 0; n < s.Segments; n++ {
		if err := s.WaitSegment(ctx, n, 90*time.Second); err != nil {
			t.Fatal(err)
		}
		path := s.SegmentPath(n)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(boxes(data, "moof", 0)); got != 1 {
			t.Fatalf("segment %d has %d fragments, want 1", n, got)
		}
		if !segmentComplete(path) {
			t.Fatalf("segment %d is whole but not judged so", n)
		}
		cut := filepath.Join(t.TempDir(), "cut.mp4")
		if err := os.WriteFile(cut, data[:len(data)-100], 0o644); err != nil {
			t.Fatal(err)
		}
		if segmentComplete(cut) {
			t.Fatalf("segment %d cut short is judged whole", n)
		}
	}
}
