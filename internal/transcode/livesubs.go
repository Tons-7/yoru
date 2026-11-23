package transcode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"yoru/internal/proc"
)

func liveName(track, from int) string { return fmt.Sprintf("live-%d-%d.ass", track, from) }

func (s *Session) subtitleArgs(offset float64, from int) []string {
	outputs := s.liveSubtitleArgs(from)
	if len(outputs) == 0 {
		return nil
	}
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
	if offset > 0 {
		a = append(a, "-ss", fmt.Sprintf("%.3f", offset))
	}
	a = append(a,
		"-readrate", fmt.Sprintf("%.1f", readRate),
		"-readrate_initial_burst", fmt.Sprintf("%.0f", initialBurstSeconds),
	)
	a = append(a, reconnectArgs(s.Source)...)
	a = append(a,
		"-i", s.Source,
		"-copyts",
	)
	return append(a, outputs...)
}

func (s *Session) liveSubtitleArgs(from int) []string {
	if !s.liveSubs || s.Info == nil {
		return nil
	}
	var a []string
	for _, sub := range s.Info.Subtitles {
		if !Renderable(sub.Codec) {
			continue
		}
		codec := "ass"
		if isASS(sub.Codec) {
			codec = "copy"
		}
		a = append(a,
			"-map", fmt.Sprintf("0:%d?", sub.Index), "-c:s", codec, "-f", "ass",
			"-ignore_readorder", "1", "-flush_packets", "1",
			"-y", liveName(sub.Index, from),
		)
	}
	return a
}

func (s *Session) LiveSubtitles() (start float64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.liveSubs || s.run == nil || !s.run.subsAlive.Load() {
		return 0, false
	}
	return s.segmentStart(s.headFrom), true
}

var liveHeaderWait = 5 * time.Second

func (s *Subtitles) AwaitLive(ctx context.Context, dir string, track int, codec string) (string, bool) {
	deadline := time.After(liveHeaderWait)
	for {
		if path, err := s.MergeLive(dir, track, codec); err == nil {
			if text, readErr := os.ReadFile(path); readErr == nil && strings.Contains(string(text), "[Events]") {
				return path, true
			}
		}
		select {
		case <-ctx.Done():
			return "", false
		case <-deadline:
			return "", false
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func LiveCovers(start, at float64) bool { return start == 0 || at >= start+aroundBefore }

func (s *Session) startSubtitles(r *run) {
	args := s.subtitleArgs(s.segmentStart(r.from), r.from)
	if args == nil {
		return
	}
	cmd := proc.Hide(exec.Command(s.ffmpeg, args...))
	cmd.Dir = s.dir
	var stderr tailBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		s.log.Warn("subtitle process did not start", "session", s.ID, "err", err)
		return
	}
	r.subs = cmd
	r.subsGone = make(chan struct{})
	r.subsAlive.Store(true)
	go func() {
		defer close(r.subsGone)
		err := cmd.Wait()
		r.subsAlive.Store(false)
		if err != nil && !r.subsStopped.Load() {
			s.log.Warn("subtitle process failed", "session", s.ID, "err", err,
				"detail", lastLines(stderr.String(), encoderLogLines))
		}
	}()
}

func (r *run) stopSubtitles() {
	if r.subs == nil || r.subs.Process == nil {
		return
	}
	r.subsStopped.Store(true)
	r.subs.Process.Kill()
	select {
	case <-r.subsGone:
	case <-time.After(time.Second):
	}
}

func (s *Subtitles) MergeLive(dir string, track int, codec string) (string, error) {
	path := filepath.Join(dir, fmt.Sprintf("sub-%d.%s", track, subtitleExt(codec)))
	lives, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("live-%d-*.ass", track)))
	if len(lives) == 0 {
		return path, nil
	}

	one := s.lockPath(path)
	defer one.Unlock()

	have, _ := os.ReadFile(path)
	merged := string(have)
	for _, live := range lives {
		raw, err := os.ReadFile(live)
		if err != nil {
			continue
		}
		text := string(raw)
		if i := strings.LastIndexByte(text, '\n'); i >= 0 {
			text = text[:i+1]
		} else {
			continue
		}
		merged = mergeASS(merged, text)
	}
	if merged == string(have) {
		return path, nil
	}
	staging := path + ".live"
	if err := os.WriteFile(staging, []byte(merged), 0o644); err != nil {
		return path, err
	}
	err := os.Rename(staging, path)
	for try := 1; err != nil && try <= 5; try++ {
		time.Sleep(time.Duration(try) * 20 * time.Millisecond)
		err = os.Rename(staging, path)
	}
	if err != nil {
		os.Remove(staging)
		return path, err
	}
	return path, nil
}
