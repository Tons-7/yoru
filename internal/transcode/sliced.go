package transcode

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// A copied picture cuts only at keyframes, counted from each pass's start, so evenly spaced segments drift apart;
// knowing the source's keyframes, segments start on them, every pass's per-keyframe files gathered alike.

// How far a fragment may start from its planned keyframe: the index and fragments agree to the millisecond.
const startSlack = 0.002

// The shortest a last segment may be: a keyframe closer to the end than this starts nothing.
const minTail = 1.0

// planSegments starts a segment at 0 and then at the first keyframe at least SegmentSeconds past the last start.
func planSegments(keys []float64, duration float64) []float64 {
	starts := []float64{0}
	for _, k := range keys {
		if k >= starts[len(starts)-1]+SegmentSeconds && duration-k >= minTail {
			starts = append(starts, k)
		}
	}
	return starts
}

// indexKeyframes plans segments from the source's keyframes, or nil to keep evenly spaced ones.
func (m *Manager) indexKeyframes(ctx context.Context, id, source string, info *MediaInfo, plan Plan) []float64 {
	// Only from disk: through the engine, the index at a file's end would be fetched ahead of its start.
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

// No encoder leaves keyframes this far apart: a gap like it is an index cut off where a download's pieces stop.
const maxKeyframeGap = 30.0

// coversEpisode: the index runs from the start to near the end without a gap, or it was read before it arrived.
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

// sliced: segments start on the source's keyframes (see above), not every SegmentSeconds.
func (s *Session) sliced() bool { return s.starts != nil }

// segmentStart is where segment n begins in the source's time.
func (s *Session) segmentStart(n int) float64 {
	if !s.sliced() {
		return float64(n) * SegmentSeconds
	}
	if n >= len(s.starts) {
		return s.Info.Duration
	}
	return s.starts[max(n, 0)]
}

// SegmentAt is the segment time t falls in, as the player would ask for it.
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

// partPath is a sliced pass's file j, one keyframe to the next.
func (s *Session) partPath(r *run, j int) string {
	return filepath.Join(s.dir, fmt.Sprintf("p%d_%d.mp4", r.id, j))
}

// gather turns a sliced pass's per-keyframe files into segments as each completes.
func (s *Session) gather(r *run) {
	defer close(r.gathered)
	var tracks map[uint32]fmp4Track
	n, j := r.from, 0
	var parts []string
	for {
		exited := closedChan(r.exited)
		cur := s.partPath(r, j)
		// ffmpeg opens a file only once the one before is whole; the last is whole when the pass has ended.
		if !fileReady(s.partPath(r, j+1)) && !(exited && fileReady(cur)) {
			if exited {
				break
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if tracks == nil {
			// Any pass's init: the copied picture's track is the same in all of them, so it times parts alike.
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
		data, err := os.ReadFile(cur)
		if err != nil {
			break
		}
		at, ok := pictureStart(tracks, data)
		j++
		// The seek landed on the keyframe before the segment asked for: that lead-in belongs to no segment here.
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
	// A pass that read to the end leaves the last segment; one killed or cut short leaves only a part of one.
	if r.clean.Load() && len(parts) > 0 {
		if n != s.Segments-1 {
			s.log.Warn("keyframe index planned past the picture", "session", s.ID, "ended", n, "segments", s.Segments)
		}
		s.publish(r, n, parts)
		parts = nil
	}
	// Published parts belong to their segment now; only what never became one goes.
	removeFiles(parts)
	for ; fileReady(s.partPath(r, j)); j++ {
		os.Remove(s.partPath(r, j))
	}
}

// sliceSegment is a finished sliced segment: the pass that made it and its parts, in order, served as they are.
type sliceSegment struct {
	by    *run
	parts []string
	at    time.Time
}

// publish makes segment n its parts, replacing any earlier pass's; nothing is copied.
func (s *Session) publish(r *run, n int, parts []string) {
	s.mu.Lock()
	// Checked under the lock the wipes take, so a stopped pass's segment lands before one or not at all.
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
	// After the swap: a reader that opened the old parts reads on, the files going once it lets go.
	removeFiles(old.parts)
}

// segmentDone: segment n can be served whole, whichever pass made it.
func (s *Session) segmentDone(n int) bool {
	if !s.sliced() {
		return segmentReady(s.SegmentPath(n))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.segs[n]
	return ok
}

// madeByLocked: pass r made segment n. The caller holds s.mu.
func (s *Session) madeByLocked(r *run, n int) bool {
	if !s.sliced() {
		return r.wrote(s.SegmentPath(n))
	}
	seg, ok := s.segs[n]
	return ok && seg.by == r
}

// dropSegments forgets every segment and removes its files, for a plan whose output no longer matches.
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

// OpenSegment opens segment n as it is sent: its file, or a sliced segment's parts end to end.
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
	// Opened under the lock: a replacement takes it before removing these, so all of them are still there.
	return openParts(seg.parts, seg.at)
}

func removeFiles(paths []string) {
	for _, p := range paths {
		os.Remove(p)
	}
}

// targetDuration is the playlist's EXT-X-TARGETDURATION: the longest segment, rounded up.
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
