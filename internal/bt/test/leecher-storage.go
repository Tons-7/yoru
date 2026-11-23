package test

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"testing"
	"testing/iotest"

	"github.com/anacrolix/missinggo/v2/bitmap"
	qt "github.com/go-quicktest/qt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"yoru/internal/bt"
	"yoru/internal/bt/internal/testutil"
	"yoru/internal/bt/storage"
)

type LeecherStorageTestCase struct {
	Name       string
	Factory    StorageFactory
	GoMaxProcs int
}

type StorageFactory func(string) storage.ClientImplCloser

func TestLeecherStorage(t *testing.T, ls LeecherStorageTestCase) {
	for _, ss := range []struct {
		name string
		f    StorageFactory
	}{
		{"File", storage.NewFile},
	} {
		t.Run(fmt.Sprintf("%sSeederStorage", ss.name), func(t *testing.T) {
			for _, responsive := range []bool{false, true} {
				t.Run(fmt.Sprintf("Responsive=%v", responsive), func(t *testing.T) {
					t.Run("NoReadahead", func(t *testing.T) {
						testClientTransfer(t, testClientTransferParams{
							Responsive:     responsive,
							SeederStorage:  ss.f,
							LeecherStorage: ls.Factory,
							GOMAXPROCS:     ls.GoMaxProcs,
						})
					})
					for _, readahead := range []int64{-1, 0, 1, 2, 9, 20} {
						t.Run(fmt.Sprintf("readahead=%v", readahead), func(t *testing.T) {
							testClientTransfer(t, testClientTransferParams{
								SeederStorage:  ss.f,
								Responsive:     responsive,
								SetReadahead:   true,
								Readahead:      readahead,
								LeecherStorage: ls.Factory,
								GOMAXPROCS:     ls.GoMaxProcs,
							})
						})
					}
				})
			}
		})
	}
}

type ConfigureClient struct {
	Config func(cfg *bt.ClientConfig)
	Client func(cl *bt.Client)
}

type testClientTransferParams struct {
	Responsive                 bool
	Readahead                  int64
	SetReadahead               bool
	LeecherStorage             func(string) storage.ClientImplCloser
	LeecherStorageCapacity     int64
	SeederStorage              func(string) storage.ClientImplCloser
	SeederUploadRateLimiter    *rate.Limiter
	LeecherDownloadRateLimiter *rate.Limiter
	ConfigureSeeder            ConfigureClient
	ConfigureLeecher           ConfigureClient
	GOMAXPROCS                 int

	LeecherStartsWithoutMetadata bool
}

