package transcode

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"yoru/internal/proc"
	"yoru/internal/sound"
)

const (
	SegmentSeconds = 6.0

	restartAfterSegments = 4

	sessionIdle = 10 * time.Minute

	softwareEncoder = "libx264"

	encoderLogLines = 10
)

var errSessionClosed = fmt.Errorf("stream session closed")

type Session struct {
	ID       string     `json:"id"`
	Source   string     `json:"-"`
	Info     *MediaInfo `json:"info"`
	Plan     Plan       `json:"plan"`
	Segments int        `json:"segments"`

	dir        string
	encoder    string
	ffmpeg     string
	onSeek     SeekHook
	onFallback func(codec string)
	log        *slog.Logger

	mu           sync.Mutex
	run          *run
	waiters      map[int]int
	headFrom     int
	headTo       int
	touched      time.Time
	audioTrack   int
	audioPref    string
	sound        sound.Options
	caps         *Caps
	want         Want
	tuned        map[string][]string
	toneMapper   string
	closed       bool
	startMu      sync.Mutex
	refs         int
	fellBack     bool
	initServed   bool
	initStale    bool
	liveSubs     bool
	burned       bool
	burnPictures int
	burnDelay    float64
	finished     *run
	ended        *run
	barren       int
	broken       *Unconvertible
	starts       []float64
	segs         map[int]sliceSegment
}

var passes atomic.Int64

var sessionDirs atomic.Int64

const failedPasses = 3

type Unconvertible struct{ Detail string }

func (e *Unconvertible) Error() string { return "the encoder failed on every try: " + e.Detail }

type run struct {
	cmd         *exec.Cmd
	stderr      tailBuffer
	from        int
	exited      chan struct{}
	began       time.Time
	subs        *exec.Cmd
	subsGone    chan struct{}
	subsStopped atomic.Bool
	subsAlive   atomic.Bool
	id          int
	clean       atomic.Bool
	gathered    chan struct{}
	discarded   atomic.Bool
}

func (r *run) wrote(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0 && !info.ModTime().Before(r.began)
}

type SeekHook func(sessionID string, fraction float64)

type Manager struct {
	ffmpeg     string
	prober     *Prober
	root       string
	encoder    string
	tuned      map[string][]string
	toneMapper string
	onSeek     SeekHook
	onIdle     func(id string)
	arriving   func(ctx context.Context, id string) int64
	log        *slog.Logger

	mu       sync.Mutex
	sessions map[string]*Session
}

func (m *Manager) OnSeek(hook SeekHook) { m.onSeek = hook }

