// Package player drives mpv over its JSON IPC socket (a named pipe on Windows), and VLC over HTTP.
package player

import (
	"bufio"
	"context"
	"encoding/json"
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
	URL          string
	Title        string
	StartAt      float64
	ChaptersFile string
	SkipRanges   []SkipRange
	AutoSkip     bool
	Anime4K      bool
	Anime4KMode  string
	Anime4KSize  string
	// Audio is the sub/dub preference; mpv picks the matching track of a dual-audio file by language.
	Audio string
	// SubLanguages are the settings' ISO 639-1 codes, best first.
	SubLanguages []string
	// Sound is what to do to the audio: dialogue lift, night mode, headphones, mono.
	Sound sound.Options
	// SubLook is the settings' subtitle look ("", "outline", "yellow", "box"); see SubtitleLookArgs.
	SubLook string
	// SoftwareDecode turns off mpv's GPU decoding, which it otherwise uses where it's known safe.
	SoftwareDecode bool
	// Deband smooths the banding of flat gradients, which the high-quality profile no longer does.
	Deband bool
	// Soften dims sudden bright flashes (ffmpeg's photosensitivity filter).
	Soften bool
	Extra  []string
}

// SubtitleLookArgs: a look for mpv's text subtitles (ASS keeps its own), named as this mpv names its options.
func SubtitleLookArgs(look string, takes func(option string) bool) []string {
	if look == "box" && !takes("sub-border-style") {
		look = "outline"
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
		return []string{"--sub-color=" + color, "--sub-" + edge + "-color=#000000", "--sub-" + edge + "-size=3", "--sub-shadow-offset=1"}
	case "box":
		return []string{"--sub-color=#FFFFFF", "--sub-border-style=background-box", "--sub-back-color=#99000000"}
	}
	return nil
}

// AudioLanguages is mpv's --alang list for a preference, empty when either track will do.
func AudioLanguages(pref string) string {
	switch pref {
	case "dub":
		return "eng,en,english"
	case "sub":
		return "jpn,ja,jp,japanese"
	}
	return ""
}

// languageTags are how files tag a language's tracks, matched as Player.tsx does.
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

// SubtitleLanguages is mpv's --slang list for the settings' codes, in order.
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
	binary string
	// base names the IPC socket; each launch gets its own, so a dying mpv and a second Yoru never answer.
	base     string
	socket   string
	launches atomic.Int64
	shaders  string
	log      *slog.Logger

	mu     sync.Mutex
	cmd    *exec.Cmd
	conn   net.Conn
	exited chan struct{}
	events chan Event
	reqID  atomic.Int64

	opts     Options
	lastSkip float64

	// The options and profiles this mpv has: an older one refuses newer ones and won't start.
	listMu            sync.Mutex
	options, profiles string
}

// Warm reads what this mpv takes in the background, so the first Play doesn't wait on it.
func (m *MPV) Warm() {
	go func() {
		m.takes("")
		m.profile()
	}()
}

// takes says whether this mpv has an option; a build that won't list them is taken at its word for none.
func (m *MPV) takes(option string) bool {
	return strings.Contains(m.listing(&m.options, "--list-options"), "--"+option+" ")
}

// listing is mpv's answer to arg, kept once given; none (mpv not installed yet) is asked again next time.
func (m *MPV) listing(kept *string, arg string) string {
	m.listMu.Lock()
	defer m.listMu.Unlock()
	if *kept == "" {
		*kept = m.ask(arg)
	}
	return *kept
}

// profile is the first of these profiles this mpv has (high-quality came in 0.37, gpu-hq is older), or "".
func (m *MPV) profile(names ...string) string {
	listed := strings.Fields(m.listing(&m.profiles, "--profile=help"))
	for _, name := range names {
		if slices.Contains(listed, name) {
			return name
		}
	}
	return ""
}

// ask runs mpv for a listing, given up after a while: a hung build mustn't hold up playing.
func (m *MPV) ask(arg string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, _ := proc.Hide(exec.CommandContext(ctx, m.binary, "--no-config", arg)).Output()
	return string(out)
}

