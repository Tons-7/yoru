package transcode

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"yoru/internal/proc"
)

type Span struct{ From, To int64 }

const (
	tileRead      = 24 << 20
	keyframeReach = 20.0
	tilesPerPass  = 48
	scanChunk     = 4 << 20
	scanPerPass   = 256 << 20
)

var mkvClusterID = []byte{0x1F, 0x43, 0xB6, 0x75}

var openSegment = []byte{0x18, 0x53, 0x80, 0x67, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

type cluster struct {
	at, end int64
	time    float64
}

type partial struct {
	head             []byte
	scale            uint64
	scanned          []Span
	clusters         []cluster
	failed           map[int]time.Time
	headFailed, used time.Time
	bytes            int64
	passed           time.Time
}

const partialIdle = time.Hour

func tilePath(dir string, i int) string {
	return filepath.Join(dir, "tiles", fmt.Sprintf("%04d.jpg", i))
}

func partialSheet(dir string) string { return filepath.Join(dir, "partial.jpg") }

func partialIndex(dir string) string { return filepath.Join(dir, "partial.json") }

func (t *Thumbnails) Fill(source, dir string, duration, aspect float64, have []Span) {
	plan := PlanSheet(duration)
	if t == nil || plan.Count == 0 || source == "" || len(have) == 0 {
		return
	}
	if _, err := os.Stat(sheetPath(dir)); err == nil {
		return
	}
	key := dir + "#partial"
	t.mu.Lock()
	if _, busy := t.running[key]; busy {
		t.mu.Unlock()
		return
	}
	maps.DeleteFunc(t.partials, func(d string, q *partial) bool { return d != dir && time.Since(q.used) > partialIdle })
	p := t.partials[dir]
	if p == nil {
		p = &partial{failed: map[int]time.Time{}}
		t.partials[dir] = p
	}
	p.used = time.Now()
	var bytes int64
	for _, s := range have {
		bytes += s.To - s.From
	}
	if bytes == p.bytes && time.Since(p.passed) < partialRetry {
		t.mu.Unlock()
		return
	}
	p.bytes, p.passed = bytes, time.Now()
	t.running[key] = struct{}{}
	t.mu.Unlock()

	go func() {
		defer func() {
			t.mu.Lock()
			delete(t.running, key)
			t.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), thumbDeadline)
		defer cancel()
		height := tileHeight(aspect)
		made, err := t.fill(ctx, p, source, dir, plan, height, have)
		if err != nil {
			t.log.Debug("scrub previews so far", "err", err)
		}
		if _, err := os.Stat(sheetPath(dir)); err == nil {
			t.dropPartial(dir)
			return
		}
		if made == 0 {
			return
		}
		if err := layOut(dir, plan, height); err != nil {
			t.log.Warn("scrub previews so far", "err", err)
		}
	}()
}

func (t *Thumbnails) fill(ctx context.Context, p *partial, source, dir string, plan Sheet, height int, have []Span) (int, error) {
	f, err := os.Open(source)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if p.head == nil {
		if time.Since(p.headFailed) < partialRetry {
			return 0, nil
		}
		if p.head, p.scale, err = clusterHead(f, st.Size(), have); err != nil {
			p.headFailed = time.Now()
			return 0, err
		}
	}
	budget := int64(scanPerPass)
	for _, s := range have {
		for _, gap := range unscanned(s, p.scanned) {
			if budget <= 0 || ctx.Err() != nil {
				break
			}
			part := Span{gap.From, min(gap.To, gap.From+budget)}
			p.clusters = append(p.clusters, scanClusters(ctx, f, part, p.scale)...)
			p.scanned = mergeSpans(append(p.scanned, part))
			budget -= part.To - part.From
		}
	}
	slices.SortFunc(p.clusters, func(a, b cluster) int { return cmp.Compare(a.at, b.at) })
	p.clusters = slices.CompactFunc(p.clusters, func(a, b cluster) bool { return a.at == b.at })
	if err := os.MkdirAll(filepath.Join(dir, "tiles"), 0o755); err != nil {
		return 0, err
	}

	made := 0
	for i := 0; i < plan.Count && made < tilesPerPass && ctx.Err() == nil; i++ {
		if _, err := os.Stat(tilePath(dir, i)); err == nil {
			continue
		}
		if at, ok := p.failed[i]; ok && time.Since(at) < partialRetry {
			continue
		}
		want := float64(i) * plan.Interval
		from, to, ok := leadingUp(p.clusters, have, p.scanned, want, st.Size())
		if !ok {
			continue
		}
		body := make([]byte, to-from)
		if _, err := f.ReadAt(body, from); err != nil && !errors.Is(err, io.EOF) {
			return made, err
		}
		if err := t.tile(ctx, p.head, body, want, tilePath(dir, i), height); err != nil {
			t.log.Debug("scrub preview tile", "tile", i, "err", err)
			p.failed[i] = time.Now()
			continue
		}
		made++
	}
	return made, nil
}