func (m *Manager) Installed() bool {
	for _, p := range []string{m.ffmpeg, m.prober.ffprobe} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

func (m *Manager) OnIdle(hook func(id string)) { m.onIdle = hook }

func NewManager(ffmpeg, ffprobe, root, encoder string, log *slog.Logger) *Manager {
	return &Manager{
		ffmpeg:   absolute(ffmpeg),
		prober:   NewProber(absolute(ffprobe)),
		root:     root,
		encoder:  encoder,
		log:      log,
		sessions: map[string]*Session{},
	}
}

func absolute(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func (m *Manager) Purge() (int, error) {
	root := filepath.Join(m.root, "hls")

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var removed int
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
			m.log.Warn("purge transcode dir", "dir", e.Name(), "err", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		m.log.Info("cleared stale transcode output", "sessions", removed)
	}
	return removed, nil
}

const probeExtensions = 4

func (m *Manager) OnArriving(bytes func(ctx context.Context, id string) int64) { m.arriving = bytes }

func (m *Manager) probeWhileArriving(ctx context.Context, id, source string) (*MediaInfo, error) {
	var seen int64
	if m.arriving != nil {
		seen = m.arriving(ctx, id)
	}
	for extended := 0; ; extended++ {
		info, err := m.prober.Probe(ctx, source)
		if err == nil || !errors.Is(err, ErrProbeTimeout) || m.arriving == nil || extended >= probeExtensions {
			return info, err
		}
		got := m.arriving(ctx, id)
		if got <= seen {
			return nil, err
		}
		m.log.Info("the start of the file is still arriving, waiting on", "session", id, "arrivedMB", got>>20)
		seen = got
	}
}

func (m *Manager) Open(ctx context.Context, id, source string) (*Session, error) {
	return m.OpenProbing(ctx, id, source, source)
}

func (m *Manager) OpenProbing(ctx context.Context, id, source, probeSource string) (*Session, error) {
	return m.open(ctx, id, source, probeSource, nil, nil, Want{})
}

func (m *Manager) OpenFor(ctx context.Context, id, source, probeSource string, caps *Caps, want Want) (*Session, error) {
	return m.open(ctx, id, source, probeSource, nil, caps, want)
}

type Burn struct {
	Subtitles string
	Fonts     string
	Pictures  int
	Delay     float64
}

func (m *Manager) OpenBurned(ctx context.Context, id, source, probeSource string, burn Burn, want Want) (*Session, error) {
	return m.open(ctx, id, source, probeSource, &burn, nil, want)
}

func (m *Manager) open(ctx context.Context, id, source, probeSource string, burn *Burn, caps *Caps, want Want) (*Session, error) {
	m.mu.Lock()
	encoder, toneMapper := m.encoder, m.toneMapper
	var info *MediaInfo
	sibling := m.sibling(id, source)
	if sibling != nil {
		info = sibling.Info
		id = variantID(id, plannable(info, toneMapper), encoder, caps, want)
	}
	if s, ok := m.sessions[id]; ok && s.Source == source {
		s.touch()
		s.mu.Lock()
		s.refs++
		s.mu.Unlock()
		m.mu.Unlock()
		return s, nil
	}
	stale := m.otherSources(id, source)
	m.mu.Unlock()

	for _, old := range stale {
		m.discard(old)
		if old != id && m.onIdle != nil {
			m.onIdle(old)
		}
	}

	probed := "session"
	if info == nil {
		var err error
		if probeSource == source {
			info, err = m.probeWhileArriving(ctx, id, source)
			probed = "source"
		} else if info, err = m.prober.Probe(ctx, probeSource); err == nil {
			probed = "disk"
		} else {
			m.log.Warn("probe from disk failed, reading through the engine",
				"session", id, "err", err)
			info, err = m.probeWhileArriving(ctx, id, source)
			probed = "engine"
		}
		if err != nil {
			return nil, err
		}
		id = variantID(id, plannable(info, toneMapper), encoder, caps, want)
	}

	dir := filepath.Join(m.root, "hls", fmt.Sprintf("%s-%d", id, sessionDirs.Add(1)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	m.mu.Lock()
	if existing, ok := m.sessions[id]; ok && existing.Source == source {
		existing.mu.Lock()
		existing.refs++
		existing.mu.Unlock()
		m.mu.Unlock()
		existing.touch()
		os.RemoveAll(dir)
		return existing, nil
	}
	m.mu.Unlock()

	plan := planOf(plannable(info, toneMapper), encoder, 0, sound.Options{}, caps, want, burn != nil)
	if burn != nil {
		if err := burn.into(dir); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
	}
	var starts []float64
	if sibling != nil && sibling.starts != nil && plan.VideoCopy {
		starts = sibling.starts
	} else {
		starts = m.indexKeyframes(ctx, id, probeSource, info, plan)
	}
	segments := segmentCount(info.Duration)
	if starts != nil {
		segments = len(starts)
	}
	s := &Session{
		ID: id, Source: source, Info: info,
		Plan:     plan,
		Segments: segments,
		starts:   starts,
		dir:      dir, encoder: encoder, ffmpeg: m.ffmpeg, log: m.log,
		onSeek:     m.onSeek,
		onFallback: m.softwareOnly,
		headFrom:   -1,
		touched:    time.Now(),
		refs:       1,
		liveSubs:   burn == nil && liveSubtitlesWanted(source),
		burned:     burn != nil,
		caps:       caps,
		want:       want,
	}
	s.tuned, _ = m.tools()
	s.toneMapper = toneMapper
	if burn != nil {
		s.burnPictures, s.burnDelay = burn.Pictures, burn.Delay
	}

	m.mu.Lock()
	if existing, ok := m.sessions[id]; ok {
		if existing.Source == source {
			existing.mu.Lock()
			existing.refs++
			existing.mu.Unlock()
			m.mu.Unlock()
			s.stop()
			os.RemoveAll(dir)
			return existing, nil
		}
		defer m.discardSession(existing)
	}
	m.sessions[id] = s
	m.mu.Unlock()

	cuts := "even"
	if s.sliced() {
		cuts = "keyframes"
	}
	m.log.Info("stream session opened", "id", id,
		"duration", int(info.Duration), "plan", s.Plan.Reason, "probed", probed,
		"audio", len(info.Audio), "subtitles", len(info.Subtitles), "segments", s.Segments, "cuts", cuts)
	return s, nil
}

func (m *Manager) currentEncoder() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.encoder
}

func (m *Manager) SetEncoder(encoder string) {
	if encoder == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.encoder = encoder
}

func (m *Manager) SetTuning(encoder string, tuning []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tuned = map[string][]string{encoder: tuning}
}

func (m *Manager) SetToneMapper(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.toneMapper = name
}

func (m *Manager) Converter() (encoder, toneMapper string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.encoder, m.toneMapper
}

func (m *Manager) tools() (map[string][]string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tuned, m.toneMapper
}

func (m *Manager) softwareOnly(codec string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.encoder == softwareEncoder {
		return
	}
	m.log.Warn("hardware encoder abandoned for this run",
		"encoder", codec, "using", softwareEncoder)
	m.encoder = softwareEncoder
}

func (m *Manager) sibling(id, source string) *Session {
	base := BaseID(id)
	for sid, s := range m.sessions {
		if s.Source == source && BaseID(sid) == base {
			return s
		}
	}
	return nil
}

func (m *Manager) otherSources(id, source string) []string {
	base := BaseID(id)
	var out []string
	for sid, s := range m.sessions {
		if s.Source != source && BaseID(sid) == base {
			out = append(out, sid)
		}
	}
	return out
}

func (m *Manager) Episode(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		return s, true
	}
	for sid, s := range m.sessions {
		if BaseID(sid) == id {
			return s, true
		}
	}
	return nil, false
}

func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if ok {
		s.touch()
	}
	return s, ok
}

func (m *Manager) Hold(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return false
	}
	s.mu.Lock()
	s.refs++
	s.mu.Unlock()
	s.touch()
	return true
}

