package skipfind

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"testing"
)

// quietEpisode is 10 s of one tune, half a second of silence from 10 s, then 10 s of another.
func quietEpisode(t *testing.T, ff string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "episode.mka")
	cmd := exec.Command(ff, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "sine=f=440:r=44100:d=10", "-f", "lavfi", "-i", "aevalsrc=0:s=44100:d=0.5",
		"-f", "lavfi", "-i", "sine=f=660:r=44100:d=10",
		"-filter_complex", "[0:a][1:a][2:a]concat=n=3:v=0:a=1[a]", "-map", "[a]", "-c:a", "flac", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make episode: %v: %s", err, b)
	}
	return out
}

// A mark a second or so off lands on the song's edge: a start where sound resumes, an end where it stops.
func TestSnapFindsTheSongsEdge(t *testing.T) {
	ff := ffmpeg(t)
	path := quietEpisode(t, ff)
	for _, c := range []struct {
		at      float64
		isStart bool
		want    float64
	}{{11.4, true, 10.5}, {8.8, false, 10}} {
		from, length := SnapWindow(c.at)
		quiet, err := Silences(context.Background(), ff, path, from, length)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := Snap(quiet, c.at, c.isStart, from, from+length)
		if !ok || math.Abs(got-c.want) > 0.05 {
			t.Errorf("snap %.1f (start %v) = %.2f, %v; want %.1f from %+v", c.at, c.isStart, got, ok, c.want, quiet)
		}
	}
}

// Dialogue's pauses come seconds apart; a song's edge has unbroken sound on its side.
func TestSnapSkipsPausesInDialogue(t *testing.T) {
	quiet := []Quiet{{Start: 16.8, End: 17.2}, {Start: 18.6, End: 19.2}, {Start: 20.1, End: 20.5}, {Start: 22.5, End: 23}}
	// 20.5 has a pause 2 s after it; 23 has 3 s of sound after it, but is 3 s past the mark.
	if got, ok := Snap(quiet, 20, true, 14.5, 25.5); !ok || got != 23 {
		t.Errorf("start = %v, %v; want 23, the only edge with a song after it", got, ok)
	}
	if got, ok := Snap(quiet, 21, false, 15.5, 26.5); ok {
		t.Errorf("end = %v; want none: every quiet is close to another", got)
	}
	// Cut off by the window: the quiet may have begun earlier.
	if got, ok := Snap([]Quiet{{Start: 10, End: 10.4}}, 11, false, 10, 16); ok {
		t.Errorf("end at the window's edge = %v; want the mark kept", got)
	}
}

func TestParseSilences(t *testing.T) {
	log := "[silencedetect @ 0x2] silence_start: -0.01\n[silencedetect @ 0x2] silence_end: 0.4 | silence_duration: 0.41\n" +
		"[silencedetect @ 0x2] silence_start: 5.5\n"
	got := parseSilences(log, 100, 6)
	want := []Quiet{{100, 100.4}, {105.5, 106}}
	if len(got) != len(want) {
		t.Fatalf("silences = %+v, want %+v", got, want)
	}
	for i := range want {
		if math.Abs(got[i].Start-want[i].Start) > 1e-9 || math.Abs(got[i].End-want[i].End) > 1e-9 {
			t.Errorf("silence %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
