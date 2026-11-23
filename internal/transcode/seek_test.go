package transcode

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestShouldRestart(t *testing.T) {
	const from, to = 38, 45
	cases := []struct {
		name string
		n    int
		want bool
	}{
		{"not running", 40, true},
		{"the segment being worked on", 46, false},
		{"a few ahead, buffer drift", 49, false},
		{"just past the grace window", 51, true},
		{"far forward jump", 200, true},
		{"backward seek before the pass", 10, true},
		{"the pass start itself", 38, false},
		{"one before the start", 37, true},
	}
	for _, c := range cases {
		active := c.name != "not running"
		if got := shouldRestart(c.n, from, to, active); got != c.want {
			t.Errorf("%s: shouldRestart(%d, from=%d, to=%d) = %v, want %v",
				c.name, c.n, from, to, got, c.want)
		}
	}

	for n := 0; n <= restartAfterSegments; n++ {
		if shouldRestart(n, 0, -1, true) {
			t.Errorf("fresh pass restarted for a near segment %d", n)
		}
	}
	if !shouldRestart(restartAfterSegments+1, 0, -1, true) {
		t.Error("a jump past a fresh encoder's reach should restart")
	}
}

func TestSeekingForwardAndBackDelivers(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	clip := testMKV(t, ffmpeg, 60)

	log := slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", log)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	session, err := m.Open(ctx, "seek", clip)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close("seek")

	if err := session.WaitSegment(ctx, 0, 90*time.Second); err != nil {
		t.Fatalf("first segment: %v", err)
	}

	last := session.Segments - 1
	if err := session.WaitSegment(ctx, last, 90*time.Second); err != nil {
		t.Fatalf("seek to the final segment: %v", err)
	}

	start := time.Now()
	if err := session.WaitSegment(ctx, 1, 90*time.Second); err != nil {
		t.Fatalf("seek back to segment 1: %v", err)
	}
	t.Logf("backward seek delivered in %s", time.Since(start).Round(time.Millisecond))

	if err := session.WaitSegment(ctx, last/2, 90*time.Second); err != nil {
		t.Fatalf("mid seek: %v", err)
	}
}

func TestAdvanceHeadTracksProducedSegments(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	clip := testMKV(t, ffmpeg, 30)

	log := slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelWarn}))
	m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", log)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	session, err := m.Open(ctx, "advance", clip)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close("advance")

	last := -1
	for n := 0; n < session.Segments; n++ {
		if err := session.WaitSegment(ctx, n, 60*time.Second); err != nil {
			t.Fatalf("segment %d: %v", n, err)
		}
		session.mu.Lock()
		from := session.headFrom
		session.mu.Unlock()
		if from < last {
			t.Fatalf("forward play moved the encoder backward at segment %d: headFrom %d -> %d", n, last, from)
		}
		last = from
	}
}

func TestPlaylistSegmentMapsToFixedTime(t *testing.T) {
	s := &Session{Info: &MediaInfo{Duration: 120}, Segments: segmentCount(120)}
	playlist := s.Playlist()
	if !strings.Contains(playlist, "#EXT-X-PLAYLIST-TYPE:VOD") {
		t.Error("expected a VOD playlist")
	}
	if got := strings.Count(playlist, "#EXTINF"); got != s.Segments {
		t.Errorf("%d segments listed, want %d", got, s.Segments)
	}
}