func (m *Manager) Peek(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (s *Session) Kept() bool { return s.want.Keep }

func (s *Session) Holders() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refs
}

func (m *Manager) Close(id string) bool {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return false
	}

	s.mu.Lock()
	s.refs--
	last := s.refs <= 0
	if last {
		s.closed = true
	}
	s.mu.Unlock()

	if last {
		delete(m.sessions, id)
	}
	m.mu.Unlock()

	if !last {
		return false
	}
	return m.teardown(id, s)
}

func (m *Manager) teardown(id string, s *Session) bool {
	s.stop()
	os.RemoveAll(s.dir)
	m.mu.Lock()
	owner := m.sessions[id]
	m.mu.Unlock()
	return owner == nil || owner == s
}

func (m *Manager) discard(id string) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()

	if ok {
		m.discardSession(s)
	}
}

func (m *Manager) discardSession(s *Session) {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.stop()
	os.RemoveAll(s.dir)
}

func (m *Manager) Converting() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.mu.Lock()
		busy := s.run != nil && !s.Plan.VideoCopy && !usesHardware(s.encoder)
		s.mu.Unlock()
		if busy {
			return true
		}
	}
	return false
}

func (m *Manager) Sources() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]string, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Source)
	}
	return out
}

func liveSubtitlesWanted(source string) bool {
	return remote(source) && strings.Contains(source, "/torrents/")
}

