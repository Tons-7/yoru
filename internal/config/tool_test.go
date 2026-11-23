package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExeNameSuffixesOnlyOnWindows(t *testing.T) {
	got := ExeName("ffmpeg")
	want := "ffmpeg"
	if runtime.GOOS == "windows" {
		want = "ffmpeg.exe"
	}
	if got != want {
		t.Errorf("ExeName = %q, want %q", got, want)
	}
}

func TestToolPrefersBinThenPath(t *testing.T) {
	bin := t.TempDir()
	c := Config{BinDir: bin}

	if got := c.Tool("nedb-nonesuch"); got != filepath.Join(bin, ExeName("nedb-nonesuch")) {
		t.Errorf("missing tool = %q, want the bin path", got)
	}

	name := "yoru-tool-test"
	inBin := filepath.Join(bin, ExeName(name))
	if err := os.WriteFile(inBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := c.Tool(name); got != inBin {
		t.Errorf("bin tool = %q, want %q", got, inBin)
	}

	pathDir := t.TempDir()
	onPath := filepath.Join(pathDir, ExeName("yoru-path-tool"))
	if err := os.WriteFile(onPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)
	if got := (Config{BinDir: bin}).Tool("yoru-path-tool"); got != onPath {
		t.Errorf("path tool = %q, want %q", got, onPath)
	}
}

func TestFfprobePairsWithFfmpeg(t *testing.T) {
	bin, pathDir := t.TempDir(), t.TempDir()
	for _, p := range []string{
		filepath.Join(bin, ExeName("ffprobe")),
		filepath.Join(pathDir, ExeName("ffmpeg")),
		filepath.Join(pathDir, ExeName("ffprobe")),
	} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", pathDir)
	if got, want := (Config{BinDir: bin}).Tool("ffprobe"), filepath.Join(pathDir, ExeName("ffprobe")); got != want {
		t.Errorf("ffprobe = %q, want the one beside ffmpeg %q", got, want)
	}
}
