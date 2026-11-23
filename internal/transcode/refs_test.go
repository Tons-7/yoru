package transcode

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCloseKeepsASessionOtherHoldersStillUse(t *testing.T) {
	m := NewManager("ffmpeg", "ffprobe", t.TempDir(), "libx264", slog.New(slog.NewTextHandler(io.Discard, nil)))
	dir := filepath.Join(t.TempDir(), "1-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Session{ID: "1-1", Source: "a.mkv", Info: &MediaInfo{}, dir: dir, refs: 1, touched: time.Now(), log: m.log}
	m.sessions["1-1"] = s

	got, err := m.Open(context.Background(), "1-1", "a.mkv")
	if err != nil || got != s {
		t.Fatalf("open = %v, %v; want the existing session", got, err)
	}
	if s.Holders() != 2 {
		t.Fatalf("holders = %d, want 2", s.Holders())
	}
	if m.Close("1-1") {
		t.Fatal("the first close released a session someone still holds")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("the first close removed the folder")
	}
	if found, ok := m.Get("1-1"); !ok || found != s {
		t.Fatal("the session went after the first close")
	}
	if !m.Close("1-1") {
		t.Fatal("the last close did not release the session")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the last close left the folder")
	}
	if _, ok := m.Get("1-1"); ok {
		t.Fatal("the session survives its last close")
	}
}

func TestADiscardedPassPublishesNothing(t *testing.T) {
	dir := t.TempDir()
	s := &Session{dir: dir, starts: []float64{0, 6}, Segments: 2, Info: &MediaInfo{Duration: 12}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	stale := &run{id: 1}
	stale.discarded.Store(true)
	parts, _ := writeParts(t, dir, "p1", 100, 200)
	s.publish(stale, 1, parts)
	if s.segmentDone(1) {
		t.Fatal("a discarded pass published its segment")
	}
	if _, err := s.OpenSegment(1); !os.IsNotExist(err) {
		t.Fatalf("open = %v, want not exist", err)
	}
	for _, p := range parts {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s stayed on disk", p)
		}
	}

	live := &run{id: 2}
	good, _ := writeParts(t, dir, "p2", 100, 200)
	s.publish(live, 1, good)
	if !s.segmentDone(1) {
		t.Fatal("a live pass did not publish")
	}
	late, _ := writeParts(t, dir, "p3", 50)
	s.publish(stale, 1, late)
	seg, err := s.OpenSegment(1)
	if err != nil {
		t.Fatal(err)
	}
	defer seg.Close()
	if size := seg.Size(); size != 300 {
		t.Fatalf("segment is %d bytes, want the live pass's 300", size)
	}
}
