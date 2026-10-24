package skipfind

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func ffmpeg(t *testing.T) string {
	t.Helper()
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if p := filepath.Join("..", "..", "bin", name); exists(p) {
		return p
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	t.Skip("no ffmpeg")
	return ""
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// song is the same 90 s tune in every episode; the rest is each episode's own tune.
const song = "aevalsrc='0.4*sin(2*PI*(220+55*floor(mod(t*2,12)))*t)+0.3*sin(2*PI*(330+40*floor(mod(t,7)))*t)':s=44100:d=90"

func episode(t *testing.T, ff string, before, after, seed int) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "episode.mka")
	tune := func(d, s int) string {
		step, base := strconv.Itoa(3+s%5), strconv.Itoa(180+s*23)
		return "aevalsrc='0.4*sin(2*PI*(" + base + "+31*floor(mod(t*" + step + ",11)))*t)':s=44100:d=" + strconv.Itoa(d)
	}
	cmd := exec.Command(ff, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", tune(before, seed),
		"-f", "lavfi", "-i", song,
		"-f", "lavfi", "-i", tune(after, seed+1),
		"-filter_complex", "[0:a][1:a][2:a]concat=n=3:v=0:a=1[a]", "-map", "[a]", "-c:a", "flac", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make episode: %v: %s", err, b)
	}
	return out
}

func print(t *testing.T, ff, path string, start, length float64) []uint32 {
	t.Helper()
	items, err := Fingerprint(context.Background(), ff, path, start, length)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func near(got, want float64) bool { return math.Abs(got-want) <= 2 }

// Against real releases: set REALPAIR to two episodes of one season, "a|b".
func TestRealPair(t *testing.T) {
	pair := strings.Split(os.Getenv("REALPAIR"), "|")
	if len(pair) != 2 {
		t.Skip("set REALPAIR")
	}
	ff := ffmpeg(t)
	for _, part := range []struct {
		name          string
		start, length float64
	}{{"opening", 0, OpeningWindow}, {"ending", -1, EndingWindow}} {
		began := time.Now()
		var prints [2][]uint32
		for i, path := range pair {
			start := part.start
			if start < 0 {
				start = duration(t, ff, path) - part.length
			}
			prints[i] = print(t, ff, path, start, part.length)
		}
		m, ok := Shared(prints[0], prints[1])
		t.Logf("%s: ok=%v a=%.1f-%.1f b=%.1f-%.1f (%.1fs) in %s", part.name, ok,
			float64(m.AStart)*Point, float64(m.AEnd)*Point, float64(m.BStart)*Point, float64(m.BEnd)*Point,
			m.Seconds(), time.Since(began).Round(time.Millisecond))
	}
}

func duration(t *testing.T, ff, path string) float64 {
	t.Helper()
	out, _ := exec.Command(ff, "-hide_banner", "-i", path).CombinedOutput()
	m := regexp.MustCompile(`Duration: (\d+):(\d+):([\d.]+)`).FindStringSubmatch(string(out))
	if m == nil {
		t.Fatalf("no duration for %s", path)
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	s, _ := strconv.ParseFloat(m[3], 64)
	return float64(h*3600+mi*60) + s
}

// Two episodes with the opening at different times: found in both, to within two seconds.
func TestTheSharedSongIsFoundInBoth(t *testing.T) {
	ff := ffmpeg(t)
	a, b := episode(t, ff, 30, 100, 1), episode(t, ff, 70, 60, 7)
	pa := print(t, ff, a, 0, OpeningWindow)
	m, ok := Shared(pa, print(t, ff, b, 0, OpeningWindow))
	if !ok {
		t.Fatalf("no song found: best %+v (%.1fs)", m, m.Seconds())
	}
	if at, bt := float64(m.AStart)*Point, float64(m.BStart)*Point; !near(at, 30) || !near(bt, 70) || !near(m.Seconds(), 90) {
		t.Errorf("found at %.1fs and %.1fs for %.1fs, want 30, 70 and 90", at, bt, m.Seconds())
	}

	// Kept as a print, the song is found in a third episode on its own.
	c := episode(t, ff, 12, 120, 13)
	if got, ok := Covers(pa[m.AStart:m.AEnd], print(t, ff, c, 0, OpeningWindow)); !ok || !near(float64(got.BStart)*Point, 12) {
		t.Errorf("the kept print found %+v (ok %v), want the song at 12s", got, ok)
	}

	// The episodes' own sound shares nothing.
	if got, ok := Shared(print(t, ff, a, 125, 60), print(t, ff, b, 165, 60)); ok {
		t.Errorf("unrelated sound matched: %+v", got)
	}
}