func (m *Manager) Reap(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			m.closeAll()
			return
		case <-time.After(time.Minute):
		}

		m.mu.Lock()
		var stale []string
		for id, s := range m.sessions {
			if time.Since(s.lastTouch()) > sessionIdle {
				stale = append(stale, id)
			}
		}
		m.mu.Unlock()

		for _, id := range stale {
			m.log.Info("stream session expired", "id", id)
			m.discard(id)
			if m.onIdle != nil {
				m.onIdle(id)
			}
		}
	}
}

func (m *Manager) closeAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Go(func() { m.discard(id) })
	}
	wg.Wait()
}

func (s *Session) touch() {
	s.mu.Lock()
	s.touched = time.Now()
	s.mu.Unlock()
}

func (s *Session) lastTouch() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.touched
}

func segmentCount(duration float64) int {
	n := int(duration / SegmentSeconds)
	if duration-float64(n)*SegmentSeconds >= 1 {
		n++
	}
	return max(n, 1)
}

func (s *Session) Playlist() string {
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:7\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", s.targetDuration())
	fmt.Fprintf(&b, "#EXT-X-MEDIA-SEQUENCE:0\n")
	fmt.Fprintf(&b, "#EXT-X-PLAYLIST-TYPE:VOD\n")
	fmt.Fprintf(&b, "#EXT-X-MAP:URI=\"init.mp4\"\n")

	for i := 0; i < s.Segments; i++ {
		d := max(s.segmentStart(i+1)-s.segmentStart(i), 0.001)
		if i == s.Segments-1 {
			d = max(s.Info.Duration-s.segmentStart(i), 0.001)
		}
		fmt.Fprintf(&b, "#EXTINF:%.3f,\n%d.mp4\n", d, i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func (s *Session) SegmentPath(n int) string {
	return filepath.Join(s.dir, fmt.Sprintf("%d.mp4", n))
}

func (s *Session) InitPath() string { return filepath.Join(s.dir, "init.mp4") }

func (s *Session) Dir() string { return s.dir }

func (m *Manager) DirPrefix(id string) string { return filepath.Join(m.root, "hls", id+"-") }

func (m *Manager) Owns(id, path string) bool {
	rest, ok := strings.CutPrefix(path, m.DirPrefix(id))
	digits := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
	return ok && rest != "" && digits != 0 && (digits < 0 || os.IsPathSeparator(rest[digits]))
}

func (s *Session) Prestart(from int) error {
	if s.segmentDone(from) {
		return nil
	}
	return s.startIfIdle(from)
}

func (s *Session) startIfIdle(from int) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errSessionClosed
	}
	if s.run != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	return s.startLocked(from)
}

func (s *Session) WaitInit(ctx context.Context, from int, timeout time.Duration) (string, error) {
	path := s.InitPath()
	if fileReady(path) {
		s.touch()
		return path, nil
	}
	if err := s.startIfIdle(from); err != nil {
		return "", err
	}

	deadline := time.After(timeout)
	for {
		if fileReady(path) {
			s.touch()
			return path, nil
		}
		if !s.running() {
			if err := s.startIfIdle(from); err != nil {
				return "", err
			}
			if err := s.awaitFile(ctx, path, deadline); err != nil {
				return "", err
			}
			return path, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline:
			return "", fmt.Errorf("init segment not ready in %s (produced: %s)",
				timeout, strings.Join(s.produced(), ", "))
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *Session) awaitFile(ctx context.Context, path string, deadline <-chan time.Time) error {
	for !fileReady(path) {
		if !s.running() && !fileReady(path) {
			s.mu.Lock()
			broken := s.broken
			s.mu.Unlock()
			if broken != nil {
				return broken
			}
			return fmt.Errorf("%s was not produced", filepath.Base(path))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("%s was not produced", filepath.Base(path))
		case <-time.After(100 * time.Millisecond):
		}
	}
	s.touch()
	return nil
}

func (s *Session) WaitSegment(ctx context.Context, n int, timeout time.Duration) error {
	if s.segmentDone(n) {
		s.touch()
		return nil
	}

	s.addWaiter(n)
	defer s.dropWaiter(n)

	if err := s.ensureHead(n); err != nil {
		return err
	}

	deadline := time.After(timeout)
	var restarted bool
	for {
		s.advanceHead()

		if s.segmentDone(n) && (s.sliced() || s.nextStarted(n) || n >= s.Segments-1 || s.wroteWhole(s.SegmentPath(n)) || segmentComplete(s.SegmentPath(n))) {
			s.touch()
			return nil
		}

		if !s.running() {
			if restarted {
				return fmt.Errorf("segment %d was not produced", n)
			}
			restarted = true
			if err := s.start(n); err != nil {
				return err
			}
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("segment %d not ready in %s (produced: %s)",
				n, timeout, strings.Join(s.produced(), ", "))
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func (s *Session) produced() []string {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return []string{"unreadable: " + err.Error()}
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			out = append(out, fmt.Sprintf("%s(%d)", e.Name(), info.Size()))
		}
	}
	if len(out) == 0 {
		return []string{"nothing"}
	}
	return out
}

func fileReady(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

func cutShort(source, detail string) bool { return remote(source) && endedEarly(detail) }

func endedEarly(detail string) bool {
	for _, sign := range []string{"ends prematurely", "Error during demuxing", "I/O error", "Input/output error"} {
		if strings.Contains(detail, sign) {
			return true
		}
	}
	return false
}

func segmentReady(path string) bool {
	f, err := openShared(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	return bytes.Contains(head[:n], []byte("moof"))
}

func segmentComplete(path string) bool {
	f, err := openShared(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() < 16 {
		return false
	}
	var at int64
	var head [16]byte
	sawMoof, last := false, ""
	for at < info.Size() {
		if _, err := f.ReadAt(head[:8], at); err != nil {
			return false
		}
		size := int64(binary.BigEndian.Uint32(head[:4]))
		typ := string(head[4:8])
		switch size {
		case 0:
			return false
		case 1:
			if _, err := f.ReadAt(head[8:16], at+8); err != nil {
				return false
			}
			size = int64(binary.BigEndian.Uint64(head[8:16]))
		}
		if size < 8 {
			return false
		}
		if typ == "moof" {
			sawMoof = true
		}
		last = typ
		at += size
	}
	return at == info.Size() && sawMoof && last == "mdat"
}

func (s *Session) wroteWhole(path string) bool {
	s.mu.Lock()
	r := s.ended
	s.mu.Unlock()
	return r != nil && r.wrote(path)
}

func (s *Session) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.run != nil
}

func (s *Session) Speed() float64 {
	s.advanceHead()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil || s.headTo < s.headFrom {
		return 0
	}
	elapsed := time.Since(s.run.began).Seconds()
	if elapsed < 2 {
		return 0
	}
	return (s.segmentStart(s.headTo+1) - s.segmentStart(s.headFrom)) / elapsed
}

func (s *Session) replan() {
	s.Plan = planOf(plannable(s.Info, s.toneMapper), s.encoder, s.audioTrack, s.sound, s.caps, s.want, s.burned)
}

func (s *Session) CurrentPlan() Plan {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Plan
}

func (s *Session) advanceHead() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run == nil {
		return
	}
	for s.madeByLocked(s.run, s.headTo+1) && (s.sliced() || s.madeByLocked(s.run, s.headTo+2) || segmentComplete(s.SegmentPath(s.headTo+1))) {
		s.headTo++
	}
}

func (s *Session) nextStarted(n int) bool {
	s.mu.Lock()
	r := s.run
	s.mu.Unlock()
	next := s.SegmentPath(n + 1)
	if r != nil && n >= r.from {
		return r.wrote(next)
	}
	return fileReady(next)
}

func (s *Session) ensureHead(n int) error {
	s.advanceHead()
	s.mu.Lock()
	active := s.run != nil
	from, to := s.headFrom, s.headTo
	s.mu.Unlock()

	if shouldRestart(n, from, to, active) {
		return s.start(n)
	}
	return nil
}

const strandedGrace = 12 * time.Second

func (s *Session) awaitStranded(from int) {
	deadline := time.Now().Add(strandedGrace)
	for time.Now().Before(deadline) {
		s.advanceHead()
		s.mu.Lock()
		run, cur, to := s.run, s.headFrom, s.headTo
		stranded := false
		for n := range s.waiters {
			servedSoon := !shouldRestart(n, cur, to, run != nil)
			byNewPass := !shouldRestart(n, from, from-1, true)
			if servedSoon && !byNewPass {
				stranded = true
			}
		}
		s.mu.Unlock()
		if run == nil || !stranded {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (s *Session) addWaiter(n int) {
	s.mu.Lock()
	if s.waiters == nil {
		s.waiters = map[int]int{}
	}
	s.waiters[n]++
	s.mu.Unlock()
}

func (s *Session) dropWaiter(n int) {
	s.mu.Lock()
	if s.waiters[n]--; s.waiters[n] <= 0 {
		delete(s.waiters, n)
	}
	s.mu.Unlock()
}

func shouldRestart(n, from, to int, active bool) bool {
	switch {
	case !active:
		return true
	case n < from:
		return true
	case n > to+1+restartAfterSegments:
		return true
	default:
		return false
	}
}

func (s *Session) start(from int) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	return s.startLocked(from)
}

func (s *Session) startLocked(from int) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errSessionClosed
	}
	if s.broken != nil {
		s.mu.Unlock()
		return s.broken
	}
	if r := s.run; r != nil && from >= s.headFrom && from-s.headFrom <= restartAfterSegments {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	s.awaitStranded(from)

	if killed := s.stop(); killed != nil {
		s.dropTail(killed)
	}

	for n := from; !s.sliced() && fileReady(s.SegmentPath(n)); n++ {
		os.Remove(s.SegmentPath(n))
	}

	offset := s.segmentStart(from)

	if from > 0 && s.onSeek != nil && s.Info.Duration > 0 {
		go s.onSeek(s.ID, offset/s.Info.Duration)
	}

	r, err := s.launch(from)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.closed {
		r.discarded.Store(true)
		s.mu.Unlock()
		r.cmd.Process.Kill()
		go s.await(r)
		return errSessionClosed
	}
	s.run, s.headFrom, s.headTo, s.touched = r, from, from-1, time.Now()
	s.mu.Unlock()

	go s.await(r)
	return nil
}

func (s *Session) launch(from int) (*run, error) {
	r := &run{from: from, exited: make(chan struct{}), began: time.Now(), id: int(passes.Add(1)), gathered: make(chan struct{})}
	r.cmd = proc.Hide(exec.Command(s.ffmpeg, s.args(r)...))
	r.cmd.Dir = s.dir
	r.cmd.Stderr = &r.stderr

	if err := r.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}
	s.log.Debug("encoder started", "session", s.ID, "encoder", s.Plan.VideoCodec, "fromSegment", from)
	s.startSubtitles(r)
	if s.sliced() {
		go s.gather(r)
	} else {
		close(r.gathered)
	}
	return r, nil
}

func (s *Session) await(r *run) {
	err := r.cmd.Wait()
	detail := lastLines(r.stderr.String(), encoderLogLines)
	r.clean.Store(err == nil && !cutShort(s.Source, detail))
	close(r.exited)
	<-r.gathered

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.run != r {
		r.stopSubtitles()
		return
	}
	s.run = nil

	if err == nil {
		s.barren = 0
		s.finished = r
		if detail != "" {
			s.log.Warn("encoder output", "session", s.ID, "detail", detail)
		}
		if cutShort(s.Source, detail) {
			s.log.Warn("encoder ended before the source did", "session", s.ID, "fromSegment", r.from)
			s.dropTail(r)
		}
		s.ended = r
		return
	}
	r.stopSubtitles()
	s.log.Warn("encoder failed", "session", s.ID, "encoder", s.Plan.VideoCodec,
		"fromSegment", r.from, "err", err, "detail", detail)

	retry := s.canFallBack(r.from, detail)
	s.dropTail(r)
	if !retry || s.closed {
		s.ended = r
		if s.madeByLocked(r, r.from) {
			s.barren = 0
		} else {
			s.barren++
		}
		if s.barren >= failedPasses && s.broken == nil {
			s.broken = &Unconvertible{Detail: detail}
			s.log.Warn("the encoder fails on every try; the player is told instead", "session", s.ID, "passes", s.barren)
		}
		return
	}

	previous := s.switchToSoftware()
	s.log.Warn("falling back to software encoding", "session", s.ID, "was", previous)
	if s.onFallback != nil {
		go s.onFallback(previous)
	}

	next, lerr := s.launch(r.from)
	if lerr != nil {
		s.log.Warn("software encoder did not start", "session", s.ID, "err", lerr)
		return
	}
	s.run, s.headFrom, s.headTo = next, r.from, r.from-1
	go s.await(next)
}

func (s *Session) switchToSoftware() (previous string) {
	previous = s.Plan.VideoCodec
	s.Plan.VideoCodec, s.encoder, s.fellBack = softwareEncoder, softwareEncoder, true
	if fileReady(s.InitPath()) {
		s.wipeOutput()
		s.initStale = s.initServed
	}
	return previous
}

func (s *Session) ServedInit() {
	s.mu.Lock()
	s.initServed, s.initStale = true, false
	s.mu.Unlock()
}

func (s *Session) InitStale() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initStale
}

func (s *Session) wipeOutput() {
	s.dropSegments()
	os.Remove(s.InitPath())
}

func (s *Session) canFallBack(from int, detail string) bool {
	return !s.fellBack && !s.Plan.VideoCopy && usesHardware(s.Plan.VideoCodec) &&
		!fileReady(s.SegmentPath(from)) && encoderInitFailed(detail)
}

func encoderInitFailed(detail string) bool {
	d := strings.ToLower(detail)
	for _, m := range []string{
		"initializeencoder", "openencodesessionex", "cannot load", "cannot init",
		"no capable devices", "doesn't support", "no device", "error initializing",
		"init failed", "not supported", "failed to initialise", "failed to initialize",
	} {
		if strings.Contains(d, m) {
			return true
		}
	}
	return false
}

func (s *Session) dropTail(r *run) {
	if s.sliced() {
		return
	}
	last := -1
	for n := r.from; r.wrote(s.SegmentPath(n)); n++ {
		last = n
	}
	if last >= 0 {
		os.Remove(s.SegmentPath(last))
	}
}

func lastLines(s string, n int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.Join(lines, " | ")
}

func (s *Session) stop() *run {
	s.mu.Lock()
	r := s.run
	s.run, s.ended = nil, nil
	if done := s.finished; done != nil {
		done.stopSubtitles()
		s.finished = nil
	}
	if r != nil {
		r.discarded.Store(true)
	}
	s.mu.Unlock()

	if r == nil {
		return nil
	}
	r.cmd.Process.Kill()
	r.stopSubtitles()
	select {
	case <-r.exited:
	case <-time.After(3 * time.Second):
	}
	select {
	case <-r.gathered:
	case <-time.After(3 * time.Second):
	}
	return r
}
