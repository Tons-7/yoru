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
	"yoru/internal/torrent"
)

func TestLayoutAvoidsYorusOwnFolders(t *testing.T) {
	l := newLayout(t.TempDir())
	for i, name := range []string{"hls", "engine-session", "Update", "thumbs", "chapters-12-3.ini"} {
		if got := l.top(name, metainfo.Hash{byte(i + 1)}); strings.EqualFold(got, name) || torrent.OwnFolder(got) {
			t.Errorf("%q kept its reserved name", name)
		}
	}
	if got := l.top("Chapters-1-12 [BD]", metainfo.Hash{9}); got != "Chapters-1-12 [BD]" {
		t.Errorf("a release named like chapters went under %q", got)
	}
}

func TestLayoutNamesDotTorrentsByHash(t *testing.T) {
	l := newLayout(t.TempDir())
	for i, name := range []string{".", "..", ""} {
		h := metainfo.Hash{byte(i + 1)}
		got := l.top(name, h)
		if !safeComponent(got) || (name != "" && got != h.HexString()) {
			t.Errorf("%q went under %q", name, got)
		}
	}
}

func TestPackFilesNeverShareADiskName(t *testing.T) {
	info := &metainfo.Info{Name: "Pack", PieceLength: 4, Files: []metainfo.FileInfo{
		{Path: []string{"Fonts", "ARIAL.TTF"}, Length: 1},
		{Path: []string{"Fonts", "arial.ttf"}, Length: 1},
		{Path: []string{"Ep 01.mkv"}, Length: 1},
	}}
	got := packParts(info)
	second := got[pathKey([]string{"Fonts", "arial.ttf"})]
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		if !slices.Equal(second, []string{"Fonts", "arial (2).ttf"}) {
			t.Errorf("case twin went to %q", second)
		}
	} else if !slices.Equal(second, []string{"Fonts", "arial.ttf"}) {
		t.Errorf("a case-sensitive system renamed %q", second)
	}
	if first := got[pathKey([]string{"Fonts", "ARIAL.TTF"})]; !slices.Equal(first, []string{"Fonts", "ARIAL.TTF"}) {
		t.Errorf("the first file moved to %q", first)
	}
	if ep := got[pathKey([]string{"Ep 01.mkv"})]; !slices.Equal(ep, []string{"Ep 01.mkv"}) {
		t.Errorf("a file with no twin moved to %q", ep)
	}
}

func TestInTorrentKeepsPathsInTheirFolder(t *testing.T) {
	base := t.TempDir()
	own := filepath.Join(base, "Pack")
	for _, c := range []struct {
		top  string
		dir  bool
		path string
		want bool
	}{
		{"Pack", true, filepath.Join(own, "ep1.mkv"), true},
		{"Pack", true, filepath.Join(own, "Extras", "ncop.mkv"), true},
		{"Pack", true, own, false},
		{"Pack", true, filepath.Join(own, "..", "hls", "x"), false},
		{"Pack", true, filepath.Join(own, "..", "Other", "ep1.mkv"), false},
		{"Pack", true, filepath.Join(base, "ep1.mkv"), false},
		{"ep1.mkv", false, filepath.Join(base, "ep1.mkv"), true},
		{"ep1.mkv", false, filepath.Join(base, "ep2.mkv"), false},
		{".", true, filepath.Join(base, "ep1.mkv"), false},
		{"..", false, filepath.Dir(base), false},
	} {
		if got := inTorrent(base, c.top, c.dir, c.path); got != c.want {
			t.Errorf("inTorrent(%q, %v, %q) = %v", c.top, c.dir, c.path, got)
		}
	}
}

func TestLegacyMigrationSkipsDotParts(t *testing.T) {
	for _, c := range []struct {
		best  string
		parts []string
		want  bool
	}{
		{"Re:Zero", []string{"Re:Zero 01.mkv"}, true},
		{"Pack", []string{"..", "..", "Users", "x.mkv"}, false},
		{"Pack", []string{"a\\..\\..\\x.mkv"}, false},
		{"..", []string{"x.mkv"}, false},
		{"Pack", []string{".", "x.mkv"}, false},
		{"Pack", []string{"x."}, runtime.GOOS != "windows"},
	} {
		if got := legacySafe(c.best, true, c.parts); got != c.want {
			t.Errorf("legacySafe(%q, %q) = %v", c.best, c.parts, got)
		}
	}
}

func TestLayoutKeepsCaseOnlyDifferentNamesApart(t *testing.T) {
	l := newLayout(t.TempDir())
	a := l.top("Show Pack", metainfo.Hash{1})
	b := l.top("show pack", metainfo.Hash{2})
	if strings.EqualFold(a, b) {
		t.Fatalf("both under %q", a)
	}
}

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

func TestRebuildKeepsSameNamedReleasesApart(t *testing.T) {
	cache, sessions := t.TempDir(), t.TempDir()
	var hashes []metainfo.Hash
	for i := range 2 {
		info := metainfo.Info{Name: "Show Pack", PieceLength: 16 << 10, Pieces: make([]byte, 20),
			Files: []metainfo.FileInfo{{Path: []string{"Show - 01.mkv"}, Length: int64(100 + i)}}}
		body, err := bencode.Marshal(info)
		if err != nil {
			t.Fatal(err)
		}
		mi := metainfo.MetaInfo{InfoBytes: body}
		hashes = append(hashes, mi.HashInfoBytes())
		if err := writeMetainfo(filepath.Join(sessions, mi.HashInfoBytes().HexString()+".torrent"), mi); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{"Show Pack", suffixed("Show Pack", hashes[1])} {
		os.MkdirAll(filepath.Join(cache, dir), 0o755)
		os.WriteFile(filepath.Join(cache, dir, "Show - 01.mkv"), []byte("x"), 0o644)
	}
	e := &Engine{opts: Options{CacheDir: cache, SessionDir: sessions}}
	for _, r := range e.rebuildSession().Torrents {
		want := ""
		if r.InfoHash == hashes[1].HexString() {
			want = suffixed("Show Pack", hashes[1])
		}
		if r.Top != want {
			t.Errorf("%s rebuilt under %q, want %q", r.InfoHash[:8], r.Top, want)
		}
	}
}

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

func TestSuffixGoesBeforeTheExtension(t *testing.T) {
	l := newLayout(t.TempDir())
	l.top("Show - 01.mkv", metainfo.Hash{1})
	got := l.top("Show - 01.mkv", metainfo.Hash{2, 0xab})
	if !strings.HasSuffix(got, ".mkv") || got == "Show - 01.mkv" {
		t.Fatalf("second copy named %q", got)
	}
	l.top("Show S01 1080p.WEB", metainfo.Hash{3})
	if got := l.top("Show S01 1080p.WEB", metainfo.Hash{4}); strings.HasSuffix(got, ".WEB") {
		t.Errorf("pack folder named %q", got)
	}
}

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
