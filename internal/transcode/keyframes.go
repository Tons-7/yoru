package transcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

// Matroska element IDs read here, marker bits kept as written.
const (
	mkvEBML           = 0x1A45DFA3
	mkvSegment        = 0x18538067
	mkvSeekHead       = 0x114D9B74
	mkvSeek           = 0x4DBB
	mkvSeekID         = 0x53AB
	mkvSeekPosition   = 0x53AC
	mkvInfo           = 0x1549A966
	mkvTimestampScale = 0x2AD7B1
	mkvTracks         = 0x1654AE6B
	mkvTrackEntry     = 0xAE
	mkvTrackNumber    = 0xD7
	mkvTrackType      = 0x83
	mkvCues           = 0x1C53BB6B
	mkvCuePoint       = 0xBB
	mkvCueTime        = 0xB3
	mkvCuePositions   = 0xB7
	mkvCueTrack       = 0xF7
	mkvCluster        = 0x1F43B675
)

// errNoIndex: the file has no keyframe index to plan from; the caller keeps evenly spaced segments.
var errNoIndex = errors.New("no keyframe index")

// Bounds against a damaged file: no real header or index comes near these.
const (
	maxMetaBytes = 4 << 20
	maxCueBytes  = 32 << 20
)

// keyframesOf reads the first video track's keyframe times, in seconds, from a Matroska file's Cues.
func keyframesOf(ctx context.Context, source string) ([]float64, error) {
	r, size, closer, err := openReaderAt(ctx, source)
	if err != nil {
		return nil, err
	}
	defer closer()
	return matroskaKeyframes(r, size)
}

func matroskaKeyframes(r io.ReaderAt, size int64) ([]float64, error) {
	top, err := matroskaTop(r, size)
	if err != nil {
		return nil, err
	}
	track := uint64(0)
	if at, ok := top.where[mkvTracks]; ok {
		if body, err := readBody(r, at, maxMetaBytes); err == nil {
			for _, entry := range children(body, mkvTrackEntry) {
				var number, kind uint64
				for _, f := range fields(entry) {
					switch f.id {
					case mkvTrackNumber:
						number = uintOf(f.data)
					case mkvTrackType:
						kind = uintOf(f.data)
					}
				}
				if kind == 1 && number > 0 {
					track = number
					break
				}
			}
		}
	}
	at, ok := top.where[mkvCues]
	if !ok || track == 0 {
		return nil, errNoIndex
	}
	body, err := readBody(r, at, maxCueBytes)
	if err != nil {
		return nil, errNoIndex
	}

	var times []float64
	for _, point := range children(body, mkvCuePoint) {
		var when uint64
		video := false
		for _, f := range fields(point) {
			switch f.id {
			case mkvCueTime:
				when = uintOf(f.data)
			case mkvCuePositions:
				for _, p := range fields(f.data) {
					if p.id == mkvCueTrack && uintOf(p.data) == track {
						video = true
					}
				}
			}
		}
		if video {
			times = append(times, float64(when)*float64(top.scale)/1e9)
		}
	}
	if len(times) < 2 {
		return nil, errNoIndex
	}
	slices.Sort(times)
	return slices.Compact(times), nil
}

// mkvTop is a Matroska file's layout before its first Cluster.
type mkvTop struct {
	// ebmlEnd: where the EBML header ends; seg: where the Segment's body starts.
	ebmlEnd, seg, segEnd int64
	// where: each top-level element's offset, from the SeekHead and the elements before the first Cluster.
	where map[uint64]int64
	// Nanoseconds per timestamp tick.
	scale uint64
}

func matroskaTop(r io.ReaderAt, size int64) (*mkvTop, error) {
	id, ebmlLen, n, err := elementAt(r, 0)
	if err != nil || id != mkvEBML {
		return nil, errNoIndex
	}
	id, segSize, head, err := elementAt(r, n+ebmlLen)
	if err != nil || id != mkvSegment {
		return nil, errNoIndex
	}
	seg := n + ebmlLen + head
	segEnd := size
	if segSize >= 0 && seg+segSize < size {
		segEnd = seg + segSize
	}

	// Where Info, Tracks and Cues are: the SeekHead says, and the top level before the first Cluster shows.
	where := map[uint64]int64{}
	var seekHeads []int64
	for at := seg; at < segEnd; {
		id, length, head, err := elementAt(r, at)
		if err != nil || length < 0 {
			break
		}
		if _, ok := where[id]; !ok {
			where[id] = at
		}
		if id == mkvSeekHead {
			seekHeads = append(seekHeads, at)
		}
		if id == mkvCluster {
			break
		}
		at += head + length
	}
	for i := 0; i < len(seekHeads) && i < 4; i++ {
		body, err := readBody(r, seekHeads[i], maxMetaBytes)
		if err != nil {
			continue
		}
		for _, seek := range children(body, mkvSeek) {
			sid, pos := uint64(0), int64(-1)
			for _, f := range fields(seek) {
				switch f.id {
				case mkvSeekID:
					for _, b := range f.data {
						sid = sid<<8 | uint64(b)
					}
				case mkvSeekPosition:
					pos = int64(uintOf(f.data))
				}
			}
			if pos < 0 || seg+pos >= segEnd {
				continue
			}
			if _, ok := where[sid]; !ok {
				where[sid] = seg + pos
			}
			if sid == mkvSeekHead && !slices.Contains(seekHeads, seg+pos) {
				seekHeads = append(seekHeads, seg+pos)
			}
		}
	}

	scale := uint64(1_000_000)
	if at, ok := where[mkvInfo]; ok {
		if body, err := readBody(r, at, maxMetaBytes); err == nil {
			for _, f := range fields(body) {
				if f.id == mkvTimestampScale {
					if v := uintOf(f.data); v > 0 {
						scale = v
					}
				}
			}
		}
	}
	return &mkvTop{ebmlEnd: n + ebmlLen, seg: seg, segEnd: segEnd, where: where, scale: scale}, nil
}

