package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"yoru/internal/proc"
	"yoru/internal/sound"
)

type EventKind string

const (
	EventPosition EventKind = "position"
	EventPause    EventKind = "pause"
	EventEnd      EventKind = "end"
	EventExit     EventKind = "exit"
)

type Event struct {
	Kind     EventKind
	Position float64
	Duration float64
	Paused   bool
	Reason   string
}

type Options struct {
	URL            string
	Title          string
	StartAt        float64
	ChaptersFile   string
	SkipRanges     []SkipRange
	AutoSkip       bool
	Anime4K        bool
	Anime4KMode    string
	Anime4KSize    string
	Audio          string
	SubLanguages   []string
	Sound          sound.Options
	SubLook        string
	SubRaise       bool
	SoftwareDecode bool
	Deband         bool
	Soften         bool
	Extra          []string
}

func SubtitleLookArgs(look string, raise bool, takes func(option string) bool) []string {
	var out []string
	if raise {
		out = append(out, "--sub-margin-y=70")
	}
	if !takes("sub-border-style") {
		switch look {
		case "box":
			look = "outline"
		case "yellow-box":
			look = "yellow"
		}
	}
	edge := "border"
	if takes("sub-outline-color") {
		edge = "outline"
	}
	switch look {
	case "outline", "yellow":
		color := "#FFFFFF"
		if look == "yellow" {
			color = "#FFFF00"
		}
		return append(out, "--sub-color="+color, "--sub-"+edge+"-color=#000000", "--sub-"+edge+"-size=3", "--sub-shadow-offset=1")
	case "box", "yellow-box":
		color := "#FFFFFF"
		if look == "yellow-box" {
			color = "#FFFF00"
		}
		return append(out, "--sub-color="+color, "--sub-border-style=background-box", "--sub-back-color=#99000000")
	}
	return out
}

func AudioLanguages(pref string) string {
	switch pref {
	case "dub":
		return "eng,en,english"
	case "sub":
		return "jpn,ja,jp,japanese"
	}
	return ""
}

var languageTags = map[string][]string{
	"en": {"en", "eng", "english"},
	"es": {"es", "spa", "spanish", "es-419", "es-es"},
	"pt": {"pt", "por", "portuguese", "pt-br"},
	"fr": {"fr", "fre", "fra", "french"},
	"de": {"de", "ger", "deu", "german"},
	"it": {"it", "ita", "italian"},
	"ru": {"ru", "rus", "russian"},
	"ar": {"ar", "ara", "arabic"},
	"zh": {"zh", "chi", "zho", "chinese"},
	"ko": {"ko", "kor", "korean"},
	"id": {"id", "ind", "indonesian"},
	"vi": {"vi", "vie", "vietnamese"},
	"th": {"th", "tha", "thai"},
}

func SubtitleLanguages(codes []string) string {
	var out []string
	for _, c := range codes {
		if tags, ok := languageTags[strings.ToLower(c)]; ok {
			out = append(out, tags...)
		} else if c != "" {
			out = append(out, c)
		}
	}
	return strings.Join(out, ",")
}

type MPV struct {
	binary   string
	base     string
	socket   string
	launches atomic.Int64
	shaders  string
	log      *slog.Logger

	playMu sync.Mutex
	mu     sync.Mutex
	cmd    *exec.Cmd
	conn   net.Conn
	exited chan struct{}
	events chan Event
	reqID  atomic.Int64

	opts     Options
	lastSkip float64

	listMu            sync.Mutex
	options, profiles string
}

func (m *MPV) Warm() {
	go func() {
		m.takes("")
		m.profile()
	}()
}

func (m *MPV) takes(option string) bool {
	return strings.Contains(m.listing(&m.options, "--list-options"), "--"+option+" ")
}

func (m *MPV) listing(kept *string, arg string) string {
	m.listMu.Lock()
	defer m.listMu.Unlock()
	if *kept == "" {
		out, err := m.ask(arg)
		if out == "" && err == nil {
			out = " "
		}
		*kept = out
	}
	return *kept
}

func (m *MPV) profile(names ...string) string {
	listed := strings.Fields(m.listing(&m.profiles, "--profile=help"))
	for _, name := range names {
		if slices.Contains(listed, name) {
			return name
		}
	}
	return ""
}

