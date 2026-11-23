package transcode

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/proc"
)

const subtitleDeadline = 45 * time.Second

const subtitleRefresh = 8 * time.Second

type Subtitles struct {
	ffmpeg string

	mu   sync.Mutex
	done map[string]struct{}
	busy map[string]*sync.Mutex
}

func NewSubtitles(ffmpeg string) *Subtitles {
	return &Subtitles{
		ffmpeg: absolute(ffmpeg),
		done:   map[string]struct{}{},
		busy:   map[string]*sync.Mutex{},
	}
}

func (s *Subtitles) lockFor(path string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m, ok := s.busy[path]; ok {
		return m
	}
	m := &sync.Mutex{}
	s.busy[path] = m
	return m
}

func (s *Subtitles) lockPath(path string) *sync.Mutex {
	for {
		m := s.lockFor(path)
		m.Lock()
		s.mu.Lock()
		current := s.busy[path] == m
		s.mu.Unlock()
		if current {
			return m
		}
		m.Unlock()
	}
}

func (s *Subtitles) Forget(dir string) {
	inside := filepath.Clean(dir) + string(filepath.Separator)
	s.ForgetOwned(func(path string) bool { return strings.HasPrefix(path, inside) })
}

func (s *Subtitles) ForgetOwned(owned func(path string) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for path := range s.done {
		if owned(path) {
			delete(s.done, path)
		}
	}
	for path, m := range s.busy {
		if owned(path) && m.TryLock() {
			m.Unlock()
			delete(s.busy, path)
		}
	}
}

type Track struct {
	Index    int    `json:"index"`
	Language string `json:"language"`
	Title    string `json:"title"`
	Codec    string `json:"codec"`
	Default  bool   `json:"default"`
	Forced   bool   `json:"forced"`
	URL      string `json:"url"`
	Pictures bool   `json:"pictures,omitempty"`
}

type FontFile struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (s *Subtitles) Cues(dir string, track int, codec string) int {
	return dialogueLines(filepath.Join(dir, fmt.Sprintf("sub-%d.%s", track, subtitleExt(codec))))
}

func (s *Subtitles) Whole(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.done[path]
	return ok
}

func (s *Subtitles) Extract(ctx context.Context, source, dir string, track int, codec string, complete bool) (string, error) {
	name := fmt.Sprintf("sub-%d.%s", track, subtitleExt(codec))
	path := filepath.Join(dir, name)

	one := s.lockPath(path)
	defer one.Unlock()

	s.mu.Lock()
	_, already := s.done[path]
	s.mu.Unlock()
	if already {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
		s.mu.Lock()
		delete(s.done, path)
		s.mu.Unlock()
	}

	if !complete {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) < subtitleRefresh && dialogueLines(path) > 0 {
			return path, nil
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, subtitleDeadline)
	defer cancel()

	args := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, reconnectArgs(source)...)
	args = append(args, "-i", source, "-map", fmt.Sprintf("0:%d", track))
	if isASS(codec) {
		args = append(args, "-c:s", "copy")
	} else {
		args = append(args, "-c:s", "ass")
	}
	staging := path + ".part"
	args = append(args, "-f", subtitleExt(codec), "-y", staging)

	out, err := proc.Hide(exec.CommandContext(ctx, s.ffmpeg, args...)).CombinedOutput()

	cues := dialogueLines(staging)
	have := dialogueLines(path)
	if err != nil && cues == 0 && have == 0 {
		os.Remove(staging)
		return "", fmt.Errorf("extract subtitle %d: %w: %s", track, err, strings.TrimSpace(string(out)))
	}

	if have > 0 {
		if mergeErr := mergeInto(staging, path); mergeErr != nil {
			os.Remove(staging)
			return path, nil
		}
	}

	if renameErr := os.Rename(staging, path); renameErr != nil {
		os.Remove(staging)
		if have > 0 {
			return path, nil
		}
		return "", fmt.Errorf("install subtitle %d: %w", track, renameErr)
	}

	if err == nil && complete {
		s.mu.Lock()
		s.done[path] = struct{}{}
		s.mu.Unlock()
	}
	return path, nil
}

const (
	aroundBefore = 30.0
	aroundSpan   = 330.0
	aroundLimit  = 20 * time.Second
)

func (s *Subtitles) ExtractAround(ctx context.Context, source, dir string, track int, codec string, at float64) (string, error) {
	path := filepath.Join(dir, fmt.Sprintf("sub-%d.%s", track, subtitleExt(codec)))

	one := s.lockPath(path)
	defer one.Unlock()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, aroundLimit)
	defer cancel()

	from := max(0, at-aroundBefore)
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-ss", strconv.FormatFloat(from, 'f', 3, 64), "-t", strconv.FormatFloat(aroundSpan, 'f', 0, 64),
		"-copyts",
	}
	args = append(args, reconnectArgs(source)...)
	args = append(args, "-i", source, "-map", fmt.Sprintf("0:%d", track))
	if isASS(codec) {
		args = append(args, "-c:s", "copy")
	} else {
		args = append(args, "-c:s", "ass")
	}
	staging := path + ".around"
	args = append(args, "-f", subtitleExt(codec), "-y", staging)
	out, err := proc.Hide(exec.CommandContext(ctx, s.ffmpeg, args...)).CombinedOutput()
	defer os.Remove(staging)

	if dialogueLines(staging) == 0 {
		if err != nil {
			return path, fmt.Errorf("extract subtitle %d around %.0fs: %w: %s", track, at, err, strings.TrimSpace(string(out)))
		}
		return path, nil
	}
	if _, statErr := os.Stat(path); statErr == nil {
		if err := mergeInto(staging, path); err != nil {
			return path, err
		}
	}
	return path, os.Rename(staging, path)
}

