package bt

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/iotest"
	"time"

	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2"
	"github.com/go-quicktest/qt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"yoru/internal/bt/dht"

	"yoru/internal/bt/bencode"
	"yoru/internal/bt/internal/testutil"
	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/storage"
)

func TestClientDefault(t *testing.T) {
	cl, err := NewClient(TestingConfig(t))
	require.NoError(t, err)
	require.Empty(t, cl.Close())
}

func TestClientDefaultConfig(t *testing.T) {
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(t.TempDir())
	cfg := NewDefaultClientConfig()
	cfg.ListenHost = LoopbackListenHost
	cl, err := NewClient(cfg)
	require.NoError(t, err)
	require.Empty(t, cl.Close())
}

func TestAddDropTorrent(t *testing.T) {
	cl, err := NewClient(TestingConfig(t))
	require.NoError(t, err)
	defer cl.Close()
	dir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(dir)
	tt, new, err := cl.AddTorrentSpec(TorrentSpecFromMetaInfo(mi))
	require.NoError(t, err)
	assert.True(t, new)
	tt.SetMaxEstablishedConns(0)
	tt.SetMaxEstablishedConns(1)
	tt.Drop()
}

func TestAddTorrentNoSupportedTrackerSchemes(t *testing.T) {
	t.SkipNow()
}

func TestAddTorrentNoUsableURLs(t *testing.T) {
	t.SkipNow()
}

func TestAddPeersToUnknownTorrent(t *testing.T) {
	t.SkipNow()
}

func TestPieceHashSize(t *testing.T) {
	assert.Equal(t, 20, pieceHash.Size())
}

func TestTorrentInitialState(t *testing.T) {
	dir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(dir)
	var cl Client
	cl.init(TestingConfig(t))
	defer cl.Close()
	tor := cl.newTorrent(
		mi.HashInfoBytes(),
		storage.NewFileWithCompletion(t.TempDir(), storage.NewMapPieceCompletion()),
	)
	tor.setChunkSize(2)
	tor.cl.lock()
	err := tor.setInfoBytesLocked(mi.InfoBytes)
	tor.cl.unlock()
	require.NoError(t, err)
	require.Len(t, tor.pieces, 3)
	tor.pendAllChunkSpecs(0)
	tor.cl.lock()
	assert.EqualValues(t, 3, tor.pieceNumPendingChunks(0))
	tor.cl.unlock()
	assert.EqualValues(t, ChunkSpec{Begin: 4, Length: 1}, chunkIndexSpec(2, tor.pieceLength(0), tor.chunkSize))
}

func TestReducedDialTimeout(t *testing.T) {
	cfg := NewDefaultClientConfig()
	for _, _case := range []struct {
		Max             time.Duration
		HalfOpenLimit   int
		PendingPeers    int
		ExpectedReduced time.Duration
	}{
		{cfg.NominalDialTimeout, 40, 0, cfg.NominalDialTimeout},
		{cfg.NominalDialTimeout, 40, 1, cfg.NominalDialTimeout},
		{cfg.NominalDialTimeout, 40, 39, cfg.NominalDialTimeout},
		{cfg.NominalDialTimeout, 40, 40, cfg.NominalDialTimeout / 2},
		{cfg.NominalDialTimeout, 40, 80, cfg.NominalDialTimeout / 3},
		{cfg.NominalDialTimeout, 40, 4000, cfg.NominalDialTimeout / 101},
	} {
		reduced := reducedDialTimeout(cfg.MinDialTimeout, _case.Max, _case.HalfOpenLimit, _case.PendingPeers)
		expected := max(_case.ExpectedReduced, cfg.MinDialTimeout)
		if reduced != expected {
			t.Fatalf("expected %s, got %s", _case.ExpectedReduced, reduced)
		}
	}
}

func TestAddDropManyTorrents(t *testing.T) {
	cl, err := NewClient(TestingConfig(t))
	require.NoError(t, err)
	defer cl.Close()
	for i := range 1000 {
		var opts AddTorrentOpts
		binary.PutVarint(opts.InfoHash[:], int64(i+1))
		tt, new := cl.AddTorrentOpt(opts)
		assert.True(t, new)
		defer tt.Drop()
	}
}