type field struct {
	id   uint64
	data []byte
}

// fields splits an element's body into its children; a damaged one ends the list.
func fields(buf []byte) []field {
	var out []field
	for len(buf) > 0 {
		id, n := vint(buf, true)
		if n == 0 {
			return out
		}
		length, m := vint(buf[n:], false)
		if m == 0 || length < 0 || int64(n+m)+length > int64(len(buf)) {
			return out
		}
		out = append(out, field{id: uint64(id), data: buf[n+m : int64(n+m)+length]})
		buf = buf[int64(n+m)+length:]
	}
	return out
}

func children(buf []byte, id uint64) [][]byte {
	var out [][]byte
	for _, f := range fields(buf) {
		if f.id == id {
			out = append(out, f.data)
		}
	}
	return out
}

func uintOf(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// vint reads an EBML variable-length integer: an ID keeps its marker bits, a size drops them (-1 when unknown).
func vint(buf []byte, keepMarker bool) (int64, int) {
	if len(buf) == 0 || buf[0] == 0 {
		return 0, 0
	}
	n := 1
	for mask := byte(0x80); buf[0]&mask == 0; mask >>= 1 {
		n++
	}
	if n > 8 || len(buf) < n {
		return 0, 0
	}
	v := uint64(buf[0])
	if !keepMarker {
		v &= uint64(0xff >> n)
	}
	allOnes := v == uint64(0xff>>n)
	for _, b := range buf[1:n] {
		v = v<<8 | uint64(b)
		allOnes = allOnes && b == 0xff
	}
	if !keepMarker && allOnes {
		return -1, n
	}
	return int64(v), n
}

// elementAt reads the header of the element at off: its ID, body length (-1 unknown) and header length.
func elementAt(r io.ReaderAt, off int64) (uint64, int64, int64, error) {
	var buf [16]byte
	got, err := r.ReadAt(buf[:], off)
	if got == 0 {
		return 0, 0, 0, err
	}
	id, n := vint(buf[:got], true)
	if n == 0 {
		return 0, 0, 0, errNoIndex
	}
	length, m := vint(buf[n:got], false)
	if m == 0 {
		return 0, 0, 0, errNoIndex
	}
	return uint64(id), length, int64(n + m), nil
}

// readBody reads the body of the element at off, refusing one larger than limit.
func readBody(r io.ReaderAt, off, limit int64) ([]byte, error) {
	_, length, head, err := elementAt(r, off)
	if err != nil {
		return nil, err
	}
	if length < 0 || length > limit {
		return nil, errNoIndex
	}
	body := make([]byte, length)
	if _, err := r.ReadAt(body, off+head); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return body, nil
}

// openReaderAt opens a file on disk, or a URL read by byte ranges, as the encoder would read it.
func openReaderAt(ctx context.Context, source string) (io.ReaderAt, int64, func(), error) {
	if !remote(source) {
		f, err := os.Open(source)
		if err != nil {
			return nil, 0, nil, err
		}
		info, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, 0, nil, err
		}
		return f, info.Size(), func() { f.Close() }, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, source, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.ContentLength <= 0 {
		return nil, 0, nil, fmt.Errorf("%w: %s answered %s", errNoIndex, source, res.Status)
	}
	return &rangeReader{ctx: ctx, url: source}, res.ContentLength, func() {}, nil
}

// rangeReader reads a URL by byte ranges.
type rangeReader struct {
	ctx context.Context
	url string
}

func (h *rangeReader) ReadAt(p []byte, off int64) (int, error) {
	req, err := http.NewRequestWithContext(h.ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", off, off+int64(len(p))-1))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusPartialContent {
		return 0, fmt.Errorf("%w: range read answered %s", errNoIndex, res.Status)
	}
	n, err := io.ReadFull(res.Body, p)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = io.EOF
	}
	return n, err
}

// The longest a session's open waits on the index: a download's last pieces may not have come yet.
const keyframeIndexWait = 4 * time.Second

// isMatroska: the container the index reader understands.
func isMatroska(container string) bool { return strings.Contains(container, "matroska") }
