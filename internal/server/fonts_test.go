package server

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yoru/internal/config"
	"yoru/internal/transcode"
)

// One build of a file at a time, even while its session is forgotten around it.
func TestBuildOnceHoldsAsTheSessionGoes(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	h.server.streams = transcode.NewManager("ffmpeg", "ffprobe", t.TempDir(), "libx264", slog.New(slog.DiscardHandler))
	dir := h.server.streams.DirPrefix("1-2") + "1"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "jimaku-x.ass")
	var inside, overlapped atomic.Int32
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			h.server.buildOnce(path, func(string) error {
				if inside.Add(1) > 1 {
					overlapped.Add(1)
				}
				time.Sleep(100 * time.Microsecond)
				inside.Add(-1)
				// Never made, so every caller builds in turn.
				return errors.New("not made")
			})
		}()
		go func() {
			defer wg.Done()
			h.server.SessionGone("1-2")
		}()
	}
	wg.Wait()
	if n := overlapped.Load(); n > 0 {
		t.Errorf("%d builds of the same file ran at once", n)
	}
}

// Fonts are kept per session folder: closing an episode forgets all of its folders, and only its own.
func TestSessionGoneForgetsOnlyItsFonts(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	h.server.streams = transcode.NewManager("ffmpeg", "ffprobe", t.TempDir(), "libx264", slog.New(slog.DiscardHandler))
	mine, replaced := h.server.streams.DirPrefix("1-2")+"7", h.server.streams.DirPrefix("1-2")+"3"
	other := h.server.streams.DirPrefix("1-20") + "4"
	device := h.server.streams.DirPrefix("1-2-dev0a1b2c3d") + "5"
	h.server.fonts[mine] = []transcode.FontFile{}
	h.server.fonts[other] = []transcode.FontFile{}
	h.server.fonts[device] = []transcode.FontFile{}
	h.server.fontJobs[replaced] = struct{}{}
	h.server.building.Store(filepath.Join(mine, "jimaku-1.ass"), &sync.Mutex{})

	h.server.SessionGone("1-2")
	if _, ok := h.server.fonts[device]; !ok {
		t.Error("a device's own session of the episode lost its fonts with the episode's")
	}
	if _, ok := h.server.building.Load(filepath.Join(mine, "jimaku-1.ass")); ok {
		t.Error("the closed session's build locks are still held")
	}
	if _, ok := h.server.fonts[mine]; ok {
		t.Error("the closed session's fonts are still listed")
	}
	if _, ok := h.server.fontJobs[replaced]; ok {
		t.Error("an extraction for the replaced session is still marked running")
	}
	if _, ok := h.server.fonts[other]; !ok {
		t.Error("episode 20's fonts went with episode 2's")
	}
}
