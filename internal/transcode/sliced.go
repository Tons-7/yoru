package transcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const startSlack = 0.002

const minTail = 1.0

func planSegments(keys []float64, duration float64) []float64 {
	starts := []float64{0}
	for _, k := range keys {
		if k >= starts[len(starts)-1]+SegmentSeconds && duration-k >= minTail {
			starts = append(starts, k)
		}
	}
	return starts
}

func (m *Manager) indexKeyframes(ctx context.Context, id, source string, info *MediaInfo, plan Plan) []float64 {
	if !plan.VideoCopy || !isMatroska(info.Container) || remote(source) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, keyframeIndexWait)
	defer cancel()
	keys, err := keyframesOf(ctx, source)
	if err == nil {
		err = coversEpisode(keys, info.Duration)
	}
	if err != nil {
		m.log.Info("segments evenly spaced", "session", id, "reason", err)
		return nil
	}
	return planSegments(keys, info.Duration)
}

const maxKeyframeGap = 30.0

func coversEpisode(keys []float64, duration float64) error {
	prev := 0.0
	for _, k := range keys {
		if k-prev > maxKeyframeGap {
			return fmt.Errorf("%w: keyframes %.0f s apart at %.0f s", errNoIndex, k-prev, prev)
		}
		prev = k
	}
	if duration-prev > maxKeyframeGap {
		return fmt.Errorf("%w: the last keyframe is %.0f s before the end", errNoIndex, duration-prev)
	}
	return nil
}

func (s *Session) sliced() bool { return s.starts != nil }

func (s *Session) segmentStart(n int) float64 {
	if !s.sliced() {
		return float64(n) * SegmentSeconds
	}
	if n >= len(s.starts) {
		return s.Info.Duration
	}
	return s.starts[max(n, 0)]
}

func (s *Session) SegmentStart(n int) float64 { return s.segmentStart(n) }

func (s *Session) SegmentAt(t float64) int {
	if !s.sliced() {
		return min(int(t/SegmentSeconds), max(s.Segments-1, 0))
	}
	n, found := slices.BinarySearch(s.starts, t)
	if !found {
		n--
	}
	return min(max(n, 0), s.Segments-1)
}

func (s *Session) partPath(r *run, j int) string {
	return filepath.Join(s.dir, fmt.Sprintf("p%d_%d.mp4", r.id, j))
}

func (s *Session) gather(r *run) {
	defer close(r.gathered)
	var tracks map[uint32]fmp4Track
	n, j := r.from, 0
	var parts []string
	whole := false
	var stuckSince time.Time
	for {
		exited := closedChan(r.exited)
		cur := s.partPath(r, j)
		if !fileReady(s.partPath(r, j+1)) && !(exited && fileReady(cur)) && !segmentComplete(cur) {
			if exited {
				whole = true
				break
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if tracks == nil {
			init, err := os.ReadFile(s.InitPath())
			if err == nil {
				tracks, err = initTracks(init)
			}
			if err != nil {
				if exited {
					break
				}
				time.Sleep(50 * time.Millisecond)
				continue
			}
		}
		at, ok, err := partStart(tracks, cur)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				j++
				continue
			}
			if exited {
				if stuckSince.IsZero() {
					stuckSince = time.Now()
				}
				if time.Since(stuckSince) > 3*time.Second {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		stuckSince = time.Time{}
		j++
		if ok && at < s.segmentStart(r.from)-startSlack {
			os.Remove(cur)
			continue
		}
		if ok && n == r.from && len(parts) == 0 && at > s.segmentStart(n)+startSlack {
			s.log.Warn("pass began past its segment's keyframe", "session", s.ID, "segment", n,
				"planned", s.segmentStart(n), "found", at)
		}
		for ok && n+1 < s.Segments && at >= s.segmentStart(n+1)-startSlack {
			if at > s.segmentStart(n+1)+startSlack {
				s.log.Warn("keyframe index missed a cut", "session", s.ID, "segment", n+1,
					"planned", s.segmentStart(n+1), "found", at)
			}
			if len(parts) > 0 {
				s.publish(r, n, parts)
			}
			parts, n = nil, n+1
		}
		parts = append(parts, cur)
	}
	if whole && r.clean.Load() && len(parts) > 0 {
		if n != s.Segments-1 {
			s.log.Warn("keyframe index planned past the picture", "session", s.ID, "ended", n, "segments", s.Segments)
		}
		s.publish(r, n, parts)
		parts = nil
	}
	removeFiles(parts)
	for ; fileReady(s.partPath(r, j)); j++ {
		os.Remove(s.partPath(r, j))
	}
}

func partStart(tracks map[uint32]fmp4Track, path string) (float64, bool, error) {
	f, err := openShared(path)
	if err != nil {
		return 0, false, err
	}
	defer f.Close()
	head := make([]byte, 256<<10)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return 0, false, err
	}
	if at, ok := pictureStart(tracks, head[:n]); ok || n < len(head) {
		return at, ok, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false, err
	}
	at, ok := pictureStart(tracks, data)
	return at, ok, nil
}

type sliceSegment struct {
	by    *run
	parts []string
	at    time.Time
}

func (s *Session) publish(r *run, n int, parts []string) {
	s.mu.Lock()
	if r.discarded.Load() {
		s.mu.Unlock()
		removeFiles(parts)
		return
	}
	if s.segs == nil {
		s.segs = map[int]sliceSegment{}
	}
	old := s.segs[n]
	s.segs[n] = sliceSegment{by: r, parts: parts, at: time.Now()}
	s.mu.Unlock()
	removeFiles(old.parts)
}

func (s *Session) segmentDone(n int) bool {
	if !s.sliced() {
		return segmentReady(s.SegmentPath(n))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.segs[n]
	return ok
}

func (s *Session) madeByLocked(r *run, n int) bool {
	if !s.sliced() {
		return r.wrote(s.SegmentPath(n))
	}
	seg, ok := s.segs[n]
	return ok && seg.by == r
}

func (s *Session) dropSegments() {
	if !s.sliced() {
		for n := range s.Segments {
			os.Remove(s.SegmentPath(n))
		}
		return
	}
	s.mu.Lock()
	segs := s.segs
	s.segs = nil
	s.mu.Unlock()
	for _, seg := range segs {
		removeFiles(seg.parts)
	}
}

func (s *Session) OpenSegment(n int) (*SegmentReader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.sliced() {
		return openParts([]string{s.SegmentPath(n)}, time.Time{})
	}
	seg, ok := s.segs[n]
	if !ok {
		return nil, os.ErrNotExist
	}
	return openParts(seg.parts, seg.at)
}

func removeFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

func (s *Session) targetDuration() int {
	if !s.sliced() {
		return int(SegmentSeconds) + 1
	}
	longest := 0.0
	for n := range s.Segments {
		longest = max(longest, s.segmentStart(n+1)-s.segmentStart(n))
	}
	return int(math.Ceil(longest))
}

func closedChan(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}
