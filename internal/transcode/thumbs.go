package transcode

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"yoru/internal/proc"
)

type Thumbnails struct {
	ffmpeg string
	log    *slog.Logger

	mu       sync.Mutex
	running  map[string]struct{}
	failed   map[string]time.Time
	partials map[string]*partial
}

func NewThumbnails(ffmpeg string, log *slog.Logger) *Thumbnails {
	return &Thumbnails{
		ffmpeg:   absolute(ffmpeg),
		log:      log,
		running:  map[string]struct{}{},
		failed:   map[string]time.Time{},
		partials: map[string]*partial{},
	}
}

type Sheet struct {
	Ready    bool    `json:"ready"`
	Interval float64 `json:"interval"`
	Columns  int     `json:"columns"`
	Rows     int     `json:"rows"`
	Count    int     `json:"count"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	Partial  bool    `json:"partial,omitempty"`
	Have     string  `json:"have,omitempty"`
	Version  int     `json:"version,omitempty"`
}

const (
	thumbWidth    = 160
	thumbTarget   = 144
	thumbDeadline = 5 * time.Minute
	thumbRetry    = time.Hour
)

func sheetPath(dir string) string { return filepath.Join(dir, "thumbs.jpg") }

func sheetArgs(source, filter, part string) []string {
	return []string{"-hide_banner", "-loglevel", "error", "-y", "-skip_frame", "nokey", "-i", source,
		"-vf", filter, "-frames:v", "1", "-q:v", "6", part}
}

func tail(out string) string {
	const keep = 300
	out = strings.TrimSpace(out)
	if len(out) <= keep {
		return out
	}
	return out[len(out)-keep:]
}

func PlanSheet(duration float64) Sheet {
	if duration <= 0 {
		return Sheet{}
	}

	interval := math.Max(2, math.Round(duration/thumbTarget))
	count := max(1, int(math.Ceil((duration-1)/interval)))
	columns := int(math.Ceil(math.Sqrt(float64(count))))
	rows := int(math.Ceil(float64(count) / float64(columns)))

	return Sheet{
		Interval: interval,
		Columns:  columns,
		Rows:     rows,
		Count:    count,
		Width:    thumbWidth,
	}
}

func (t *Thumbnails) Read(dir string, duration float64, aspect float64) (Sheet, bool) {
	plan := PlanSheet(duration)
	if plan.Count == 0 {
		return Sheet{}, false
	}
	plan.Height = tileHeight(aspect)

	if _, err := os.Stat(sheetPath(dir)); err != nil {
		if have := readPartial(dir); len(have) == plan.Count {
			plan.Ready, plan.Partial, plan.Have, plan.Version = true, true, have, strings.Count(have, "1")
		}
		return plan, false
	}
	plan.Ready = true
	return plan, true
}

func SheetFile(dir string) string {
	for _, path := range []string{sheetPath(dir), partialSheet(dir)} {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func tileHeight(aspect float64) int {
	if aspect <= 0 {
		aspect = 16.0 / 9.0
	}
	h := int(math.Round(float64(thumbWidth) / aspect))
	if h%2 != 0 {
		h++
	}
	return h
}

func (t *Thumbnails) Generate(source, dir string, duration, aspect float64) {
	plan := PlanSheet(duration)
	if t == nil || plan.Count == 0 || source == "" {
		return
	}
	if _, err := os.Stat(sheetPath(dir)); err == nil {
		return
	}

	t.mu.Lock()
	if _, busy := t.running[dir]; busy || time.Since(t.failed[dir]) < thumbRetry {
		t.mu.Unlock()
		return
	}
	t.running[dir] = struct{}{}
	t.mu.Unlock()

	go func() {
		defer func() {
			t.mu.Lock()
			delete(t.running, dir)
			t.mu.Unlock()
		}()

		ctx, cancel := context.WithTimeout(context.Background(), thumbDeadline)
		defer cancel()

		part := filepath.Join(dir, "thumbs.part.jpg")
		filter := fmt.Sprintf("tpad=stop_mode=clone:stop_duration=%g,fps=1/%g,scale=%d:%d,tile=%dx%d",
			duration, plan.Interval, thumbWidth, tileHeight(aspect), plan.Columns, plan.Rows)

		started := time.Now()
		out, err := proc.Background(exec.CommandContext(ctx, t.ffmpeg, sheetArgs(source, filter, part)...)).CombinedOutput()
		if err != nil {
			t.log.Warn("scrub previews", "err", err, "detail", tail(string(out)))
			os.Remove(part)
			t.mu.Lock()
			maps.DeleteFunc(t.failed, func(_ string, at time.Time) bool { return time.Since(at) >= thumbRetry })
			t.failed[dir] = time.Now()
			t.mu.Unlock()
			return
		}

		if err := os.Rename(part, sheetPath(dir)); err != nil {
			t.log.Warn("scrub previews", "err", err)
			return
		}
		t.dropPartial(dir)
		t.log.Info("scrub previews ready",
			"frames", plan.Count, "took", time.Since(started).Round(time.Second))
	}()
}