func TestMergingTrackersByAddingSpecs(t *testing.T) {
	cl, err := NewClient(TestingConfig(t))
	require.NoError(t, err)
	defer cl.Close()
	spec := TorrentSpec{}
	rand.Read(spec.InfoHash[:])
	T, new, _ := cl.AddTorrentSpec(&spec)
	if !new {
		t.FailNow()
	}
	spec.Trackers = [][]string{{"http://a"}, {"udp://b"}}
	_, new, _ = cl.AddTorrentSpec(&spec)
	assert.False(t, new)
	assert.EqualValues(t, [][]string{{"http://a"}, {"udp://b"}}, T.announceList)
	assert.EqualValues(t, 0, len(T.trackerAnnouncers))
}

func TestCompletedPieceWrongSize(t *testing.T) {
	cfg := TestingConfig(t)
	cfg.DefaultStorage = badStorage{}
	cl, err := NewClient(cfg)
	require.NoError(t, err)
	defer cl.Close()
	info := metainfo.Info{
		PieceLength: 15,
		Pieces:      make([]byte, 20),
		Files: []metainfo.FileInfo{
			{Path: []string{"greeting"}, Length: 13},
		},
	}
	b, err := bencode.Marshal(info)
	require.NoError(t, err)
	tt, new := cl.AddTorrentOpt(AddTorrentOpts{
		InfoBytes: b,
		InfoHash:  metainfo.HashBytes(b),
	})
	defer tt.Drop()
	assert.True(t, new)
	r := tt.NewReader()
	defer r.Close()
	r.SetContext(t.Context())
	qt.Check(t, qt.IsNil(iotest.TestReader(r, []byte(testutil.GreetingFileContents))))
}

func BenchmarkAddLargeTorrent(b *testing.B) {
	cfg := TestingConfig(b)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	cl, err := NewClient(cfg)
	require.NoError(b, err)
	defer cl.Close()
	b.ReportAllocs()
	for i := 0; i < b.N; i += 1 {
		t, err := cl.AddTorrentFromFile("testdata/bootstrap.dat.torrent")
		if err != nil {
			b.Fatal(err)
		}
		t.Drop()
	}
}

func TestResponsive(t *testing.T) {
	seederDataDir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(seederDataDir)
	cfg := TestingConfig(t)
	cfg.Seed = true
	cfg.DataDir = seederDataDir
	seeder, err := NewClient(cfg)
	require.Nil(t, err)
	defer seeder.Close()
	seederTorrent, _, _ := seeder.AddTorrentSpec(TorrentSpecFromMetaInfo(mi))
	seederTorrent.VerifyData()
	leecherDataDir := t.TempDir()
	cfg = TestingConfig(t)
	cfg.DataDir = leecherDataDir
	leecher, err := NewClient(cfg)
	require.Nil(t, err)
	defer leecher.Close()
	leecherTorrent, _, _ := leecher.AddTorrentSpec(func() (ret *TorrentSpec) {
		ret = TorrentSpecFromMetaInfo(mi)
		ret.ChunkSize = 2
		return
	}())
	leecherTorrent.AddClientPeer(seeder)
	reader := leecherTorrent.NewReader()
	defer reader.Close()
	reader.SetReadahead(0)
	reader.SetResponsive()
	b := make([]byte, 2)
	_, err = reader.Seek(3, io.SeekStart)
	require.NoError(t, err)
	_, err = io.ReadFull(reader, b)
	assert.Nil(t, err)
	assert.EqualValues(t, "lo", string(b))
	_, err = reader.Seek(11, io.SeekStart)
	require.NoError(t, err)
	n, err := io.ReadFull(reader, b)
	assert.Nil(t, err)
	assert.EqualValues(t, 2, n)
	assert.EqualValues(t, "d\n", string(b))
}