func New(binary, socket string, log *slog.Logger) *MPV {
	if socket == "" {
		socket = defaultSocket()
	}
	return &MPV{
		binary: binary,
		base:   socket,
		socket: socket,
		log:    log,
		events: make(chan Event, eventBuffer),
		// The Anime4K shaders sit beside the binary, so no location is configured.
		shaders: filepath.Join(filepath.Dir(binary), "shaders"),
	}
}

const eventBuffer = 64

// Events is the current run's: a late exit from the last mpv ended the next episode's tracking.
func (m *MPV) Events() <-chan Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.events
}

func (m *MPV) Play(ctx context.Context, opts Options) error {
	m.Stop()

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
		// The stream is served over HTTP by the engine; without a generous cache
		// mpv re-requests aggressively and fights the piece scheduler.
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
	args = append(args, SubtitleLookArgs(opts.SubLook, m.takes)...)
	// auto-safe: mpv's own list of GPU decoders that don't break playback; HEVC 10-bit off the CPU.
	switch {
	case opts.SoftwareDecode:
	case opts.Soften:
		// The flash filter works on frames in memory, which the copy-back decoders hand over.
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
		// gpu-next runs the shader chain well; the profile only raises the scaling defaults.
		if p := m.profile("high-quality", "gpu-hq"); p != "" {
			args = append(args, "--profile="+p)
		}
		args = append(args, "--vo=gpu-next")
		args = append(args, m.anime4kArgs(opts.Anime4KMode, opts.Anime4KSize)...)
	}
	args = append(args, opts.Extra...)
	args = append(args, opts.URL)

	cmd := exec.Command(m.binary, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start mpv: %w", err)
	}

	m.mu.Lock()
	m.cmd = cmd
	m.mu.Unlock()

	conn, err := m.dial(ctx, 15*time.Second)
	if err != nil {
		// Reaped before Stop waits on it, or Stop sits out its whole wait and the process is never collected.
		go func() {
			cmd.Wait()
			close(exited)
		}()
		m.Stop()
		return err
	}

	m.mu.Lock()
	m.conn = conn
	m.mu.Unlock()

	go m.readLoop(conn, events)
	go m.reap(cmd, events, exited)

	for i, prop := range []string{"time-pos", "duration", "pause"} {
		m.send(map[string]any{"command": []any{"observe_property", i + 1, prop}})
	}
	return nil
}

// mpv creates the pipe only after it starts, so connecting is a retry loop.
func (m *MPV) dial(ctx context.Context, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for {
		conn, err := dialIPC(ctx, m.socket)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("mpv ipc %s: %w", m.socket, err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
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

// skipTarget is pos's range unless it was the last skipped, or landing on r.End loops.
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

func (m *MPV) SetPause(paused bool) error {
	return m.send(map[string]any{"command": []any{"set_property", "pause", paused}})
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
	cmd.Wait()
	close(exited)
	m.mu.Lock()
	var conn net.Conn
	if m.cmd == cmd {
		conn, m.cmd, m.conn = m.conn, nil, nil
	}
	m.mu.Unlock()
	// mpv exiting on its own never reaches Stop, so the handle is dropped here.
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

// Stop ends the player and waits briefly for it to go, so the next launch does not share its moment.
func (m *MPV) Stop() {
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

// uniqueSocket is the IPC name for one launch: the base with the pid and a counter, before any extension.
func uniqueSocket(base string, n int64) string {
	ext := filepath.Ext(base)
	if ext != ".sock" {
		ext = ""
	}
	return fmt.Sprintf("%s-%d-%d%s", strings.TrimSuffix(base, ext), os.Getpid(), n, ext)
}

// Dropping events is correct: a stalled consumer must not block playback.
func emit(events chan Event, e Event) {
	select {
	case events <- e:
	default:
	}
}

// deliver is emit for an end or an exit, which the tracker must see to save, unpin and move on: it waits
// for room (the tracker may be in a database write), but not for a tracker that has gone.
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