const partialRetry = 2 * time.Minute

func leadingUp(clusters []cluster, have, scanned []Span, want float64, size int64) (from, to int64, ok bool) {
	k := -1
	for i, c := range clusters {
		if c.time <= want {
			k = i
		}
	}
	if k < 0 || clusters[k].end < 0 || clusters[k].end-clusters[k].at > tileRead {
		return 0, 0, false
	}
	last := clusters[k]
	next := k+1 < len(clusters) && clusters[k+1].at == last.end
	if !covered(have, last.at, last.end) || !next && !covered(scanned, last.end, min(last.end+16, size)) {
		return 0, 0, false
	}
	from = last.at
	for j := k; j > 0; j-- {
		prev := clusters[j-1]
		if prev.end != clusters[j].at || want-prev.time > keyframeReach || last.end-prev.at > tileRead {
			break
		}
		from = prev.at
	}
	return from, last.end, true
}

func clusterHead(f io.ReaderAt, size int64, have []Span) ([]byte, uint64, error) {
	top, err := matroskaTop(f, size)
	if err != nil {
		return nil, 0, err
	}
	head := make([]byte, top.ebmlEnd)
	if !covered(have, 0, top.ebmlEnd) {
		return nil, 0, errNoIndex
	}
	if _, err := f.ReadAt(head, 0); err != nil {
		return nil, 0, err
	}
	head = append(head, openSegment...)
	for _, id := range []uint64{mkvInfo, mkvTracks} {
		at, ok := top.where[id]
		if !ok {
			return nil, 0, errNoIndex
		}
		_, length, n, err := elementAt(f, at)
		if err != nil || length < 0 || length > maxMetaBytes || !covered(have, at, at+n+length) {
			return nil, 0, errNoIndex
		}
		el := make([]byte, n+length)
		if _, err := f.ReadAt(el, at); err != nil {
			return nil, 0, err
		}
		head = append(head, el...)
	}
	return head, top.scale, nil
}

func scanClusters(ctx context.Context, r io.ReaderAt, s Span, scale uint64) []cluster {
	var out []cluster
	buf := make([]byte, scanChunk+32)
	for from := max(0, s.From-64); from < s.To && ctx.Err() == nil; from += scanChunk {
		stride := int(min(scanChunk, s.To-from))
		n, _ := r.ReadAt(buf[:stride+32], from)
		chunk := buf[:n]
		for i := 0; i < min(n, stride); {
			j := bytes.Index(chunk[i:], mkvClusterID)
			if j < 0 || i+j >= min(n, stride) {
				break
			}
			at := i + j
			if c, ok := readCluster(chunk[at:], scale); ok {
				c.at = from + int64(at)
				if c.end >= 0 {
					c.end += c.at
				}
				out = append(out, c)
			}
			i = at + 1
		}
	}
	return out
}

func readCluster(b []byte, scale uint64) (cluster, bool) {
	if len(b) < 6 {
		return cluster{}, false
	}
	size, n := vint(b[4:], false)
	if n == 0 || size > tileRead {
		return cluster{}, false
	}
	rest := b[4+n:]
	for skips := 0; skips < 2 && len(rest) > 1 && (rest[0] == 0xBF || rest[0] == 0xEC); skips++ {
		length, m := vint(rest[1:], false)
		if m == 0 || length < 0 || length > 16 || len(rest) < 1+m+int(length) {
			return cluster{}, false
		}
		rest = rest[1+m+int(length):]
	}
	if len(rest) < 2 || rest[0] != 0xE7 {
		return cluster{}, false
	}
	length, m := vint(rest[1:], false)
	if m == 0 || length < 1 || length > 8 || len(rest) < 1+m+int(length) {
		return cluster{}, false
	}
	ticks := uintOf(rest[1+m : 1+m+int(length)])
	if after := rest[1+m+int(length):]; len(after) == 0 || !blockStart(after) {
		return cluster{}, false
	}
	end := int64(-1)
	if size >= 0 {
		end = int64(4+n) + size
	}
	return cluster{end: end, time: float64(ticks) * float64(scale) / 1e9}, true
}