func TestResponsiveTcpOnly(t *testing.T) {
	seederDataDir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(seederDataDir)
	cfg := TestingConfig(t)
	cfg.DisableUTP = true
	cfg.Seed = true
	cfg.DataDir = seederDataDir
	seeder, err := NewClient(cfg)
	require.Nil(t, err)
	defer seeder.Close()
	seederTorrent, _, _ := seeder.AddTorrentSpec(TorrentSpecFromMetaInfo(mi))
	seederTorrent.VerifyData()
	leecherDataDir := t.TempDir()
	cfg = TestingConfig(t)
	cfg.DataDir = leecherDataDir
	leecher, err := NewClient(cfg)
	require.Nil(t, err)
	defer leecher.Close()
	leecherTorrent, _, _ := leecher.AddTorrentSpec(func() (ret *TorrentSpec) {
		ret = TorrentSpecFromMetaInfo(mi)
		ret.ChunkSize = 2
		return
	}())
	leecherTorrent.AddClientPeer(seeder)
	reader := leecherTorrent.NewReader()
	defer reader.Close()
	reader.SetReadahead(0)
	reader.SetResponsive()
	b := make([]byte, 2)
	_, err = reader.Seek(3, io.SeekStart)
	require.NoError(t, err)
	_, err = io.ReadFull(reader, b)
	assert.Nil(t, err)
	assert.EqualValues(t, "lo", string(b))
	_, err = reader.Seek(11, io.SeekStart)
	require.NoError(t, err)
	n, err := io.ReadFull(reader, b)
	assert.Nil(t, err)
	assert.EqualValues(t, 2, n)
	assert.EqualValues(t, "d\n", string(b))
}

func TestTorrentDroppedDuringResponsiveRead(t *testing.T) {
	seederDataDir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(seederDataDir)
	cfg := TestingConfig(t)
	cfg.Seed = true
	cfg.DataDir = seederDataDir
	seeder, err := NewClient(cfg)
	require.Nil(t, err)
	defer seeder.Close()
	seederTorrent, _, _ := seeder.AddTorrentSpec(TorrentSpecFromMetaInfo(mi))
	seederTorrent.VerifyData()
	leecherDataDir := t.TempDir()
	cfg = TestingConfig(t)
	cfg.DataDir = leecherDataDir
	leecher, err := NewClient(cfg)
	require.Nil(t, err)
	defer leecher.Close()
	leecherTorrent, _, _ := leecher.AddTorrentSpec(func() (ret *TorrentSpec) {
		ret = TorrentSpecFromMetaInfo(mi)
		ret.ChunkSize = 2
		return
	}())
	leecherTorrent.AddClientPeer(seeder)
	rdr := leecherTorrent.NewReader()
	t.Cleanup(func() { rdr.Close() })
	rdr.SetReadahead(0)
	rdr.SetResponsive()
	b := make([]byte, 2)
	_, err = rdr.Seek(3, io.SeekStart)
	require.NoError(t, err)
	_, err = io.ReadFull(rdr, b)
	assert.Nil(t, err)
	assert.EqualValues(t, "lo", string(b))
	_, err = rdr.Seek(11, io.SeekStart)
	require.NoError(t, err)
	leecherTorrent.Drop()
	n, err := rdr.Read(b)
	qt.Assert(t, qt.Equals(err, errTorrentClosed))
	assert.EqualValues(t, 0, n)
}

func TestAddTorrentSpecMerging(t *testing.T) {
	cl, err := NewClient(TestingConfig(t))
	require.NoError(t, err)
	defer cl.Close()
	dir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(dir)
	tt, new := cl.AddTorrentOpt(AddTorrentOpts{
		InfoHash: mi.HashInfoBytes(),
	})
	require.True(t, new)
	require.Nil(t, tt.Info())
	_, new, err = cl.AddTorrentSpec(TorrentSpecFromMetaInfo(mi))
	require.NoError(t, err)
	require.False(t, new)
	require.NotNil(t, tt.Info())
}

func TestTorrentDroppedBeforeGotInfo(t *testing.T) {
	dir, mi := testutil.GreetingTestTorrent()
	os.RemoveAll(dir)
	cl, _ := NewClient(TestingConfig(t))
	defer cl.Close()
	tt, _ := cl.AddTorrentOpt(AddTorrentOpts{
		InfoHash: mi.HashInfoBytes(),
	})
	tt.Drop()
	assert.EqualValues(t, 0, len(cl.Torrents()))
	select {
	case <-tt.GotInfo():
		t.FailNow()
	default:
	}
}