func mergeInto(read, have string) error {
	a, err := os.ReadFile(have)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(read)
	if err != nil {
		return err
	}
	return os.WriteFile(read, []byte(mergeASS(string(a), string(b))), 0o644)
}

func dialogueLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	var n int
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scan.Scan() {
		if strings.HasPrefix(scan.Text(), "Dialogue:") {
			n++
		}
	}
	return n
}

func (s *Subtitles) ExtractFonts(ctx context.Context, source, dir string, attachments []Attachment) ([]FontFile, error) {
	fontDir := filepath.Join(dir, "fonts")

	if entries, err := os.ReadDir(fontDir); err == nil && len(entries) > 0 {
		return listFonts(fontDir), nil
	}
	if len(attachments) == 0 {
		return nil, nil
	}
	partDir := fontDir + ".part"
	os.RemoveAll(partDir)
	if err := os.Mkdir(partDir, 0o755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(partDir)

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	used := map[string]bool{}
	for i, a := range attachments {
		name := safeFontName(a.Filename, i)
		for used[strings.ToLower(name)] {
			name = fmt.Sprintf("%d-%s", i, name)
		}
		used[strings.ToLower(name)] = true
		args = append(args, fmt.Sprintf("-dump_attachment:%d", a.Index), filepath.Join(partDir, name))
	}
	args = append(args, "-i", source, "-t", "0", "-f", "null", "-")

	_ = proc.Hide(exec.CommandContext(ctx, s.ffmpeg, args...)).Run()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("fonts: %w", err)
	}
	for _, f := range listFonts(partDir) {
		path := filepath.Join(partDir, f.Name)
		switch fontHeader(path) {
		case fontArriving:
			return nil, fmt.Errorf("font %s is not whole yet", f.Name)
		case fontBroken:
			os.Remove(path)
		}
	}
	if err := os.Rename(partDir, fontDir); err != nil {
		return nil, err
	}
	return listFonts(fontDir), nil
}

type fontState int

const (
	fontWhole fontState = iota
	fontArriving
	fontBroken
)

func fontHeader(path string) fontState {
	f, err := os.Open(path)
	if err != nil {
		return fontBroken
	}
	defer f.Close()
	var head [4]byte
	n, _ := io.ReadFull(f, head[:])
	switch {
	case n == 0:
		return fontBroken
	case head == [4]byte{}:
		return fontArriving
	}
	switch string(head[:n]) {
	case "\x00\x01\x00\x00", "OTTO", "true", "typ1":
		data, err := os.ReadFile(path)
		if err != nil {
			return fontBroken
		}
		return fontTables(data)
	case "ttcf", "wOFF", "wOF2":
		data, err := os.ReadFile(path)
		if err != nil {
			return fontBroken
		}
		if allZero(data[max(len(data)-4096, 0):]) {
			return fontArriving
		}
		return fontWhole
	}
	return fontBroken
}

func fontTables(data []byte) fontState {
	if len(data) < 12 {
		return fontBroken
	}
	count := int(binary.BigEndian.Uint16(data[4:6]))
	if count == 0 || 12+16*count > len(data) {
		return fontBroken
	}
	for i := range count {
		rec := data[12+16*i:]
		off, size := int64(binary.BigEndian.Uint32(rec[8:12])), int64(binary.BigEndian.Uint32(rec[12:16]))
		if off+size > int64(len(data)) {
			return fontBroken
		}
		if size >= 64 && allZero(data[off:off+size]) {
			return fontArriving
		}
	}
	return fontWhole
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

func safeFontName(stored string, i int) string {
	base := filepath.Base(strings.ReplaceAll(stored, `\`, "/"))
	base = strings.Trim(unsafeName.ReplaceAllString(base, "_"), ". ")
	if base == "" || base == "_" {
		return fmt.Sprintf("font%d.ttf", i)
	}
	if len(base) > 200 {
		ext := filepath.Ext(base)
		if len(ext) > 8 {
			ext = ".ttf"
		}
		return fmt.Sprintf("font%d%s", i, ext)
	}
	return base
}

func listFonts(dir string) []FontFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var out []FontFile
	for _, e := range entries {
		if e.IsDir() || !isFont(e.Name()) {
			continue
		}
		out = append(out, FontFile{Name: e.Name()})
	}
	return out
}

func isFont(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range []string{".ttf", ".otf", ".ttc", ".woff", ".woff2"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func isBitmapSubtitle(codec string) bool {
	switch codec {
	case "hdmv_pgs_subtitle", "pgssub", "dvd_subtitle", "dvdsub",
		"dvb_subtitle", "dvbsub", "xsub", "hdmv_text_subtitle":
		return true
	}
	return false
}

func isTextSubtitle(codec string) bool {
	return codec != "" && !isBitmapSubtitle(codec)
}

func TextSubtitle(codec string) bool { return isTextSubtitle(codec) }

func isASS(codec string) bool { return codec == "ass" || codec == "ssa" }

func subtitleExt(codec string) string {
	if isTextSubtitle(codec) {
		return "ass"
	}
	return "sup"
}

func Renderable(codec string) bool { return isTextSubtitle(codec) }

func PictureSubtitle(codec string) bool { return codec == "hdmv_pgs_subtitle" || codec == "pgssub" }

func (m *MediaInfo) SubtitleCodec(index int) string {
	for _, s := range m.Subtitles {
		if s.Index == index {
			return s.Codec
		}
	}
	return ""
}
