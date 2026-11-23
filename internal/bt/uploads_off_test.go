package bt

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"yoru/internal/bt/bencode"
	"yoru/internal/bt/metainfo"
)

func TestSetUploadsOffChokesConnectedPeers(t *testing.T) {
	seedDir := t.TempDir()
	data := make([]byte, 1<<20)
	rand.Read(data)
	if err := os.WriteFile(filepath.Join(seedDir, "big.bin"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	info := metainfo.Info{PieceLength: 32 << 10}
	if err := info.BuildFromFilePath(filepath.Join(seedDir, "big.bin")); err != nil {
		t.Fatal(err)
	}
	infoBytes, _ := bencode.Marshal(info)
	mi := &metainfo.MetaInfo{InfoBytes: infoBytes}

	cfg := TestingConfig(t)
	cfg.Seed = true
	cfg.DataDir = seedDir
	cfg.UploadRateLimiter = rate.NewLimiter(64<<10, 16<<10)
	seeder, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer seeder.Close()
	seederTorrent, _, _ := seeder.AddTorrentSpec(TorrentSpecFromMetaInfo(mi))
	seederTorrent.VerifyData()

	cfg = TestingConfig(t)
	cfg.DataDir = t.TempDir()
	leecher, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer leecher.Close()
	leecherTorrent, _, _ := leecher.AddTorrentSpec(TorrentSpecFromMetaInfo(mi))
	leecherTorrent.DownloadAll()
	leecherTorrent.AddClientPeer(seeder)

	unchoked := func() (open, any bool) {
		seeder.lock()
		defer seeder.unlock()
		for c := range seederTorrent.conns {
			any = true
			open = open || !c.choking
		}
		return
	}
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(10 * time.Second); !ok(); time.Sleep(10 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	waitFor("the seeder to unchoke the leecher", func() bool { open, _ := unchoked(); return open })

	seeder.SetUploads(false, true)
	waitFor("the seeder to choke the leecher once uploads are off", func() bool {
		open, any := unchoked()
		return any && !open
	})
}
