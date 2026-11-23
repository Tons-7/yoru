package player

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"yoru/internal/sound"
)

type VLC struct {
	binary string
	log    *slog.Logger
	http   *http.Client

	playMu sync.Mutex
	mu     sync.Mutex
	cmd    *exec.Cmd
	ctl    control
	events chan Event
}

type control struct {
	port     int
	password string
}

func NewVLC(binary string, log *slog.Logger) *VLC {
	return &VLC{
		binary: binary,
		log:    log,
		http:   &http.Client{Timeout: 2 * time.Second},
		events: make(chan Event),
	}
}

func ResolveVLC(configured string) string {
	if configured == "" {
		return FindVLC()
	}
	if info, err := os.Stat(configured); err == nil && info.IsDir() {
		configured = insideInstall(configured)
	}
	if _, err := os.Stat(configured); err != nil {
		return ""
	}
	return configured
}

func insideInstall(dir string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(dir, "Contents", "MacOS", "VLC")
	}
	name := "vlc"
	if runtime.GOOS == "windows" {
		name = "vlc.exe"
	}
	return filepath.Join(dir, name)
}

func FindVLC() string {
	if p, err := exec.LookPath("vlc"); err == nil {
		return p
	}
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
			if dir := os.Getenv(env); dir != "" {
				candidates = append(candidates, filepath.Join(dir, "VideoLAN", "VLC", "vlc.exe"))
			}
		}
	case "darwin":
		candidates = append(candidates, "/Applications/VLC.app/Contents/MacOS/VLC")
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func (v *VLC) args(opts Options, ctl control) []string {
	args := []string{
		"--no-one-instance",
		"--extraintf=http",
		"--http-host=127.0.0.1",
		fmt.Sprintf("--http-port=%d", ctl.port),
		"--http-password=" + ctl.password,
		"--meta-title=" + orDefault(opts.Title, "Yoru"),
	}
	if opts.StartAt > 0 {
		args = append(args, fmt.Sprintf("--start-time=%.3f", opts.StartAt))
	}
	if alang := AudioLanguages(opts.Audio); alang != "" {
		args = append(args, "--audio-language="+alang)
	}
	if slang := SubtitleLanguages(opts.SubLanguages); slang != "" {
		args = append(args, "--sub-language="+slang)
	}
	args = append(args, sound.VLCArgs(opts.Sound)...)
	return append(args, opts.URL)
}

func newControl() (control, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return control{}, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return control{}, err
	}
	return control{port: port, password: hex.EncodeToString(b[:])}, nil
}

func (v *VLC) Play(_ context.Context, opts Options) error {
	v.playMu.Lock()
	defer v.playMu.Unlock()
	v.stop()
	if v.binary == "" {
		return fmt.Errorf("VLC is not installed")
	}
	if _, err := os.Stat(v.binary); err != nil {
		return fmt.Errorf("VLC not found at %s", v.binary)
	}
	ctl, err := newControl()
	if err != nil {
		return err
	}

	cmd := exec.Command(v.binary, v.args(opts, ctl)...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start VLC: %w", err)
	}
	events := make(chan Event, 8)
	v.mu.Lock()
	v.cmd, v.ctl, v.events = cmd, ctl, events
	v.mu.Unlock()
	v.log.Info("playing in VLC", "title", opts.Title, "port", ctl.port)

	go v.watch(cmd, ctl, opts, events)
	return nil
}

type vlcStatus struct {
	State    string  `json:"state"`
	Time     float64 `json:"time"`
	Length   float64 `json:"length"`
	Position float64 `json:"position"`
}

func (v *VLC) watch(cmd *exec.Cmd, ctl control, opts Options, events chan Event) {
	defer close(events)
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()

	emit := func(e Event) {
		select {
		case events <- e:
		default:
		}
	}
	var started, paused, ended, reached bool
	var failures int
	const unreachableAfter = 30
	var lastPos, lastLen float64
	lastSkip := -1.0
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		select {
		case <-exited:
			v.mu.Lock()
			if v.cmd == cmd {
				v.cmd = nil
			}
			v.mu.Unlock()
			if !ended {
				deliver(events, Event{Kind: EventExit, Position: lastPos, Duration: lastLen, Reason: "vlc"})
			}
			return
		case <-tick.C:
		}
		if ended {
			continue
		}
		st, err := v.status(ctl)
		if err != nil {
			if failures++; failures == unreachableAfter && !reached {
				v.log.Warn("VLC control interface unreachable; this episode will not be tracked",
					"port", ctl.port, "err", err)
			}
			continue
		}
		reached = true
		switch st.State {
		case "playing", "paused":
			started = true
			pos := st.Time
			if st.Length > 0 && st.Position > 0 {
				pos = st.Position * st.Length
			}
			if p := st.State == "paused"; p != paused {
				paused = p
				emit(Event{Kind: EventPause, Paused: p, Duration: st.Length})
			}
			lastPos, lastLen = pos, st.Length
			emit(Event{Kind: EventPosition, Position: pos, Duration: st.Length})

			if opts.AutoSkip {
				if r, ok := skipTarget(opts.SkipRanges, pos, lastSkip); ok {
					lastSkip = r.End
					v.log.Info("auto-skip", "kind", r.Kind, "to", r.End)
					if err := v.seek(ctl, r.End); err != nil {
						v.log.Warn("VLC seek", "err", err)
					} else {
						lastPos = r.End
					}
				}
			}
		case "stopped":
			if !started {
				continue
			}
			ended = true
			reason := "stop"
			if lastLen > 0 && lastLen-lastPos <= 3 {
				reason = "eof"
			}
			deliver(events, Event{Kind: EventEnd, Position: lastPos, Duration: lastLen, Reason: reason})
			v.mu.Lock()
			if v.cmd == cmd {
				v.cmd = nil
			}
			v.mu.Unlock()
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		}
	}
}

func (v *VLC) status(ctl control) (vlcStatus, error) {
	var st vlcStatus
	res, err := v.request(ctl, "")
	if err != nil {
		return st, err
	}
	defer res.Body.Close()
	err = json.NewDecoder(res.Body).Decode(&st)
	return st, err
}

func (v *VLC) seek(ctl control, seconds float64) error {
	res, err := v.request(ctl, fmt.Sprintf("?command=seek&val=%d", int(math.Ceil(seconds))))
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (v *VLC) request(ctl control, query string) (*http.Response, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/requests/status.json%s", ctl.port, query)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("", ctl.password)
	res, err := v.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("VLC: HTTP %d", res.StatusCode)
	}
	return res, nil
}

func (v *VLC) Events() <-chan Event {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.events
}

func (v *VLC) Running() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.cmd != nil
}

func (v *VLC) Stop() {
	v.playMu.Lock()
	defer v.playMu.Unlock()
	v.stop()
}

func (v *VLC) stop() {
	v.mu.Lock()
	cmd := v.cmd
	v.cmd = nil
	v.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}
