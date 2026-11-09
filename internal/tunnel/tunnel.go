package tunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"yoru/internal/proc"
)

var (
	publicURL  = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
	registered = regexp.MustCompile(`(?i)registered tunnel connection`)
	logPrefix  = regexp.MustCompile(`^\S+\s+[A-Z]{3}\s+`)
)

const OpenWait = 45 * time.Second

type Tunnel struct {
	bin func() string
	log *slog.Logger

	mu  sync.Mutex
	run *run
}

type run struct {
	target string
	cmd    *exec.Cmd
	ready  chan struct{}
	exited chan struct{}
	url    string
	err    error
}

func New(bin func() string, log *slog.Logger) *Tunnel {
	return &Tunnel{bin: bin, log: log}
}

func (r *run) over() bool {
	select {
	case <-r.exited:
		return true
	default:
		return false
	}
}

func (t *Tunnel) Open(ctx context.Context, target string) (string, error) {
	t.mu.Lock()
	r := t.run
	if r == nil || r.target != target || r.over() {
		if r != nil {
			r.kill()
		}
		var err error
		if r, err = t.start(target); err != nil {
			t.run = nil
			t.mu.Unlock()
			return "", err
		}
		t.run = r
	}
	t.mu.Unlock()

	wait := time.NewTimer(OpenWait)
	defer wait.Stop()
	select {
	case <-r.ready:
		return r.url, nil
	case <-r.exited:
		return "", r.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-wait.C:
		return "", errors.New("Cloudflare didn't answer in time")
	}
}

func (t *Tunnel) URL() string {
	t.mu.Lock()
	r := t.run
	t.mu.Unlock()
	if r == nil || r.over() {
		return ""
	}
	select {
	case <-r.ready:
		return r.url
	default:
		return ""
	}
}

func (t *Tunnel) Close() {
	t.mu.Lock()
	r := t.run
	t.run = nil
	t.mu.Unlock()
	if r != nil {
		r.kill()
		t.log.Info("watch together tunnel closed")
	}
}

func (t *Tunnel) start(target string) (*run, error) {
	cmd := proc.Hide(exec.Command(t.bin(), "tunnel", "--no-autoupdate", "--url", target))
	out, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start cloudflared: %w", err)
	}
	r := &run{target: target, cmd: cmd, ready: make(chan struct{}), exited: make(chan struct{})}
	go r.watch(out, t.log)
	return r, nil
}

func (r *run) watch(out io.Reader, log *slog.Logger) {
	lines := bufio.NewScanner(out)
	var url, last string
	opened := false
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line != "" {
			last = line
		}
		if url == "" {
			url = publicURL.FindString(line)
		}
		if !opened && url != "" && registered.MatchString(line) {
			opened = true
			r.url = url
			close(r.ready)
			log.Info("watch together tunnel open", "url", url)
		}
	}
	if err := lines.Err(); err != nil && last == "" {
		last = err.Error()
	}
	err := r.cmd.Wait()
	why := logPrefix.ReplaceAllString(last, "")
	switch {
	case why != "":
		r.err = fmt.Errorf("cloudflared stopped: %s", why)
	case err != nil:
		r.err = fmt.Errorf("cloudflared stopped: %w", err)
	default:
		r.err = errors.New("cloudflared stopped")
	}
	close(r.exited)
	if opened {
		log.Warn("watch together tunnel ended", "err", r.err)
	}
}

func (r *run) kill() {
	if r.cmd.Process != nil {
		r.cmd.Process.Kill()
	}
}