func TestAddMetainfoWithNodes(t *testing.T) {
	cfg := TestingConfig(t)
	cfg.NoDHT = false
	cfg.DhtStartingNodes = func(string) dht.StartingNodesGetter { return func() ([]dht.Addr, error) { return nil, nil } }
	cl, err := NewClient(cfg)
	require.NoError(t, err)
	defer cl.Close()
	sum := func() (ret int64) {
		cl.eachDhtServer(func(s DhtServer) {
			ret += s.Stats().(dht.ServerStats).OutboundQueriesAttempted
		})
		return
	}
	assert.EqualValues(t, 0, sum())
	tt, err := cl.AddTorrentFromFile("metainfo/testdata/issue_65a.torrent")
	require.NoError(t, err)
	assert.Len(t, tt.announceList, 5)
	for sum() != int64(6*len(cl.dhtServers)) {
		time.Sleep(time.Millisecond)
	}
}

type testDownloadCancelParams struct {
	Cancel bool
}

func testDownloadCancel(t *testing.T, ps testDownloadCancelParams) {
	greetingTempDir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(greetingTempDir)
	cfg := TestingConfig(t)
	cfg.Seed = true
	cfg.DataDir = greetingTempDir
	seeder, err := NewClient(cfg)
	require.NoError(t, err)
	defer seeder.Close()
	defer testutil.ExportStatusWriter(seeder, "s", t)()
	seederTorrent, _, _ := seeder.AddTorrentSpec(TorrentSpecFromMetaInfo(mi))
	seederTorrent.VerifyData()
	leecherDataDir := t.TempDir()
	leecherStorage := storage.NewFile(leecherDataDir)
	defer leecherStorage.Close()
	cfg.DefaultStorage = leecherStorage
	cfg.DataDir = leecherDataDir
	leecher, err := NewClient(cfg)
	require.NoError(t, err)
	defer leecher.Close()
	defer testutil.ExportStatusWriter(leecher, "l", t)()
	leecherGreeting, new, err := leecher.AddTorrentSpec(func() (ret *TorrentSpec) {
		ret = TorrentSpecFromMetaInfo(mi)
		ret.ChunkSize = 2
		return
	}())
	require.NoError(t, err)
	assert.True(t, new)
	psc := leecherGreeting.SubscribePieceStateChanges()
	defer psc.Close()

	leecherGreeting.cl.lock()
	leecherGreeting.downloadPiecesLocked(0, leecherGreeting.numPieces())
	if ps.Cancel {
		leecherGreeting.cancelPiecesLocked(0, leecherGreeting.NumPieces(), "")
	}
	leecherGreeting.cl.unlock()
	done := make(chan struct{})
	defer close(done)
	go leecherGreeting.AddClientPeer(seeder)
	completes := make(map[int]bool, 3)
	expected := func() map[int]bool {
		if ps.Cancel {
			return map[int]bool{0: false, 1: false, 2: false}
		} else {
			return map[int]bool{0: true, 1: true, 2: true}
		}
	}()
	for !reflect.DeepEqual(completes, expected) {
		v := <-psc.Values
		completes[v.Index] = v.Complete
	}
}

func TestTorrentDownloadAll(t *testing.T) {
	testDownloadCancel(t, testDownloadCancelParams{})
}

func TestTorrentDownloadAllThenCancel(t *testing.T) {
	testDownloadCancel(t, testDownloadCancelParams{
		Cancel: true,
	})
}

