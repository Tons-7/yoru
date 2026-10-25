package skipfind

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"

	"yoru/internal/proc"
)

const (
	// SnapReach is how far either side of a marked moment a song's edge is looked for.
	SnapReach = 3.0
	// A song runs this long without a pause; dialogue rarely does, so its pauses aren't taken for an edge.
	songSide = 2.5
)

// Quiet is a stretch of silence, in seconds of the episode.
type Quiet struct{ Start, End float64 }

var (
	quietStart = regexp.MustCompile(`silence_start:\s*(-?[\d.]+)`)
	quietEnd   = regexp.MustCompile(`silence_end:\s*([\d.]+)`)
)

// SnapWindow is the stretch around a marked moment that Silences reads for Snap.
func SnapWindow(at float64) (from, length float64) {
	from = max(0, at-SnapReach-songSide)
	return from, at + SnapReach + songSide - from
}

// Silences finds the quiet stretches in length seconds of path from start, read at below-normal priority.
func Silences(ctx context.Context, ffmpeg, path string, start, length float64) ([]Quiet, error) {
	cmd := proc.Background(exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-nostats",
		"-ss", fmt.Sprintf("%.3f", start), "-t", fmt.Sprintf("%.3f", length), "-i", path,
		"-vn", "-sn", "-dn", "-af", "silencedetect=n=-45dB:d=0.25", "-f", "null", "-"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("silences: %v: %s", err, lastLine(stderr.Bytes()))
	}
	return parseSilences(stderr.String(), start, length), nil
}

// parseSilences reads silencedetect's log, whose times count from the window's start.
func parseSilences(log string, start, length float64) []Quiet {
	num := func(s string) float64 {
		f, _ := strconv.ParseFloat(s, 64)
		return start + max(0, f)
	}
	starts, ends := quietStart.FindAllStringSubmatch(log, -1), quietEnd.FindAllStringSubmatch(log, -1)
	out := make([]Quiet, 0, len(starts))
	for i, m := range starts {
		// Still quiet when the window ran out.
		end := start + length
		if i < len(ends) {
			end = num(ends[i][1])
		}
		out = append(out, Quiet{Start: num(m[1]), End: end})
	}
	return out
}

// Snap moves a mark to the song's edge nearby: where sound resumes (a start) or stops (an end); else keeps it.
func Snap(quiet []Quiet, at float64, isStart bool, from, to float64) (float64, bool) {
	best, found := at, false
	for i, q := range quiet {
		t := q.Start
		if isStart {
			t = q.End
		}
		if math.Abs(t-at) > SnapReach || !songAt(quiet, i, t, isStart, from, to) {
			continue
		}
		if !found || math.Abs(t-at) < math.Abs(best-at) {
			best, found = t, true
		}
	}
	return best, found
}

// songAt: the songSide seconds past an edge (after a start, before an end) are in the window and never quiet.
func songAt(quiet []Quiet, self int, t float64, isStart bool, from, to float64) bool {
	lo, hi := t-songSide, t
	if isStart {
		lo, hi = t, t+songSide
	}
	// Also drops an edge the window cut off, which says nothing of where the quiet really begins or ends.
	if lo < from || hi > to {
		return false
	}
	for i, q := range quiet {
		if i != self && q.Start < hi && q.End > lo {
			return false
		}
	}
	return true
}

func lastLine(b []byte) string {
	b = bytes.TrimSpace(b)
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return string(b[i+1:])
	}
	return string(b)
}
