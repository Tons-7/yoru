package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"yoru/internal/bt"
	"yoru/internal/bt/bencode"
	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/storage"
)

func singleFileTorrent(t *testing.T, cl *bt.Client, name string, body []byte) *bt.Torrent {
	t.Helper()
	src := filepath.Join(t.TempDir(), "src")
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	info := metainfo.Info{PieceLength: 16 << 10}
	if err := info.BuildFromFilePath(src); err != nil {
		t.Fatal(err)
	}
	info.Name = name
	raw, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: raw}
	cs := storage.NewFile(t.TempDir())
	t.Cleanup(func() { cs.Close() })
	tt, _ := cl.AddTorrentOpt(bt.AddTorrentOpts{InfoHash: mi.HashInfoBytes(), InfoBytes: raw, Storage: cs})
	<-tt.GotInfo()
	return tt
}

func TestLegacyMoveLeavesAnotherTorrentsFile(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("trailing-dot names only collide on Windows")
	}
	cl, err := bt.NewClient(bt.TestingConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	b := singleFileTorrent(t, cl, "X", []byte("episode B"))
	a := singleFileTorrent(t, cl, "X.", []byte("episode A, a different file"))

	cache := t.TempDir()
	l := newLayout(cache)
	l.top(b.Info().BestName(), b.InfoHash())
	l.top(a.Info().BestName(), a.InfoHash())
	old := legacyPath(cache, l.top(a.Info().BestName(), a.InfoHash()), a.Info().BestName(), false, nil)
	if !l.shared(old, a.InfoHash(), []*bt.Torrent{a, b}) {
		t.Fatal("A's legacy path is B's file, yet the move guard calls it free")
	}
	if l.shared(old, a.InfoHash(), []*bt.Torrent{a}) {
		t.Fatal("with B gone the guard still blocks A's move")
	}
}