func (m *MPV) ask(arg string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := proc.Hide(exec.CommandContext(ctx, m.binary, "--no-config", arg)).Output()
	return string(out), err
}

func New(binary, socket string, log *slog.Logger) *MPV {
	if socket == "" {
		socket = defaultSocket()
	}
	return &MPV{
		binary:  binary,
		base:    socket,
		socket:  socket,
		log:     log,
		events:  make(chan Event, eventBuffer),
		shaders: filepath.Join(filepath.Dir(binary), "shaders"),
	}
}

func (m *MPV) WithShaders(dir string) *MPV {
	if dir != "" {
		m.shaders = dir
	}
	return m
}

const eventBuffer = 64

func (m *MPV) Events() <-chan Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events
}

func (m *MPV) Play(ctx context.Context, opts Options) error {
	m.playMu.Lock()
	defer m.playMu.Unlock()
	m.stop()

	events := make(chan Event, eventBuffer)
	exited := make(chan struct{})
	m.mu.Lock()
	m.opts = opts
	m.lastSkip = -1
	m.events = events
	m.exited = exited
	m.socket = uniqueSocket(m.base, m.launches.Add(1))
	m.mu.Unlock()

	if _, err := os.Stat(m.binary); err != nil {
		return fmt.Errorf("mpv not found at %s", m.binary)
	}

	args := []string{
		"--input-ipc-server=" + m.socket,
		"--force-window=immediate",
		"--keep-open=no",
		"--title=" + orDefault(opts.Title, "Yoru"),
		"--cache=yes",
		"--demuxer-max-bytes=256MiB",
		"--demuxer-readahead-secs=30",
	}
	if opts.StartAt > 0 {
		args = append(args, fmt.Sprintf("--start=%.3f", opts.StartAt))
	}
	if opts.ChaptersFile != "" {
		args = append(args, "--chapters-file="+opts.ChaptersFile)
	}
	if alang := AudioLanguages(opts.Audio); alang != "" {
		args = append(args, "--alang="+alang)
	}
	if slang := SubtitleLanguages(opts.SubLanguages); slang != "" {
		args = append(args, "--slang="+slang)
	}
	args = append(args, sound.MPVArgs(opts.Sound)...)
	args = append(args, SubtitleLookArgs(opts.SubLook, opts.SubRaise, m.takes)...)
	switch {
	case opts.SoftwareDecode:
	case opts.Soften:
		args = append(args, "--hwdec=auto-copy-safe")
	default:
		args = append(args, "--hwdec=auto-safe")
	}
	if opts.Soften {
		args = append(args, "--vf=lavfi=[format=rgb24,photosensitivity]")
	}
	if opts.Deband {
		args = append(args, "--deband=yes")
	}
	if opts.Anime4K {
		if shaders := m.anime4kArgs(opts.Anime4KMode, opts.Anime4KSize); len(shaders) > 0 {
			if p := m.profile("high-quality", "gpu-hq"); p != "" {
				args = append(args, "--profile="+p)
			}
			args = append(args, "--vo=gpu-next")
			args = append(args, shaders...)
		}
	}
	args = append(args, opts.Extra...)
	args = append(args, opts.URL)

	cmd := exec.Command(m.binary, args...)
	said := &tail{}
	cmd.Stdout, cmd.Stderr = said, said
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start mpv: %w", err)
	}
	m.mu.Lock()
	m.cmd = cmd
	socket := m.socket
	m.mu.Unlock()

	go func() {
		cmd.Wait()
		dropSocket(socket)
		close(exited)
	}()

	conn, err := m.dial(ctx, socket, exited, 15*time.Second)
	if err != nil {
		m.stop()
		if last := said.String(); last != "" {
			err = fmt.Errorf("%w: %s", err, last)
		}
		m.log.Warn("mpv did not start", "err", err)
		return err
	}

	m.mu.Lock()
	current := m.cmd == cmd
	if current {
		m.conn = conn
	}
	m.mu.Unlock()
	if !current {
		conn.Close()
		return errors.New("mpv was stopped while it was starting")
	}

	go m.readLoop(conn, events)
	go m.reap(cmd, events, exited)

	for i, prop := range []string{"time-pos", "duration", "pause"} {
		m.send(map[string]any{"command": []any{"observe_property", i + 1, prop}})
	}
	return nil
}

