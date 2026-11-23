package transcode

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func twoTrackClip(t *testing.T, ffmpeg string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "two.mkv")
	if b, err := exec.Command(ffmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x240:rate=24:duration=14",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=14",
		"-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000:duration=14",
		"-map", "0:v", "-map", "1:a", "-map", "2:a",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-g", "48",
		"-c:a", "aac", "-metadata:s:a:0", "language=jpn", "-metadata:s:a:1", "language=eng", out).CombinedOutput(); err != nil {
		t.Fatalf("make the clip: %v\n%s", err, b)
	}
	return out
}

func TestSwitchingTrackMidPlayDropsTheOldSegments(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	clip := twoTrackClip(t, ffmpeg)
	m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s, err := m.Open(ctx, "1-1", clip)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close(s.ID)
	for n := range 2 {
		if err := s.WaitSegment(ctx, n, 90*time.Second); err != nil {
			t.Fatal(err)
		}
	}

	if !s.SetAudioTrack(1) {
		t.Fatal("switching to the second track was refused")
	}
	if s.segmentDone(0) {
		t.Error("segments of the old track survived the switch")
	}
	if s.AudioTrack() != 1 {
		t.Errorf("track = %d, want 1", s.AudioTrack())
	}
	if err := s.WaitSegment(ctx, 0, 90*time.Second); err != nil {
		t.Fatal(err)
	}
	init, _ := os.ReadFile(s.InitPath())
	whole := filepath.Join(t.TempDir(), "after.mp4")
	os.WriteFile(whole, append(init, segmentBytes(t, s, 0)...), 0o644)
	if high, low := bandLevel(t, ffmpeg, whole, 880), bandLevel(t, ffmpeg, whole, 440); high < low+10 {
		t.Errorf("after the switch 880 Hz is %.1f dB and 440 Hz %.1f dB, want the second track", high, low)
	}

	for _, n := range []int{1, -1, 2} {
		if s.SetAudioTrack(n) {
			t.Errorf("SetAudioTrack(%d) changed something", n)
		}
	}
	m.Close(s.ID)
	if s.SetAudioTrack(0) {
		t.Error("a closed session switched track")
	}
}
