package server

import (
	"os"
	"path/filepath"
	"testing"
)

// Subtitle files named after the video are its tracks, the language read from the name; another episode's
// file, or one for another video, is not.
func TestSidecarsAreTheFilesNamedAfterTheVideo(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Show - 01.mkv", "Show - 01.en.ass", "show - 01.ENG.forced.srt", "Show - 01.srt",
		"Show - 010.ass", "Other.ass", "Show - 01.nfo",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := sidecars(filepath.Join(dir, "Show - 01.mkv"))
	want := []sidecar{
		{language: "en", codec: "ass"},
		{codec: "subrip"},
		{language: "eng", title: "eng.forced", codec: "subrip"},
	}
	if len(got) != len(want) {
		t.Fatalf("sidecars = %+v, want %d", got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.language != w.language || g.title != w.title || g.codec != w.codec {
			t.Errorf("sidecar %d = %+v, want %+v", i, g, w)
		}
	}

	if got := sidecars("http://127.0.0.1:1/torrents/1/stream/0"); got != nil {
		t.Errorf("a torrent's stream has sidecars %+v", got)
	}
}