func TestPeerInvalidHave(t *testing.T) {
	cfg := TestingConfig(t)
	cfg.DropMutuallyCompletePeers = false
	cl, err := NewClient(cfg)
	require.NoError(t, err)
	defer cl.Close()
	info := metainfo.Info{
		PieceLength: 1,
		Pieces:      make([]byte, 20),
		Files:       []metainfo.FileInfo{{Length: 1}},
	}
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	tt, _new := cl.AddTorrentOpt(AddTorrentOpts{
		InfoBytes: infoBytes,
		InfoHash:  metainfo.HashBytes(infoBytes),
		Storage:   badStorage{},
	})
	assert.True(t, _new)
	defer tt.Drop()
	cn := &PeerConn{Peer: Peer{
		t:         tt,
		callbacks: &cfg.Callbacks,
	}}
	tt.conns[cn] = struct{}{}
	cn.legacyPeerImpl = cn
	cl.lock()
	defer cl.unlock()
	assert.NoError(t, cn.peerSentHave(0))
	assert.Error(t, cn.peerSentHave(1))
}

func TestPieceCompletedInStorageButNotClient(t *testing.T) {
	greetingTempDir, greetingMetainfo := testutil.GreetingTestTorrent()
	defer os.RemoveAll(greetingTempDir)
	cfg := TestingConfig(t)
	cfg.DataDir = greetingTempDir
	seeder, err := NewClient(TestingConfig(t))
	require.NoError(t, err)
	defer seeder.Close()
	_, new := seeder.AddTorrentOpt(AddTorrentOpts{
		InfoBytes: greetingMetainfo.InfoBytes,
		InfoHash:  greetingMetainfo.HashInfoBytes(),
	})
	qt.Check(t, qt.IsNil(err))
	qt.Check(t, qt.IsTrue(new))
}

func TestClientDynamicListenPortAllProtocols(t *testing.T) {
	cl, err := NewClient(TestingConfig(t))
	require.NoError(t, err)
	defer cl.Close()
	port := cl.LocalPort()
	assert.NotEqual(t, 0, port)
	cl.eachListener(func(s Listener) bool {
		assert.Equal(t, port, missinggo.AddrPort(s.Addr()))
		return true
	})
}

func TestClientDynamicListenTCPOnly(t *testing.T) {
	cfg := TestingConfig(t)
	cfg.DisableUTP = true
	cfg.DisableTCP = false
	cl, err := NewClient(cfg)
	require.NoError(t, err)
	defer cl.Close()
	assert.NotEqual(t, 0, cl.LocalPort())
}

func TestClientDynamicListenUTPOnly(t *testing.T) {
	cfg := TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = false
	cl, err := NewClient(cfg)
	require.NoError(t, err)
	defer cl.Close()
	assert.NotEqual(t, 0, cl.LocalPort())
}

func totalConns(tts []*Torrent) (ret int) {
	for _, tt := range tts {
		tt.cl.lock()
		ret += len(tt.conns)
		tt.cl.unlock()
	}
	return
}

func TestSetMaxEstablishedConn(t *testing.T) {
	var tts []*Torrent
	ih := testutil.GreetingMetaInfo().HashInfoBytes()
	cfg := TestingConfig(t)
	cfg.DisableAcceptRateLimiting = true
	cfg.DropDuplicatePeerIds = true
	for i := 0; i < 3; i += 1 {
		cl, err := NewClient(cfg)
		require.NoError(t, err)
		defer cl.Close()
		tt, _ := cl.AddTorrentInfoHash(ih)
		tt.SetMaxEstablishedConns(2)
		defer testutil.ExportStatusWriter(cl, fmt.Sprintf("%d", i), t)()
		tts = append(tts, tt)
	}
	addPeers := func() {
		for _, tt := range tts {
			for _, _tt := range tts {
				tt.AddClientPeer(_tt.cl)
			}
		}
	}
	waitTotalConns := func(num int) {
		for totalConns(tts) != num {
			addPeers()
			time.Sleep(time.Millisecond)
		}
	}
	addPeers()
	waitTotalConns(6)
	tts[0].SetMaxEstablishedConns(1)
	waitTotalConns(4)
	tts[0].SetMaxEstablishedConns(0)
	waitTotalConns(2)
	tts[0].SetMaxEstablishedConns(1)
	addPeers()
	waitTotalConns(4)
	tts[0].SetMaxEstablishedConns(2)
	addPeers()
	waitTotalConns(6)
}

