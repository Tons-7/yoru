package transcode

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Each picture change in a real pass: HDR out SDR (as is for an HDR screen), interlaced progressive, capped.
func TestPictureChangesRunInFFmpeg(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	dir := t.TempDir()
	clip := func(name string, args ...string) string {
		out := filepath.Join(dir, name)
		args = append([]string{"-hide_banner", "-loglevel", "error", "-y"}, append(args, out)...)
		if b, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", name, err, b)
		}
		return out
	}
	hdr := clip("hdr.mkv", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24:duration=6",
		"-vf", "zscale=tin=bt709:min=bt709:pin=bt709:rin=tv:t=smpte2084:m=bt2020nc:p=bt2020:r=tv:npl=203,format=yuv420p10le",
		"-c:v", "libx265", "-preset", "ultrafast",
		"-x265-params", "log-level=error:colorprim=bt2020:transfer=smpte2084:colormatrix=bt2020nc")
	woven := clip("interlaced.mkv", "-f", "lavfi", "-i", "testsrc2=size=720x480:rate=30000/1001:duration=6",
		"-vf", "setfield=tff", "-c:v", "libx264", "-preset", "ultrafast", "-flags", "+ildct+ilme")
	plain := clip("plain.mkv", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24:duration=6", "-c:v", "libx264", "-preset", "ultrafast")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", slog.New(slog.NewTextHandler(testWriter{t}, nil)))
	m.SetToneMapper(ToneMapper(ctx, ffmpeg))
	t.Logf("tone map: %q", m.toneMapper)

	for _, tt := range []struct {
		name, source string
		caps         *Caps
		want         Want
		copied       bool
		check        func(probe string) bool
	}{
		{"HDR to an SDR screen", hdr, nil, Want{}, false, func(p string) bool { return !strings.Contains(p, "smpte2084") }},
		{"HDR to an HDR screen", hdr, ParseCaps("hevc-10,hdr"), Want{}, true, func(p string) bool { return strings.Contains(p, "smpte2084") }},
		// Converted for a cap all the same, the picture comes out SDR, so it is tone-mapped even there.
		{"HDR capped for an HDR screen", hdr, ParseCaps("hevc-10, hdr"), Want{Height: 480, Rate: 1_200_000}, false,
			func(p string) bool { return !strings.Contains(p, "smpte2084") && strings.Contains(p, ",480,") }},
		{"interlaced", woven, nil, Want{}, false, func(p string) bool { return strings.Contains(p, "progressive") }},
		{"capped", plain, nil, Want{Height: 480, Rate: 1_200_000}, false, func(p string) bool { return strings.Contains(p, ",480,") }},
		{"softened", plain, nil, Want{Soften: true}, false, func(p string) bool { return strings.Contains(p, "h264") }},
		// A PC without Vulkan tone-maps on the CPU.
		{"HDR on the CPU", hdr, nil, Want{}, false, func(p string) bool { return !strings.Contains(p, "smpte2084") }},
	} {
		if tt.name == "HDR on the CPU" {
			m.SetToneMapper("zscale")
		}
		s, err := m.OpenFor(ctx, "7-"+strings.ReplaceAll(tt.name, " ", ""), tt.source, tt.source, tt.caps, tt.want)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if s.Plan.VideoCopy != tt.copied {
			t.Errorf("%s: copied %v, plan %+v", tt.name, s.Plan.VideoCopy, s.Plan)
		}
		if err := s.WaitSegment(ctx, 0, time.Minute); err != nil {
			t.Fatalf("%s: first segment: %v", tt.name, err)
		}
		init, _ := os.ReadFile(s.InitPath())
		seg, _ := os.ReadFile(s.SegmentPath(0))
		joined := filepath.Join(dir, "joined.mp4")
		if err := os.WriteFile(joined, append(init, seg...), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _ := exec.Command(ffprobe, "-v", "error", "-select_streams", "v", "-show_entries",
			"stream=codec_name,width,height,color_transfer,field_order", "-of", "csv=p=0", joined).CombinedOutput()
		if probe := strings.TrimSpace(string(out)); !tt.check(probe) {
			t.Errorf("%s: came out %q (%s)", tt.name, probe, s.Plan.Reason)
		}
		m.Close(s.ID)
	}
}
