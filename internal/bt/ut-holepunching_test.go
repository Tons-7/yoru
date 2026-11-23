package bt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
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

	"yoru/internal/bt/internal/testutil"
)

func TestHolepunchConnect(t *testing.T) {
	greetingTempDir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(greetingTempDir)

	cfg := TestingConfig(t)
	cfg.Seed = true
	cfg.MaxAllocPeerRequestDataPerConn = 4
	cfg.DataDir = greetingTempDir
	cfg.DisablePEX = true
	cfg.Debug = true
	cfg.AcceptPeerConnections = false
	cfg.DisableTCP = true
	cfg.DialRateLimiter = rate.NewLimiter(0, 1)
	cfg.Logger = cfg.Logger.WithContextText("seeder")
	seeder, err := NewClient(cfg)
	require.NoError(t, err)
	defer seeder.Close()
	defer testutil.ExportStatusWriter(seeder, "s", t)()
	seederTorrent, ok, err := seeder.AddTorrentSpec(TorrentSpecFromMetaInfo(mi))
	require.NoError(t, err)
	assert.True(t, ok)
	seederTorrent.VerifyData()

	cfg = TestingConfig(t)
	cfg.Seed = true
	cfg.DataDir = t.TempDir()
	cfg.AlwaysWantConns = true
	cfg.Logger = cfg.Logger.WithContextText("leecher")
	cfg.Debug = true
	leecher, err := NewClient(cfg)
	require.NoError(t, err)
	defer leecher.Close()
	defer testutil.ExportStatusWriter(leecher, "l", t)()

	cfg = TestingConfig(t)
	cfg.Seed = false
	cfg.DataDir = t.TempDir()
	cfg.MaxAllocPeerRequestDataPerConn = 4
	cfg.Debug = true
	cfg.NominalDialTimeout = time.Second
	cfg.Logger = cfg.Logger.WithContextText("leecher-leecher")
	leecherLeecher, _ := NewClient(cfg)
	require.NoError(t, err)
	defer leecherLeecher.Close()
	defer testutil.ExportStatusWriter(leecherLeecher, "ll", t)()
	leecherGreeting, ok, err := leecher.AddTorrentSpec(func() (ret *TorrentSpec) {
		ret = TorrentSpecFromMetaInfo(mi)
		ret.ChunkSize = 2
		return
	}())
	_ = leecherGreeting
	require.NoError(t, err)
	assert.True(t, ok)
	llg, ok, err := leecherLeecher.AddTorrentSpec(func() (ret *TorrentSpec) {
		ret = TorrentSpecFromMetaInfo(mi)
		ret.ChunkSize = 3
		return
	}())
	require.NoError(t, err)
	assert.True(t, ok)

	var wg sync.WaitGroup
	wg.Go(func() {
		r := llg.NewReader()
		defer r.Close()
		qt.Check(t, qt.IsNil(iotest.TestReader(r, []byte(testutil.GreetingFileContents))))
	})
	go seederTorrent.AddClientPeer(leecher)
	waitForConns(seederTorrent)
	go llg.AddClientPeer(leecher)
	waitForConns(llg)
	time.Sleep(time.Second)
	llg.cl.lock()
	targetAddr := seeder.ListenAddrs()[0]
	log.Printf("trying to initiate to %v", targetAddr)
	initiateConn(outgoingConnOpts{
		peerInfo: PeerInfo{
			Addr: targetAddr,
		},
		t:                       llg,
		requireRendezvous:       true,
		skipHolepunchRendezvous: false,
		HeaderObfuscationPolicy: llg.cl.config.HeaderObfuscationPolicy,
	}, true)
	llg.cl.unlock()
	wg.Wait()

	qt.Check(t, qt.Not(qt.HasLen(seeder.dialedSuccessfullyAfterHolepunchConnect, 0)))
	qt.Check(t, qt.Not(qt.HasLen(leecherLeecher.probablyOnlyConnectedDueToHolepunch, 0)))

	llClientStats := leecherLeecher.Stats()
	qt.Check(t, qt.Not(qt.Equals(llClientStats.NumPeersUndialableWithoutHolepunch, 0)))
	qt.Check(t, qt.Not(qt.Equals(llClientStats.NumPeersUndialableWithoutHolepunchDialedAfterHolepunchConnect, 0)))
	qt.Check(t, qt.Not(qt.Equals(llClientStats.NumPeersProbablyOnlyConnectedDueToHolepunch, 0)))
}

func waitForConns(t *Torrent) {
	t.cl.lock()
	defer t.cl.unlock()
	for {
		for range t.conns {
			return
		}
		t.cl.event.Wait()
	}
}

func TestDialTcpNotAccepting(t *testing.T) {
	l, err := net.Listen("tcp", "localhost:0")
	qt.Check(t, qt.IsNil(err))
	defer l.Close()
	dialedConn, err := net.Dial("tcp", l.Addr().String())
	qt.Assert(t, qt.IsNil(err))
	dialedConn.Close()
}