func makeMagnet(t *testing.T, cl *Client, dir, name string) string {
	os.MkdirAll(dir, 0o770)
	file, err := os.Create(filepath.Join(dir, name))
	require.NoError(t, err)
	file.Write([]byte(name))
	file.Close()
	mi := metainfo.MetaInfo{}
	mi.SetDefaults()
	info := metainfo.Info{PieceLength: 256 * 1024}
	err = info.BuildFromFilePath(filepath.Join(dir, name))
	require.NoError(t, err)
	mi.InfoBytes, err = bencode.Marshal(info)
	require.NoError(t, err)
	magnet := mi.Magnet(nil, &info).String()
	tr, err := cl.AddTorrent(&mi)
	require.NoError(t, err)
	require.True(t, tr.Seeding())
	tr.VerifyData()
	return magnet
}

func TestMultipleTorrentsWithEncryption(t *testing.T) {
	testSeederLeecherPair(
		t,
		func(cfg *ClientConfig) {
			cfg.HeaderObfuscationPolicy.Preferred = true
			cfg.HeaderObfuscationPolicy.RequirePreferred = true
		},
		func(cfg *ClientConfig) {
			cfg.HeaderObfuscationPolicy.RequirePreferred = false
		},
	)
}

func testSeederLeecherPair(t *testing.T, seeder, leecher func(*ClientConfig)) {
	cfg := TestingConfig(t)
	cfg.Seed = true
	cfg.DataDir = filepath.Join(cfg.DataDir, "server")
	os.Mkdir(cfg.DataDir, 0o755)
	seeder(cfg)
	server, err := NewClient(cfg)
	require.NoError(t, err)
	defer server.Close()
	defer testutil.ExportStatusWriter(server, "s", t)()
	magnet1 := makeMagnet(t, server, cfg.DataDir, "test1")
	makeMagnet(t, server, cfg.DataDir, "test2")
	for i := range 100 {
		makeMagnet(t, server, cfg.DataDir, fmt.Sprintf("test%d", i+3))
	}
	cfg = TestingConfig(t)
	cfg.DataDir = filepath.Join(cfg.DataDir, "client")
	leecher(cfg)
	client, err := NewClient(cfg)
	require.NoError(t, err)
	defer client.Close()
	defer testutil.ExportStatusWriter(client, "c", t)()
	tr, err := client.AddMagnet(magnet1)
	require.NoError(t, err)
	tr.AddClientPeer(server)
	<-tr.GotInfo()
	tr.DownloadAll()
	client.WaitAll()
}

func TestObfuscatedHeaderFallbackSeederDisallowsLeecherPrefers(t *testing.T) {
	testSeederLeecherPair(
		t,
		func(cfg *ClientConfig) {
			cfg.HeaderObfuscationPolicy.Preferred = false
			cfg.HeaderObfuscationPolicy.RequirePreferred = true
		},
		func(cfg *ClientConfig) {
			cfg.HeaderObfuscationPolicy.Preferred = true
			cfg.HeaderObfuscationPolicy.RequirePreferred = false
		},
	)
}

func TestObfuscatedHeaderFallbackSeederRequiresLeecherPrefersNot(t *testing.T) {
	testSeederLeecherPair(
		t,
		func(cfg *ClientConfig) {
			cfg.HeaderObfuscationPolicy.Preferred = true
			cfg.HeaderObfuscationPolicy.RequirePreferred = true
		},
		func(cfg *ClientConfig) {
			cfg.HeaderObfuscationPolicy.Preferred = false
			cfg.HeaderObfuscationPolicy.RequirePreferred = false
		},
	)
}

func TestClientAddressInUse(t *testing.T) {
	s, _ := NewUtpSocket("udp", "localhost:50007", log.Default)
	if s != nil {
		defer s.Close()
	}
	cfg := TestingConfig(t).SetListenAddr("localhost:50007")
	cfg.DisableUTP = false
	cl, err := NewClient(cfg)
	if err == nil {
		assert.Nil(t, cl.Close())
	}
	require.Error(t, err)
	require.Nil(t, cl)
}

func TestClientHasDhtServersWhenUtpDisabled(t *testing.T) {
	cc := TestingConfig(t)
	cc.DisableUTP = true
	cc.NoDHT = false
	cl, err := NewClient(cc)
	require.NoError(t, err)
	defer cl.Close()
	assert.NotEmpty(t, cl.DhtServers())
}

