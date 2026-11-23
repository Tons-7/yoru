package transcode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractGrowsWithTheSource(t *testing.T) {
	ffmpeg, _ := binaries(t)
	whole := testMKV(t, ffmpeg, 30)

	dir := t.TempDir()
	part := filepath.Join(dir, "part.mkv")
	data, err := os.ReadFile(whole)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(part, data[:len(data)*4/10], 0o644); err != nil {
		t.Fatal(err)
	}

	subs := NewSubtitles(ffmpeg)
	ctx := context.Background()
	out := t.TempDir()

	path, err := subs.Extract(ctx, part, out, 2, "ass", false)
	if err != nil {
		t.Fatalf("partial extract: %v", err)
	}
	partial := dialogueLines(path)
	if partial <= 0 {
		t.Fatalf("no cues recovered from the downloaded part")
	}

	if err := os.WriteFile(part, data, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	os.Chtimes(path, old, old)

	path, err = subs.Extract(ctx, part, out, 2, "ass", false)
	if err != nil {
		t.Fatalf("grown extract: %v", err)
	}
	full := dialogueLines(path)
	if full <= partial {
		t.Fatalf("track did not grow: %d then %d cues", partial, full)
	}

	if err := os.WriteFile(part, data[:len(data)/10], 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(path, old, old)
	path, err = subs.Extract(ctx, part, out, 2, "ass", false)
	if err != nil {
		t.Fatalf("shrunken extract: %v", err)
	}
	if got := dialogueLines(path); got < full {
		t.Fatalf("a worse read replaced the track: %d after %d", got, full)
	}

	if err := os.WriteFile(part, data, 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(path, old, old)
	if _, err := subs.Extract(ctx, part, out, 2, "ass", true); err != nil {
		t.Fatalf("complete extract: %v", err)
	}
	subs.mu.Lock()
	_, done := subs.done[path]
	subs.mu.Unlock()
	if !done {
		t.Error("a complete extraction should be remembered as done")
	}
}
