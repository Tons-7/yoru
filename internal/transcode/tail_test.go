package transcode

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"yoru/internal/sound"
)

// Every segment up to the last arrives, asked in order as hls.js does, on sources with an awkward end.
func TestTheLastSegmentArrives(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	cases := []struct {
		name         string
		video, audio float64
		gop          int
		codec        string
		headphones   bool
		subsTo       float64
	}{
		{"keyframes 10 s apart", 63.4, 63.4, 240, "aac", false, 0},
		{"sound past the picture", 60, 62.5, 48, "aac", false, 0},
		{"picture past the sound", 62.5, 60, 48, "aac", false, 0},
		{"E-AC-3 re-encoded, headphones on", 63.4, 64, 240, "eac3", true, 0},
		{"half a second past the last mark", 60.5, 60.5, 48, "aac", false, 0},
		{"subtitles past the picture and sound", 60, 60, 48, "aac", false, 61.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			clip := filepath.Join(dir, "clip.mkv")
			args := []string{"-hide_banner", "-loglevel", "error", "-y",
				"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=640x360:rate=24:duration=%v", c.video),
				"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:sample_rate=48000:duration=%v", c.audio)}
			if c.subsTo > 0 {
				ass := filepath.Join(dir, "subs.ass")
				line := fmt.Sprintf("Dialogue: 0,0:00:01.00,0:%02d:%05.2f,Default,,0,0,0,,last", int(c.subsTo)/60, c.subsTo-float64(int(c.subsTo)/60*60))
				if err := os.WriteFile(ass, []byte("[Script Info]\nScriptType: v4.00+\n\n[V4+ Styles]\nFormat: Name, Fontname, Fontsize\nStyle: Default,Arial,40\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n"+line+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				args = append(args, "-i", ass, "-map", "0:v", "-map", "1:a", "-map", "2:s", "-c:s", "ass")
			}
			args = append(args, "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
				"-g", fmt.Sprint(c.gop), "-keyint_min", fmt.Sprint(c.gop), "-sc_threshold", "0",
				"-c:a", c.codec, "-ac", "2", clip)
			if o, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
				t.Fatalf("build clip: %v\n%s", err, o)
			}

			m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", slog.New(slog.NewTextHandler(testWriter{t}, nil)))
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			s, err := m.Open(ctx, "tail", clip)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close("tail")
			if c.headphones {
				s.SetSound(sound.Options{Headphones: true})
			}
			t.Logf("plan %+v, duration %.2f, %d segments", s.CurrentPlan(), s.Info.Duration, s.Segments)

			if _, err := s.WaitInit(ctx, 0, 60*time.Second); err != nil {
				t.Fatalf("init: %v", err)
			}
			for n := range s.Segments {
				began := time.Now()
				if err := s.WaitSegment(ctx, n, 60*time.Second); err != nil {
					t.Fatalf("segment %d of %d never came (after %s): %v", n, s.Segments, time.Since(began).Round(time.Millisecond), err)
				}
			}
			// The last segment runs to the end, less at most the sub-second sliver segmentCount folds into it.
			last := max(segmentEnd(t, ffprobe, s, s.Segments-1, "v:0"), segmentEnd(t, ffprobe, s, s.Segments-1, "a:0"))
			if last < s.Info.Duration-1 {
				t.Errorf("the last segment ends at %.3f, the file at %.3f", last, s.Info.Duration)
			}
		})
	}
}

// segmentEnd is the last time one track reaches in segment n, as the browser reads it; -1 for a track it lacks.
func segmentEnd(t *testing.T, ffprobe string, s *Session, n int, track string) float64 {
	t.Helper()
	init, err := os.ReadFile(s.InitPath())
	if err != nil {
		t.Fatal(err)
	}
	seg := segmentBytes(t, s, n)
	whole := filepath.Join(t.TempDir(), fmt.Sprint(n, ".mp4"))
	if err := os.WriteFile(whole, append(init, seg...), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(ffprobe, "-v", "error", "-select_streams", track,
		"-show_entries", "packet=pts_time", "-of", "csv=p=0", whole).Output()
	if err != nil {
		t.Fatalf("probe segment %d: %v", n, err)
	}
	end := -1.0
	for line := range strings.FieldsSeq(string(out)) {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(line, ","), 64); err == nil {
			end = max(end, v)
		}
	}
	return end
}
