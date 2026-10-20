package transcode

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// spanOf: one track's first and last time in segment n, as a browser reads them, and whether a keyframe leads.
func spanOf(t *testing.T, ffprobe string, s *Session, n int, track string) (first, last float64, key, ok bool) {
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
		"-show_entries", "packet=pts_time,flags", "-of", "csv=p=0", whole).Output()
	if err != nil {
		t.Fatalf("probe segment %d: %v", n, err)
	}
	first, last = math.Inf(1), math.Inf(-1)
	for i, line := range strings.Fields(string(out)) {
		pts, flags, _ := strings.Cut(line, ",")
		v, err := strconv.ParseFloat(pts, 64)
		if err != nil {
			continue
		}
		if i == 0 {
			key = strings.HasPrefix(flags, "K")
		}
		first, last, ok = min(first, v), max(last, v), true
	}
	return first, last, key, ok
}

// Segments start on the source's keyframes and hold the same picture whichever pass made them: in order to the
// end, after seeks, and the last made again by a pass of its own (where evenly spaced cuts repeated the end).
func TestKeyframeSegmentsAgreeAcrossPasses(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	cases := []struct {
		name, keys string
		duration   float64
	}{
		// No keyframe past the last 6 s mark: the shape that left the end replaying and stuck.
		{"keyframes every 3.5 s", "expr:gte(t,n_forced*3.5)", 181.5},
		{"keyframes 4 to 9 s apart", "0,4,11,19,26,30,35,39,46,50,57,61,70,78,87,93,98,102,109,117,126,132,137,143,152,158,167,173", 181.5},
		{"keyframes every 2.002 s", "expr:gte(t,n_forced*2.002)", 181.5},
		{"keyframes every 10 s", "expr:gte(t,n_forced*10)", 181.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			clip := filepath.Join(t.TempDir(), "clip.mkv")
			if o, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
				"-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=320x180:rate=24000/1001:duration=%v", c.duration-0.5),
				"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:sample_rate=48000:duration=%v", c.duration),
				"-c:v", "libx264", "-preset", "fast", "-bf", "2", "-pix_fmt", "yuv420p",
				"-g", "100000", "-sc_threshold", "0", "-force_key_frames", c.keys,
				"-c:a", "eac3", "-ac", "2", clip).CombinedOutput(); err != nil {
				t.Fatalf("build clip: %v\n%s", err, o)
			}

			m := NewManager(ffmpeg, ffprobe, t.TempDir(), "libx264", slog.New(slog.NewTextHandler(testWriter{t}, nil)))
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
			defer cancel()
			s, err := m.Open(ctx, "slots", clip)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close("slots")
			if !s.sliced() {
				t.Fatalf("segments should start on keyframes: plan %+v", s.CurrentPlan())
			}
			total := 0.0
			for line := range strings.SplitSeq(s.Playlist(), "\n") {
				if d, ok := strings.CutPrefix(line, "#EXTINF:"); ok {
					v, _ := strconv.ParseFloat(strings.TrimSuffix(d, ","), 64)
					total += v
				}
			}
			if math.Abs(total-s.Info.Duration) > 0.01 {
				t.Errorf("the playlist runs %.3f s, the media %.3f s", total, s.Info.Duration)
			}
			if _, err := s.WaitInit(ctx, 0, time.Minute); err != nil {
				t.Fatal(err)
			}

			last := s.Segments - 1
			seen := map[int][2]float64{}
			check := func(n int, how string) {
				t.Helper()
				if err := s.WaitSegment(ctx, n, 90*time.Second); err != nil {
					t.Fatalf("%s: segment %d: %v", how, n, err)
				}
				from, to := s.segmentStart(n), s.segmentStart(n+1)
				vf, vl, key, ok := spanOf(t, ffprobe, s, n, "v:0")
				if !ok || !key || math.Abs(vf-from) > 0.002 || vl >= to {
					t.Errorf("%s: segment %d's picture runs %.3f-%.3f (keyframe first %v), planned %.3f-%.3f", how, n, vf, vl, key, from, to)
				}
				if before, ok := seen[n]; ok && (math.Abs(before[0]-vf) > 0.002 || math.Abs(before[1]-vl) > 0.002) {
					t.Errorf("%s: segment %d holds %.3f-%.3f, another pass made it %.3f-%.3f", how, n, vf, vl, before[0], before[1])
				}
				seen[n] = [2]float64{vf, vl}
			}

			// In order to the end, as a viewer watching through: one pass reaches the end first.
			for n := 0; n <= last; n++ {
				check(n, "in order")
			}
			// Served from the parts as ffmpeg wrote them: no segment is ever copied into a file of its own.
			if copies, _ := filepath.Glob(filepath.Join(s.dir, "[0-9]*.mp4")); len(copies) > 0 {
				t.Errorf("segments were copied into files of their own: %v", copies)
			}
			// The end made again by passes of its own: a drag near the end, then the last alone.
			for _, n := range []int{last - 2, last - 1, last} {
				forgetSegment(s, n)
			}
			for _, n := range []int{last - 2, last - 1, last} {
				check(n, "after a drag to the end")
			}
			forgetSegment(s, last)
			check(last, "the last alone")
			// A seek mid-file and back.
			for _, n := range []int{last / 2, last/2 + 1, 3, 4} {
				forgetSegment(s, n)
			}
			for _, n := range []int{last / 2, last/2 + 1, 3, 4} {
				check(n, "a seek")
			}
		})
	}
}