func TestTcpSimultaneousOpen(t *testing.T) {
	const network = "tcp"
	ctx := context.Background()
	makeDialer := func(localPort int, remoteAddr string) func() (net.Conn, error) {
		dialer := net.Dialer{
			LocalAddr: &net.TCPAddr{
				Port: localPort,
			},
		}
		return func() (net.Conn, error) {
			return dialer.DialContext(ctx, network, remoteAddr)
		}
	}
	for range 10 {
		first, second := randPortPair()
		t.Logf("ports are %v and %v", first, second)
		err := testSimultaneousOpen(
			t.Cleanup,
			makeDialer(first, fmt.Sprintf("localhost:%d", second)),
			makeDialer(second, fmt.Sprintf("localhost:%d", first)),
		)
		if err == nil {
			return
		}
		if errors.Is(err, errMsgNotReceived) {
			t.Fatal(err)
		}
		t.Log(err)
	}
	t.Skip("couldn't synchronize dials")
}

func randIntInRange(low, high int) int {
	return rand.Intn(high-low+1) + low
}

func randDynamicPort() int {
	return randIntInRange(49152, 65535)
}

func randPortPair() (first int, second int) {
	first = randDynamicPort()
	for {
		second = randDynamicPort()
		if second != first {
			return
		}
	}
}

func writeMsg(conn net.Conn) {
	conn.Write([]byte(defaultMsg))
	conn.Close()
}

func readMsg(conn net.Conn) error {
	msgBytes, err := io.ReadAll(conn)
	if err != nil {
		return err
	}
	msgStr := string(msgBytes)
	if msgStr != defaultMsg {
		return fmt.Errorf("read %q", msgStr)
	}
	return nil
}

var errMsgNotReceived = errors.New("msg not received in time")

func testSimultaneousOpen(
	cleanup func(func()),
	firstDialer, secondDialer func() (net.Conn, error),
) error {
	errs := make(chan error)
	var dialsDone sync.WaitGroup
	const numDials = 2
	dialsDone.Add(numDials)
	signal := make(chan struct{})
	var dialersDone sync.WaitGroup
	dialersDone.Add(numDials)
	doDial := func(
		dialer func() (net.Conn, error),
		onSignal func(net.Conn),
	) {
		defer dialersDone.Done()
		conn, err := dialer()
		dialsDone.Done()
		errs <- err
		if err != nil {
			return
		}
		cleanup(func() {
			conn.Close()
		})
		<-signal
		onSignal(conn)
	}
	go doDial(
		firstDialer,
		func(conn net.Conn) {
			writeMsg(conn)
			errs <- nil
		},
	)
	go doDial(
		secondDialer,
		func(conn net.Conn) {
			gotMsg := make(chan error, 1)
			go func() {
				gotMsg <- readMsg(conn)
			}()
			select {
			case err := <-gotMsg:
				errs <- err
			case <-time.After(time.Second):
				errs <- errMsgNotReceived
			}
		},
	)
	dialsDone.Wait()
	for range numDials {
		err := <-errs
		if err != nil {
			return err
		}
	}
	close(signal)
	for range numDials {
		err := <-errs
		if err != nil {
			return err
		}
	}
	dialersDone.Wait()
	return nil
}

const defaultMsg = "hello"

func TestUtpSimultaneousOpen(t *testing.T) {
	t.Parallel()
	const network = "udp"
	ctx := context.Background()
	newUtpSocket := func(addr string) utpSocket {
		socket, err := NewUtpSocket(
			network,
			addr,
			log.Default,
		)
		qt.Assert(t, qt.IsNil(err))
		return socket
	}
	first := newUtpSocket("localhost:0")
	defer first.Close()
	second := newUtpSocket("localhost:0")
	defer second.Close()
	getDial := func(sock utpSocket, addr string) func() (net.Conn, error) {
		return func() (net.Conn, error) {
			return sock.DialContext(ctx, network, addr)
		}
	}
	t.Logf("first addr is %v. second addr is %v", first.Addr().String(), second.Addr().String())
	for range 10 {
		err := testSimultaneousOpen(
			t.Cleanup,
			getDial(first, second.Addr().String()),
			getDial(second, first.Addr().String()),
		)
		if err == nil {
			t.Fatal("expected utp to fail simultaneous open")
		}
		if errors.Is(err, errMsgNotReceived) {
			return
		}
		skipGoUtpDialIssue(t, err)
		t.Log(err)
		time.Sleep(time.Second)
	}
	t.FailNow()
}

func writeAndReadMsg(r, w net.Conn) error {
	go writeMsg(w)
	return readMsg(r)
}

func skipGoUtpDialIssue(t *testing.T, err error) {
	if err.Error() == "timed out waiting for ack" {
		t.Skip("the pure Go uTP library has issues; enable CGO for libutp")
	}
}

func TestUtpDirectDialMsg(t *testing.T) {
	t.Parallel()
	const network = "udp4"
	ctx := context.Background()
	newUtpSocket := func(addr string) utpSocket {
		socket, err := NewUtpSocket(network, addr, log.Default)
		qt.Assert(t, qt.IsNil(err))
		return socket
	}
	for range 10 {
		err := func() error {
			first := newUtpSocket("localhost:0")
			defer first.Close()
			second := newUtpSocket("localhost:0")
			defer second.Close()
			writer, err := first.DialContext(ctx, network, second.Addr().String())
			if err != nil {
				return err
			}
			defer writer.Close()
			reader, err := second.Accept()
			qt.Assert(t, qt.IsNil(err))
			defer reader.Close()
			return writeAndReadMsg(reader, writer)
		}()
		if err == nil {
			return
		}
		skipGoUtpDialIssue(t, err)
		t.Log(err)
		time.Sleep(time.Second)
	}
	t.FailNow()
}