func blockStart(b []byte) bool {
	switch b[0] {
	case 0xA3, 0xA0, 0xAF, 0xA7, 0xAB:
		return true
	}
	return false
}

func (t *Thumbnails) tile(ctx context.Context, head, body []byte, want float64, out string, height int) error {
	part := strings.TrimSuffix(out, ".jpg") + ".part.jpg"
	cmd := proc.Background(exec.CommandContext(ctx, t.ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-skip_frame", "nokey", "-copyts", "-f", "matroska", "-i", "pipe:0", "-an", "-sn", "-dn",
		"-vf", fmt.Sprintf(`select=lte(t\,%.3f),scale=%d:%d`, want+0.001, thumbWidth, height),
		"-fps_mode", "passthrough", "-update", "1", "-q:v", "6", part))
	cmd.Stdin = io.MultiReader(bytes.NewReader(head), bytes.NewReader(body))
	said, err := cmd.CombinedOutput()
	if st, statErr := os.Stat(part); err != nil || statErr != nil || st.Size() == 0 {
		os.Remove(part)
		return fmt.Errorf("no picture: %v: %s", err, tail(string(said)))
	}
	return os.Rename(part, out)
}

func layOut(dir string, plan Sheet, height int) error {
	sheet := image.NewRGBA(image.Rect(0, 0, plan.Columns*thumbWidth, plan.Rows*height))
	have := bytes.Repeat([]byte{'0'}, plan.Count)
	for i := range plan.Count {
		f, err := os.Open(tilePath(dir, i))
		if err != nil {
			continue
		}
		img, err := jpeg.Decode(f)
		f.Close()
		if err != nil {
			continue
		}
		x, y := i%plan.Columns*thumbWidth, i/plan.Columns*height
		draw.Draw(sheet, image.Rect(x, y, x+thumbWidth, y+height), img, img.Bounds().Min, draw.Src)
		have[i] = '1'
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, sheet, &jpeg.Options{Quality: 80}); err != nil {
		return err
	}
	if err := replaceFile(partialSheet(dir), out.Bytes()); err != nil {
		return err
	}
	index, _ := json.Marshal(map[string]string{"have": string(have)})
	return replaceFile(partialIndex(dir), index)
}

func readPartial(dir string) string {
	raw, err := os.ReadFile(partialIndex(dir))
	if err != nil {
		return ""
	}
	var index struct{ Have string }
	if json.Unmarshal(raw, &index) != nil {
		return ""
	}
	return index.Have
}

func (t *Thumbnails) dropPartial(dir string) {
	os.RemoveAll(filepath.Join(dir, "tiles"))
	os.Remove(partialSheet(dir))
	os.Remove(partialIndex(dir))
	t.mu.Lock()
	delete(t.partials, dir)
	t.mu.Unlock()
}

func replaceFile(path string, data []byte) error {
	part := path + ".part"
	if err := os.WriteFile(part, data, 0o644); err != nil {
		return err
	}
	return os.Rename(part, path)
}

func covered(have []Span, from, to int64) bool {
	for _, s := range have {
		if s.From <= from && to <= s.To {
			return true
		}
	}
	return false
}

func unscanned(s Span, done []Span) []Span {
	var out []Span
	at := s.From
	for _, d := range done {
		if d.To <= at || d.From >= s.To {
			continue
		}
		if d.From > at {
			out = append(out, Span{at, d.From})
		}
		at = max(at, d.To)
	}
	if at < s.To {
		out = append(out, Span{at, s.To})
	}
	return out
}

func mergeSpans(spans []Span) []Span {
	slices.SortFunc(spans, func(a, b Span) int { return cmp.Compare(a.From, b.From) })
	var out []Span
	for _, s := range spans {
		if n := len(out); n > 0 && s.From <= out[n-1].To {
			out[n-1].To = max(out[n-1].To, s.To)
			continue
		}
		out = append(out, s)
	}
	return out
}
