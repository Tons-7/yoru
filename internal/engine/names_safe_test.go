package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"yoru/internal/bt/bencode"
	"yoru/internal/bt/metainfo"
)

// A torrent named like a folder Yoru keeps in the cache would be swept with it on the next start.
func TestLayoutAvoidsYorusOwnFolders(t *testing.T) {
	l := newLayout(t.TempDir())
	for i, name := range []string{"hls", "engine-session", "Update", "thumbs", "chapters-12"} {
		if got := l.top(name, metainfo.Hash{byte(i + 1)}); strings.EqualFold(got, name) {
			t.Errorf("%q kept its reserved name", name)
		}
	}
}

// Windows and macOS put names differing only in case in one folder; deleting either would take both.
func TestLayoutKeepsCaseOnlyDifferentNamesApart(t *testing.T) {
	l := newLayout(t.TempDir())
	a := l.top("Show Pack", metainfo.Hash{1})
	b := l.top("show pack", metainfo.Hash{2})
	if strings.EqualFold(a, b) {
		t.Fatalf("both under %q", a)
	}
}

// Restored with no saved name, a torrent keeps its own, which its files are under: tagging it now would
// fetch it again. New ones still get the rules.
func TestRestoredTorrentsKeepTheirOwnNames(t *testing.T) {
	l := newLayout(t.TempDir())
	l.claim(metainfo.Hash{1}, "")
	l.claim(metainfo.Hash{2}, "")
	l.claim(metainfo.Hash{3}, "")
	if got := l.top("Show Pack", metainfo.Hash{1}); got != "Show Pack" {
		t.Errorf("first restored: %q", got)
	}
	if got := l.top("show pack", metainfo.Hash{2}); got != "show pack" {
		t.Errorf("a restored case twin was renamed: %q", got)
	}
	if got := l.top("hls", metainfo.Hash{3}); got != "hls" {
		t.Errorf("a restored torrent named like Yoru's folder was renamed: %q", got)
	}
	if got := l.top("SHOW PACK", metainfo.Hash{4}); strings.EqualFold(got, "show pack") {
		t.Errorf("a new add shares a restored one's folder: %q", got)
	}
}

// A damaged session is rebuilt from what is on disk, a torrent tagged after a clash included, where it is.
func TestRebuildFindsTaggedFolders(t *testing.T) {
	cache, sessions := t.TempDir(), t.TempDir()
	info := metainfo.Info{Name: "Show Pack", PieceLength: 16 << 10, Pieces: make([]byte, 20),
		Files: []metainfo.FileInfo{{Path: []string{"Show - 01.mkv"}, Length: 100}, {Path: []string{"Show - 02.mkv"}, Length: 100}}}
	body, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: body}
	hash := mi.HashInfoBytes()
	if err := writeMetainfo(filepath.Join(sessions, hash.HexString()+".torrent"), mi); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(cache, suffixed("Show Pack", hash))
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "Show - 02.mkv"), []byte("x"), 0o644)

	e := &Engine{opts: Options{CacheDir: cache, SessionDir: sessions}}
	doc := e.rebuildSession()
	if len(doc.Torrents) != 1 {
		t.Fatalf("rebuilt %d torrents", len(doc.Torrents))
	}
	if r := doc.Torrents[0]; r.Top != suffixed("Show Pack", hash) || !slices.Equal(r.OnlyFiles, []int{1}) {
		t.Errorf("rebuilt %+v, want the tagged folder with episode 2 selected", r)
	}
}

// A renamed single file keeps its extension, so it still opens as video.
func TestSuffixGoesBeforeTheExtension(t *testing.T) {
	l := newLayout(t.TempDir())
	l.top("Show - 01.mkv", metainfo.Hash{1})
	got := l.top("Show - 01.mkv", metainfo.Hash{2, 0xab})
	if !strings.HasSuffix(got, ".mkv") || got == "Show - 01.mkv" {
		t.Fatalf("second copy named %q", got)
	}
	// A folder name with a dot is not an extension to keep.
	l.top("Show S01 1080p.WEB", metainfo.Hash{3})
	if got := l.top("Show S01 1080p.WEB", metainfo.Hash{4}); strings.HasSuffix(got, ".WEB") {
		t.Errorf("pack folder named %q", got)
	}
}

// On NTFS ':' writes into a hidden stream and '?' fails every write.
func TestSafeNameOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		if got := safeName("Re:Zero?"); got != "Re:Zero?" {
			t.Fatalf("changed off Windows: %q", got)
		}
		return
	}
	for in, bad := range map[string]string{
		"Re:Zero - 01.mkv":         ":",
		"Why? - 01.mkv":            "?",
		`Say "Hi" - 01.mkv`:        `"`,
		"Kaguya-sama: Love Is War": ":",
		"name. ":                   ". ",
	} {
		got := safeName(in)
		if strings.Contains(got, bad) && !strings.HasSuffix(in, ". ") || strings.HasSuffix(got, " ") || strings.HasSuffix(got, ".") {
			t.Errorf("safeName(%q) = %q", in, got)
		}
	}
	for _, reserved := range []string{"CON", "nul.mkv", "COM1"} {
		if got := safeName(reserved); strings.EqualFold(got, reserved) {
			t.Errorf("reserved %q kept", reserved)
		}
	}
	if got := safeName("[Group] Show - 05 (1080p).mkv"); got != "[Group] Show - 05 (1080p).mkv" {
		t.Errorf("an ordinary name changed: %q", got)
	}
}
