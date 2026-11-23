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

const Point = 1365.0 / 11025.0

const (
	OpeningWindow = 10 * 60.0
	EndingWindow  = 4 * 60.0
)

const (
	minSpan = 20.0
	maxSpan = 150.0
)

func Usable(seconds float64) bool { return seconds >= minSpan && seconds <= maxSpan }

const maxBitErrors = 6

const maxGap = 28

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

type Match struct{ AStart, AEnd, BStart, BEnd int }

func (m Match) Seconds() float64 { return float64(m.AEnd-m.AStart) * Point }

func Shared(a, b []uint32) (Match, bool) {
	var best Match
	bestShift := 0
	for shift := -(len(b) - 1); shift < len(a); shift++ {
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
			if last+1-runStart > best.AEnd-best.AStart {
				best, bestShift = Match{AStart: runStart, AEnd: last + 1}, shift
			}
		}
	}
	best = trim(a, b, best, bestShift)
	best.BStart, best.BEnd = best.AStart-bestShift, best.AEnd-bestShift
	return best, Usable(best.Seconds())
}

func matches(a, b []uint32, i, shift int) bool {
	return bits.OnesCount32(a[i]^b[i-shift]) <= maxBitErrors && (i == 0 || a[i] != a[i-1])
}

func trim(a, b []uint32, m Match, shift int) Match {
	const window = 16
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

func Covers(print, window []uint32) (Match, bool) {
	m, ok := Shared(print, window)
	return m, ok && float64(m.AEnd-m.AStart) >= 0.8*float64(len(print))
}
