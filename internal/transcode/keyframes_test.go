package transcode

import (
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// probedKeyframes is every video keyframe time ffprobe sees, the truth the index is held to.
func probedKeyframes(t *testing.T, ffprobe, file string) []float64 {
	t.Helper()
	out, err := exec.Command(ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries", "packet=pts_time,flags",
		"-of", "csv=p=0", file).Output()
	if err != nil {
		t.Fatal(err)
	}
	var keys []float64
	for line := range strings.FieldsSeq(string(out)) {
		pts, flags, _ := strings.Cut(line, ",")
		if v, err := strconv.ParseFloat(pts, 64); err == nil && strings.HasPrefix(flags, "K") {
			keys = append(keys, v)
		}
	}
	return keys
}

func sameTimes(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 0.0005 {
			return false
		}
	}
	return true
}

// An index whose last pieces have not arrived reads as zeros past some point; it must not plan the episode.
func TestHalfArrivedIndexIsRefused(t *testing.T) {
	ffmpeg, _ := binaries(t)
	file := filepath.Join(t.TempDir(), "clip.mkv")
	if o, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x90:rate=24:duration=120",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "96", file).CombinedOutput(); err != nil {
		t.Fatalf("build clip: %v\n%s", err, o)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyframesOf(context.Background(), file)
	if err != nil || coversEpisode(keys, 120) != nil {
		t.Fatalf("the whole index should plan the episode: %v, %v", err, coversEpisode(keys, 120))
	}
	cues := strings.LastIndex(string(data), "\x1c\x53\xbb\x6b")
	if cues < 0 {
		t.Fatal("no Cues element")
	}
	from := cues + (len(data)-cues)/2
	clear(data[from:])
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}
	keys, err = keyframesOf(context.Background(), file)
	if err == nil {
		err = coversEpisode(keys, 120)
	}
	if !errors.Is(err, errNoIndex) {
		t.Errorf("a half-arrived index should be refused, got %d keyframes and %v", len(keys), err)
	}
}

// Segments start at 0 and on keyframes at least SegmentSeconds apart, never on one too near the end.
func TestPlanSegmentsAndLookups(t *testing.T) {
	starts := planSegments([]float64{0, 2, 4, 6.5, 8, 13, 14, 24, 31, 35.5}, 36)
	if want := []float64{0, 6.5, 13, 24, 31}; !sameTimes(starts, want) {
		t.Fatalf("planned %v, want %v", starts, want)
	}
	s := &Session{Info: &MediaInfo{Duration: 36}, starts: starts, Segments: len(starts)}
	for _, c := range []struct {
		at   float64
		want int
	}{{0, 0}, {6.49, 0}, {6.5, 1}, {20, 2}, {35.9, 4}, {99, 4}} {
		if got := s.SegmentAt(c.at); got != c.want {
			t.Errorf("SegmentAt(%v) = %d, want %d", c.at, got, c.want)
		}
	}
	if got := s.segmentStart(5); got != 36 {
		t.Errorf("past the last segment starts the end, got %v", got)
	}
	even := &Session{Info: &MediaInfo{Duration: 36}, Segments: 6}
	if even.SegmentAt(13) != 2 || even.segmentStart(2) != 12 {
		t.Errorf("evenly spaced: SegmentAt(13) = %d, segmentStart(2) = %v", even.SegmentAt(13), even.segmentStart(2))
	}
}

// Real releases, when named: YORU_REAL_MKV="a.mkv;b.mkv" holds each one's index to its packets.
func TestRealMatroskaKeyframes(t *testing.T) {
	files := os.Getenv("YORU_REAL_MKV")
	if files == "" {
		t.Skip("YORU_REAL_MKV names no files")
	}
	_, ffprobe := binaries(t)
	for file := range strings.SplitSeq(files, ";") {
		got, err := keyframesOf(context.Background(), file)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(file), err)
			continue
		}
		want := probedKeyframes(t, ffprobe, file)
		t.Logf("%s: %d in the index, %d in the packets", filepath.Base(file), len(got), len(want))
		if !sameTimes(got, want) {
			t.Errorf("%s: the index and the packets differ", filepath.Base(file))
		}
	}
}

// The Cues give every keyframe ffprobe finds, wherever the muxer put them; a file without them says so.
func TestMatroskaKeyframesMatchThePackets(t *testing.T) {
	ffmpeg, ffprobe := binaries(t)
	dir := t.TempDir()
	build := func(name string, extra ...string) string {
		out := filepath.Join(dir, name)
		args := []string{"-hide_banner", "-loglevel", "error", "-y",
			"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24000/1001:duration=50",
			"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=50",
			"-c:v", "libx264", "-preset", "medium", "-bf", "3", "-pix_fmt", "yuv420p",
			"-g", "100000", "-sc_threshold", "0", "-force_key_frames", "0,4,11,19,26,30,35,39,46",
			"-c:a", "aac"}
		args = append(append(args, extra...), out)
		if o, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, o)
		}
		return out
	}

	for _, c := range []struct {
		name  string
		extra []string
	}{
		{"cues-at-end.mkv", nil},
		{"cues-at-front.mkv", []string{"-reserve_index_space", "64k"}},
	} {
		file := build(c.name, c.extra...)
		got, err := keyframesOf(context.Background(), file)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if want := probedKeyframes(t, ffprobe, file); !sameTimes(got, want) {
			t.Errorf("%s: index %v, packets %v", c.name, got, want)
		}
	}

	// A file whose index was never written, as a muxer that could not seek leaves it.
	piped := filepath.Join(dir, "piped.mkv")
	src := build("source.mkv")
	f, err := os.Create(piped)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-i", src, "-c", "copy", "-f", "matroska", "pipe:1")
	cmd.Stdout = f
	if o, err := cmd.CombinedOutput(); err != nil && len(o) > 0 {
		t.Fatalf("pipe: %v\n%s", err, o)
	}
	f.Close()
	if _, err := keyframesOf(context.Background(), piped); !errors.Is(err, errNoIndex) {
		t.Errorf("an unindexed file should say errNoIndex, got %v", err)
	}
}
