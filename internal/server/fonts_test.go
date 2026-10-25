package server

import (
	"log/slog"
	"path/filepath"
	"sync"
	"testing"

	"yoru/internal/config"
	"yoru/internal/transcode"
)

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
