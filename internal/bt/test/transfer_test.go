package test

import (
	"os"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/anacrolix/log"
	qt "github.com/go-quicktest/qt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"yoru/internal/bt"
	"yoru/internal/bt/internal/testutil"
	"yoru/internal/bt/storage"
)

func TestClientTransferDefault(t *testing.T) {
	testClientTransfer(t, testClientTransferParams{
		LeecherStorage: storage.NewFile,
	})
}

func TestClientTransferDefaultNoMetadata(t *testing.T) {
	testClientTransfer(t, testClientTransferParams{
		LeecherStorage:               storage.NewFile,
		LeecherStartsWithoutMetadata: true,
	})
}

func TestClientTransferRateLimitedUpload(t *testing.T) {
	started := time.Now()
	testClientTransfer(t, testClientTransferParams{
		SeederUploadRateLimiter: rate.NewLimiter(11, 2),
	})
	require.True(t, time.Since(started) > time.Second)
}

func TestClientTransferRateLimitedDownload(t *testing.T) {
	testClientTransfer(t, testClientTransferParams{
		LeecherDownloadRateLimiter: rate.NewLimiter(512, 512),
		ConfigureSeeder: ConfigureClient{
			Config: func(cfg *bt.ClientConfig) {
				cfg.KeepAliveTimeout = time.Minute
			},
		},
	})
}

func TestFileClientTransferVarious(t *testing.T) {
	TestLeecherStorage(t, LeecherStorageTestCase{
		"File", storage.NewFile, 0,
	})
}

func testSeedAfterDownloading(t *testing.T, disableUtp bool) {
	greetingTempDir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(greetingTempDir)

	cfg := bt.TestingConfig(t)
	cfg.Seed = true
	cfg.MaxAllocPeerRequestDataPerConn = 4
	cfg.DataDir = greetingTempDir
	cfg.DisableUTP = disableUtp
	seeder, err := bt.NewClient(cfg)
	require.NoError(t, err)
	defer seeder.Close()
	defer testutil.ExportStatusWriter(seeder, "s", t)()
	seederTorrent, ok, err := seeder.AddTorrentSpec(bt.TorrentSpecFromMetaInfo(mi))
	require.NoError(t, err)
	assert.True(t, ok)
	seederTorrent.VerifyData()

	cfg = bt.TestingConfig(t)
	cfg.Seed = true
	cfg.DataDir = t.TempDir()
	cfg.DisableUTP = disableUtp
	cfg.DisablePEX = true
	cfg.Logger = log.Default.WithContextText("leecher")
	leecher, err := bt.NewClient(cfg)
	require.NoError(t, err)
	defer leecher.Close()
	defer testutil.ExportStatusWriter(leecher, "l", t)()

	cfg = bt.TestingConfig(t)
	cfg.DisableUTP = disableUtp
	cfg.Seed = false
	cfg.DataDir = t.TempDir()
	cfg.MaxAllocPeerRequestDataPerConn = 4
	cfg.Logger = log.Default.WithContextText("leecher-leecher")
	cfg.Debug = true
	leecherLeecher, _ := bt.NewClient(cfg)
	require.NoError(t, err)
	defer leecherLeecher.Close()
	defer testutil.ExportStatusWriter(leecherLeecher, "ll", t)()
	leecherGreeting, ok, err := leecher.AddTorrentSpec(func() (ret *bt.TorrentSpec) {
		ret = bt.TorrentSpecFromMetaInfo(mi)
		ret.ChunkSize = 2
		return
	}())
	require.NoError(t, err)
	assert.True(t, ok)
	llg, ok, err := leecherLeecher.AddTorrentSpec(func() (ret *bt.TorrentSpec) {
		ret = bt.TorrentSpecFromMetaInfo(mi)
		ret.ChunkSize = 3
		return
	}())
	require.NoError(t, err)
	assert.True(t, ok)
	var wg sync.WaitGroup
	{
		r := llg.NewReader()
		llg.VerifyData()
		wg.Go(func() {
			defer r.Close()
			qt.Check(t, qt.IsNil(iotest.TestReader(r, []byte(testutil.GreetingFileContents))))
		})
	}
	go leecherGreeting.AddClientPeer(seeder)
	go leecherGreeting.AddClientPeer(leecherLeecher)
	wg.Go(func() {
		leecherGreeting.DownloadAll()
		leecher.WaitAll()
	})
	wg.Wait()
}

func TestSeedAfterDownloadingDisableUtp(t *testing.T) {
	testSeedAfterDownloading(t, true)
}

func TestSeedAfterDownloadingAllowUtp(t *testing.T) {
	testSeedAfterDownloading(t, false)
}
