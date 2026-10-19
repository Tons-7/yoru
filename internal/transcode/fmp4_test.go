package transcode

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pictureStart agrees with ffprobe on where each sliced fragment's picture starts, B-frames and a seek included.
func TestPictureStartMatchesFFprobe(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	ffmpeg = absolute(ffmpeg)
	dir := t.TempDir()
	clip := filepath.Join(dir, "clip.mkv")
	if o, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24000/1001:duration=40",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=40",
		"-c:v", "libx264", "-preset", "medium", "-bf", "3", "-pix_fmt", "yuv420p",
		"-g", "100000", "-sc_threshold", "0", "-force_key_frames", "0,3,9.5,11,17,26,30,35",
		"-c:a", "aac", clip).CombinedOutput(); err != nil {
		t.Fatalf("build clip: %v\n%s", err, o)
	}

	for _, seek := range []string{"", "17.0175"} {
		out := filepath.Join(dir, "out"+seek)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-seek_timestamp", "1"}
		if seek != "" {
			args = append(args, "-ss", seek)
		}
		args = append(args, "-i", clip, "-map", "0:v:0", "-map", "0:a:0", "-c:v", "copy", "-c:a", "aac",
			"-copyts", "-avoid_negative_ts", "disabled", "-f", "hls", "-hls_time", "0.001", "-hls_segment_type", "fmp4",
			"-hls_list_size", "1", "-hls_segment_options", "movflags=+frag_discont+skip_sidx",
			"-hls_fmp4_init_filename", "init.mp4", "-hls_segment_filename", "g_%d.mp4", "-y", "playlist.m3u8")
		cmd := exec.Command(ffmpeg, args...)
		cmd.Dir = out
		if o, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("slice: %v\n%s", err, o)
		}
		init, err := os.ReadFile(filepath.Join(out, "init.mp4"))
		if err != nil {
			t.Fatal(err)
		}
		tracks, err := initTracks(init)
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; ; j++ {
			seg, err := os.ReadFile(filepath.Join(out, fmt.Sprintf("g_%d.mp4", j)))
			if err != nil {
				if j < 3 {
					t.Fatalf("only %d fragments", j)
				}
				break
			}
			got, ok := pictureStart(tracks, seg)
			whole := filepath.Join(out, "whole.mp4")
			if err := os.WriteFile(whole, append(append([]byte{}, init...), seg...), 0o644); err != nil {
				t.Fatal(err)
			}
			probe, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "packet=pts_time",
				"-of", "csv=p=0", whole).Output()
			if err != nil {
				t.Fatal(err)
			}
			want, _ := strconv.ParseFloat(strings.TrimSuffix(strings.Fields(string(probe))[0], ","), 64)
			t.Logf("seek %q fragment %d: ours %.3f, ffprobe %.3f", seek, j, got, want)
			if !ok || math.Abs(got-want) > 0.0005 {
				t.Errorf("seek %q fragment %d: picture starts at %.4f by our reading, %.4f by ffprobe's", seek, j, got, want)
			}
		}
	}
}