func (m *MPV) dial(ctx context.Context, socket string, exited <-chan struct{}, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := dialIPC(ctx, socket)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("mpv ipc %s: %w", socket, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-exited:
			return nil, errors.New("mpv closed before it started playing")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - 2048; over > 0 {
		t.buf = t.buf[over:]
	}
	t.mu.Unlock()
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

type ipcMessage struct {
	Event string          `json:"event"`
	Name  string          `json:"name"`
	Data  json.RawMessage `json:"data"`
	Rsn   string          `json:"reason"`
}

func (m *MPV) readLoop(conn net.Conn, events chan Event) {
	scan := bufio.NewScanner(conn)
	scan.Buffer(make([]byte, 0, 8192), 1<<20)

	var duration float64
	for scan.Scan() {
		var msg ipcMessage
		if err := json.Unmarshal(scan.Bytes(), &msg); err != nil {
			continue
		}

		switch msg.Event {
		case "property-change":
			switch msg.Name {
			case "duration":
				json.Unmarshal(msg.Data, &duration)
			case "time-pos":
				var pos float64
				if json.Unmarshal(msg.Data, &pos) == nil {
					m.onPosition(events, pos, duration)
				}
			case "pause":
				var paused bool
				json.Unmarshal(msg.Data, &paused)
				emit(events, Event{Kind: EventPause, Paused: paused, Duration: duration})
			}
		case "end-file":
			deliver(events, Event{Kind: EventEnd, Duration: duration, Reason: msg.Rsn})
		}
	}
}

func (m *MPV) onPosition(events chan Event, pos, duration float64) {
	emit(events, Event{Kind: EventPosition, Position: pos, Duration: duration})

	m.mu.Lock()
	opts, last := m.opts, m.lastSkip
	m.mu.Unlock()

	if !opts.AutoSkip {
		return
	}
	if r, ok := skipTarget(opts.SkipRanges, pos, last); ok {
		m.mu.Lock()
		m.lastSkip = r.End
		m.mu.Unlock()
		m.log.Info("auto-skip", "kind", r.Kind, "to", r.End)
		m.Seek(r.End)
	}
}

func skipTarget(ranges []SkipRange, pos, last float64) (SkipRange, bool) {
	for _, r := range ranges {
		if r.Contains(pos) && last != r.End {
			return r, true
		}
	}
	return SkipRange{}, false
}

func (m *MPV) Seek(seconds float64) error {
	return m.send(map[string]any{"command": []any{"seek", seconds, "absolute"}})
}

func (m *MPV) send(cmd map[string]any) error {
	m.mu.Lock()
	conn := m.conn
	m.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("mpv not running")
	}
	cmd["request_id"] = m.reqID.Add(1)

	payload, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(payload, '\n'))
	return err
}

func (m *MPV) reap(cmd *exec.Cmd, events chan Event, exited chan struct{}) {
	<-exited
	m.mu.Lock()
	var conn net.Conn
	if m.cmd == cmd {
		conn, m.cmd, m.conn = m.conn, nil, nil
	}
	m.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	deliver(events, Event{Kind: EventExit})
}

func (m *MPV) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cmd != nil
}

func (m *MPV) Stop() {
	m.playMu.Lock()
	defer m.playMu.Unlock()
	m.stop()
}

func (m *MPV) stop() {
	m.mu.Lock()
	cmd, conn, exited := m.cmd, m.conn, m.exited
	m.cmd, m.conn, m.exited = nil, nil, nil
	m.mu.Unlock()

	if conn != nil {
		conn.Close()
	}
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		if exited != nil {
			select {
			case <-exited:
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func uniqueSocket(base string, n int64) string {
	ext := filepath.Ext(base)
	if ext != ".sock" {
		ext = ""
	}
	return fmt.Sprintf("%s-%d-%d%s", strings.TrimSuffix(base, ext), os.Getpid(), n, ext)
}

func emit(events chan Event, e Event) {
	select {
	case events <- e:
	default:
	}
}

func deliver(events chan Event, e Event) {
	select {
	case events <- e:
	case <-time.After(30 * time.Second):
	}
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
