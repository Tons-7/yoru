package test

import (
	"io"
	"os"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2/filecache"
	qt "github.com/go-quicktest/qt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"yoru/internal/bt"
	"yoru/internal/bt/internal/testutil"
	"yoru/internal/bt/storage"
)

type fileCacheClientStorageFactoryParams struct {
	Capacity    int64
	SetCapacity bool
}

func newFileCacheClientStorageFactory(ps fileCacheClientStorageFactoryParams) StorageFactory {
	return func(dataDir string) storage.ClientImplCloser {
		fc, err := filecache.NewCache(dataDir)
		if err != nil {
			panic(err)
		}
		var capFuncPtr storage.TorrentCapacity
		if ps.SetCapacity {
			fc.SetCapacity(ps.Capacity)
			f := func() (cap int64, capped bool) {
				return ps.Capacity, ps.SetCapacity
			}
			capFuncPtr = &f
		}

		return struct {
			storage.ClientImpl
			io.Closer
		}{
			storage.NewResourcePiecesOpts(
				fc.AsResourceProvider(),
				storage.ResourcePiecesOpts{
					Capacity: capFuncPtr,
				}),
			io.NopCloser(nil),
		}
	}
}

func TestClientTransferDefault(t *testing.T) {
	testClientTransfer(t, testClientTransferParams{
		LeecherStorage: newFileCacheClientStorageFactory(fileCacheClientStorageFactoryParams{}),
	})
}

func TestClientTransferDefaultNoMetadata(t *testing.T) {
	testClientTransfer(t, testClientTransferParams{
		LeecherStorage:               newFileCacheClientStorageFactory(fileCacheClientStorageFactoryParams{}),
		LeecherStartsWithoutMetadata: true,
	})
}

func TestClientTransferRateLimitedUpload(t *testing.T) {
	started := time.Now()
	testClientTransfer(t, testClientTransferParams{
		// 13 bytes (the greeting torrent) in 2-byte chunks, so the burst is at least 2: (13-burst)/rate.
		SeederUploadRateLimiter: rate.NewLimiter(11, 2),
	})
	require.True(t, time.Since(started) > time.Second)
}

func TestClientTransferRateLimitedDownload(t *testing.T) {
	testClientTransfer(t, testClientTransferParams{
		LeecherDownloadRateLimiter: rate.NewLimiter(512, 512),
		ConfigureSeeder: ConfigureClient{
			Config: func(cfg *bt.ClientConfig) {
				// If we send too many keep alives, we consume all the leechers available download
				// rate. The default isn't exposed, but a minute is pretty reasonable.
				cfg.KeepAliveTimeout = time.Minute
			},
		},
	})
}

func testClientTransferSmallCache(t *testing.T, setReadahead bool, readahead int64) {
	testClientTransfer(t, testClientTransferParams{
		LeecherStorage: newFileCacheClientStorageFactory(fileCacheClientStorageFactoryParams{
			SetCapacity: true,
			// Under the piece length, no piece could complete to be hashed.
			Capacity: 5,
		}),
		LeecherStorageCapacity: 5,
		SetReadahead:           setReadahead,
		// Not far, or the cache thrashes and drops data we thought we had.
		Readahead: readahead,

		// These tests don't work well with more than 1 connection to the seeder.
		ConfigureLeecher: ConfigureClient{
			Config: func(cfg *bt.ClientConfig) {
				cfg.DropDuplicatePeerIds = true
			},
		},
	})
}

func TestClientTransferSmallCachePieceSizedReadahead(t *testing.T) {
	testClientTransferSmallCache(t, true, 5)
}

func TestClientTransferSmallCacheLargeReadahead(t *testing.T) {
	testClientTransferSmallCache(t, true, 15)
}

func TestClientTransferSmallCacheDefaultReadahead(t *testing.T) {
	testClientTransferSmallCache(t, false, -1)
}

func TestFilecacheClientTransferVarious(t *testing.T) {
	TestLeecherStorage(t, LeecherStorageTestCase{
		"Filecache", newFileCacheClientStorageFactory(fileCacheClientStorageFactoryParams{}), 0,
	})
}

// Check that after completing leeching, a leecher transitions to a seeding
// correctly. Connected in a chain like so: Seeder <-> Leecher <-> LeecherLeecher.
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
	// The leecher-leecher, with bigger chunks, mustn't reach the seeder directly: the leecher must decode them.
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
	// DownloadAll in Leecher while LeecherLeecher reads in order, which can leave LeecherLeecher choked.
	var wg sync.WaitGroup
	{
		// Prioritize a region, and ensure it's been hashed, so we want connections.
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