func testClientTransfer(t *testing.T, ps testClientTransferParams) {
	prevGOMAXPROCS := runtime.GOMAXPROCS(ps.GOMAXPROCS)
	newGOMAXPROCS := prevGOMAXPROCS
	if ps.GOMAXPROCS > 0 {
		newGOMAXPROCS = ps.GOMAXPROCS
	}
	defer func() {
		qt.Check(t, qt.ContentEquals(runtime.GOMAXPROCS(prevGOMAXPROCS), newGOMAXPROCS))
	}()

	greetingTempDir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(greetingTempDir)
	cfg := bt.TestingConfig(t)
	cfg.Seed = true
	cfg.MaxAllocPeerRequestDataPerConn = 4
	cfg.DropMutuallyCompletePeers = false
	if ps.SeederUploadRateLimiter != nil {
		cfg.UploadRateLimiter = ps.SeederUploadRateLimiter
	}
	if ps.SeederStorage != nil {
		storage := ps.SeederStorage(greetingTempDir)
		defer storage.Close()
		cfg.DefaultStorage = storage
	} else {
		cfg.DataDir = greetingTempDir
	}
	if ps.ConfigureSeeder.Config != nil {
		ps.ConfigureSeeder.Config(cfg)
	}
	seeder, err := bt.NewClient(cfg)
	require.NoError(t, err)
	if ps.ConfigureSeeder.Client != nil {
		ps.ConfigureSeeder.Client(seeder)
	}
	defer testutil.ExportStatusWriter(seeder, "s", t)()
	seederTorrent, _, _ := seeder.AddTorrentSpec(bt.TorrentSpecFromMetaInfo(mi))
	defer seederTorrent.Stats()
	defer seeder.Close()
	<-seederTorrent.Complete().On()
	leecherDataDir := t.TempDir()
	cfg = bt.TestingConfig(t)
	cfg.DropMutuallyCompletePeers = false
	if ps.LeecherStorage == nil {
		cfg.DataDir = leecherDataDir
	} else {
		storage := ps.LeecherStorage(leecherDataDir)
		defer storage.Close()
		cfg.DefaultStorage = storage
	}
	if ps.LeecherDownloadRateLimiter != nil {
		cfg.DownloadRateLimiter = ps.LeecherDownloadRateLimiter
	}
	cfg.Seed = false
	if ps.ConfigureLeecher.Config != nil {
		ps.ConfigureLeecher.Config(cfg)
	}
	leecher, err := bt.NewClient(cfg)
	require.NoError(t, err)
	defer leecher.Close()
	if ps.ConfigureLeecher.Client != nil {
		ps.ConfigureLeecher.Client(leecher)
	}
	defer testutil.ExportStatusWriter(leecher, "l", t)()
	leecherTorrent, new, err := leecher.AddTorrentSpec(func() (ret *bt.TorrentSpec) {
		ret = bt.TorrentSpecFromMetaInfo(mi)
		ret.ChunkSize = 2
		if ps.LeecherStartsWithoutMetadata {
			ret.InfoBytes = nil
		}
		return
	}())
	require.NoError(t, err)
	assert.False(t, leecherTorrent.Complete().Bool())
	assert.True(t, new)

	added := leecherTorrent.AddClientPeer(seeder)
	assert.False(t, leecherTorrent.Seeding())
	if !ps.LeecherStartsWithoutMetadata {
		assert.EqualValues(t, added, leecherTorrent.Stats().PendingPeers)
	}
	if ps.LeecherStartsWithoutMetadata {
		<-leecherTorrent.GotInfo()
	}
	r := leecherTorrent.NewReader()
	defer r.Close()
	go leecherTorrent.SetInfoBytes(mi.InfoBytes)
	if ps.Responsive {
		r.SetResponsive()
	}
	if ps.SetReadahead {
		r.SetReadahead(ps.Readahead)
	}
	assertReadAllGreeting(t, r)
	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)
	canComplete := ps.LeecherStorageCapacity == 0 || ps.LeecherStorageCapacity >= info.TotalLength()
	if !canComplete {
		go leecherTorrent.VerifyData()
	}
	if canComplete {
		<-leecherTorrent.Complete().On()
	} else {
		<-leecherTorrent.Complete().Off()
	}
	assert.NotEmpty(t, seederTorrent.PeerConns())
	leecherPeerConns := leecherTorrent.PeerConns()
	if cfg.DropMutuallyCompletePeers {
	} else {
		assert.NotEmpty(t, leecherPeerConns)
	}
	foundSeeder := false
	for _, pc := range leecherPeerConns {
		completed := pc.PeerPieces().GetCardinality()
		t.Logf("peer conn %v has %v completed pieces", pc, completed)
		if completed == bitmap.BitRange(leecherTorrent.Info().NumPieces()) {
			foundSeeder = true
		}
	}
	if !foundSeeder {
		t.Errorf("didn't find seeder amongst leecher peer conns")
	}

	seederStats := seederTorrent.Stats()
	assert.True(t, 13 <= seederStats.BytesWrittenData.Int64())
	assert.True(t, 8 <= seederStats.ChunksWritten.Int64())

	leecherStats := leecherTorrent.Stats()
	assert.True(t, 13 <= leecherStats.BytesReadData.Int64())
	assert.True(t, 8 <= leecherStats.ChunksRead.Int64())

	assertReadAllGreeting(t, r)
}

func assertReadAllGreeting(t *testing.T, r io.ReadSeeker) {
	pos, err := r.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	assert.EqualValues(t, 0, pos)
	qt.Check(t, qt.IsNil(iotest.TestReader(r, []byte(testutil.GreetingFileContents))))
}