func TestClientDisabledImplicitNetworksButDhtEnabled(t *testing.T) {
	cfg := TestingConfig(t)
	cfg.DisableTCP = true
	cfg.DisableUTP = true
	cfg.NoDHT = false
	cl, err := NewClient(cfg)
	require.NoError(t, err)
	defer cl.Close()
	assert.Empty(t, cl.listeners)
	assert.NotEmpty(t, cl.DhtServers())
}

func TestBadPeerIpPort(t *testing.T) {
	for _, tc := range []struct {
		title      string
		ip         net.IP
		port       int
		expectedOk bool
		setup      func(*Client)
	}{
		{"empty both", nil, 0, true, func(*Client) {}},
		{"empty/nil ip", nil, 6666, true, func(*Client) {}},
		{
			"empty port",
			net.ParseIP("127.0.0.1/32"),
			0, true,
			func(*Client) {},
		},
		{
			"in doppleganger addresses",
			net.ParseIP("127.0.0.1/32"),
			2322,
			true,
			func(cl *Client) {
				cl.dopplegangerAddrs["10.0.0.1:2322"] = struct{}{}
			},
		},
		{
			"in bad peer IPs",
			net.ParseIP("10.0.0.1"),
			2322,
			true,
			func(cl *Client) {
				cl.banPeerIP(net.ParseIP("10.0.0.1"))
			},
		},
		{
			"banned as 4 bytes, offered as 16",
			net.ParseIP("10.0.0.1"),
			2322,
			true,
			func(cl *Client) {
				cl.banPeerIP(netip.MustParseAddr("10.0.0.1").AsSlice())
			},
		},
		{
			"banned as 16 bytes, offered as 4",
			net.IPv4(10, 0, 0, 1).To4(),
			2322,
			true,
			func(cl *Client) {
				cl.banPeerIP(net.ParseIP("10.0.0.1"))
			},
		},
		{
			"ban expired",
			net.ParseIP("10.0.0.1"),
			2322,
			false,
			func(cl *Client) {
				cl.badPeerIPs = map[netip.Addr]time.Time{netip.MustParseAddr("10.0.0.1"): time.Now().Add(-time.Second)}
			},
		},
		{
			"good",
			net.ParseIP("10.0.0.1"),
			2322,
			false,
			func(cl *Client) {},
		},
	} {
		t.Run(tc.title, func(t *testing.T) {
			cfg := TestingConfig(t)
			cfg.DisableTCP = true
			cfg.DisableUTP = true
			cfg.NoDHT = false
			cl, err := NewClient(cfg)
			require.NoError(t, err)
			defer cl.Close()

			tc.setup(cl)
			require.Equal(t, tc.expectedOk, cl.badPeerIPPort(tc.ip, tc.port))
		})
	}
}

func TestClientConfigSetHandlerNotIgnored(t *testing.T) {
	cfg := TestingConfig(t)
	cfg.Logger.SetHandlers(log.DiscardHandler)
	cl, err := NewClient(cfg)
	qt.Assert(t, qt.IsNil(err))
	defer cl.Close()
	qt.Assert(t, qt.HasLen(cl.logger.Handlers, 1))
	h := cl.logger.Handlers[0].(log.StreamHandler)
	qt.Check(t, qt.Equals(h.W, io.Discard))
}

func TestDroppedTorrentsNotReturned(t *testing.T) {
	cl := newTestingClient(t)
	tt, _ := cl.AddTorrentOpt(testingAddTorrentOpts)
	tt1, ok := cl.Torrent(tt.InfoHash())
	qt.Check(t, qt.IsTrue(ok))
	qt.Check(t, qt.Equals(tt1, tt))
	qt.Check(t, qt.SliceContains(cl.Torrents(), tt))
	tt.Drop()
	tt1, ok = cl.Torrent(tt.InfoHash())
	qt.Check(t, qt.IsFalse(ok))
	qt.Check(t, qt.HasLen(cl.Torrents(), 0))
}
