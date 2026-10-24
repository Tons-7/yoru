// Package skipfind finds an opening or ending as the stretch of sound two episodes share, from chromaprint
// fingerprints of the windows where they sit, as Jellyfin's Intro Skipper does.
package skipfind

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/bits"
	"os/exec"

	"yoru/internal/proc"
)

// Point is the time one chromaprint item covers: 1365 samples at its 11025 Hz.
const Point = 1365.0 / 11025.0

// Where each part is looked for: openings in the first minutes, endings in the last few.
const (
	OpeningWindow = 10 * 60.0
	EndingWindow  = 4 * 60.0
)

// A song is 20 s to a little over two minutes; shorter matches are a shared jingle, longer a recap.
const (
	minSpan = 20.0
	maxSpan = 150.0
)

// Items match when this few of their 32 bits differ; one song in two encodes stays within it.
const maxBitErrors = 6

// A run survives gaps this long (a sound effect over the song), in items: about 3.5 s.
const maxGap = 28

// Fingerprint is chromaprint's items for length seconds of path from start, read at below-normal priority.
func Fingerprint(ctx context.Context, ffmpeg, path string, start, length float64) ([]uint32, error) {
	cmd := proc.Background(exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.3f", max(0, start)), "-t", fmt.Sprintf("%.3f", length), "-i", path,
		"-vn", "-sn", "-ac", "1", "-f", "chromaprint", "-fp_format", "raw", "pipe:1"))
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("fingerprint: %v: %s", err, bytes.TrimSpace(stderr.Bytes()))
	}
	raw := out.Bytes()
	items := make([]uint32, len(raw)/4)
	for i := range items {
		items[i] = binary.LittleEndian.Uint32(raw[i*4:])
	}
	return items, nil
}

// Match is one stretch two fingerprints share, in items from each one's start.
type Match struct{ AStart, AEnd, BStart, BEnd int }

// Seconds is the match's length.
func (m Match) Seconds() float64 { return float64(m.AEnd-m.AStart) * Point }

// Shared finds the longest stretch a and b share at one offset, if it runs as long as a song.
func Shared(a, b []uint32) (Match, bool) {
	var best Match
	bestShift := 0
	for shift := -(len(b) - 1); shift < len(a); shift++ {
		// a[i] is b[i-shift].
		from, to := max(0, shift), min(len(a), len(b)+shift)
		runStart, last := -1, -1
		for i := from; i < to; i++ {
			if !matches(a, b, i, shift) {
				continue
			}
			if runStart < 0 || i-last > maxGap {
				runStart = i
			}
			last = i
			if last-runStart > best.AEnd-best.AStart {
				best, bestShift = Match{AStart: runStart, AEnd: last + 1}, shift
			}
		}
	}
	best = trim(a, b, best, bestShift)
	best.BStart, best.BEnd = best.AStart-bestShift, best.AEnd-bestShift
	return best, best.Seconds() >= minSpan && best.Seconds() <= maxSpan
}

// matches: a[i] and b[i-shift] are the same sound. An item repeating the one before is silence or a held tone,
// alike everywhere, so no evidence.
func matches(a, b []uint32, i, shift int) bool {
	return bits.OnesCount32(a[i]^b[i-shift]) <= maxBitErrors && (i == 0 || a[i] != a[i-1])
}

// trim moves a run's ends in to where matches turn dense: the gaps a run bridges let a stray match before the
// song, or after it, stretch it by seconds.
func trim(a, b []uint32, m Match, shift int) Match {
	const window = 16 // about 2 s
	dense := func(from int) bool {
		hits := 0
		for i := from; i < from+window && i < m.AEnd; i++ {
			if matches(a, b, i, shift) {
				hits++
			}
		}
		return hits*2 >= window
	}
	for m.AStart < m.AEnd-window && !dense(m.AStart) {
		m.AStart++
	}
	for m.AEnd-window > m.AStart && !dense(m.AEnd-window) {
		m.AEnd--
	}
	return m
}

// Covers finds a known song (print) in an episode's window: most of it has to be there.
func Covers(print, window []uint32) (Match, bool) {
	m, ok := Shared(print, window)
	return m, ok && float64(m.AEnd-m.AStart) >= 0.8*float64(len(print))
}
