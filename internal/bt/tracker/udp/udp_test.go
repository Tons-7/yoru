package udp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	_ "github.com/anacrolix/envpprof"
	qt "github.com/go-quicktest/qt"
	"github.com/stretchr/testify/require"
	"yoru/internal/bt/dht/krpc"
)

func TestNetIPv4Bytes(t *testing.T) {
	ip := net.IP([]byte{127, 0, 0, 1})
	if ip.String() != "127.0.0.1" {
		t.FailNow()
	}
	if string(ip) != "\x7f\x00\x00\x01" {
		t.Fatal([]byte(ip))
	}
}

func TestMarshalAnnounceResponse(t *testing.T) {
	peers := krpc.CompactIPv4NodeAddrs{
		{IP: []byte{127, 0, 0, 1}, Port: 2},
		{IP: []byte{255, 0, 0, 3}, Port: 4},
	}
	b, err := peers.MarshalBinary()
	require.NoError(t, err)
	require.EqualValues(t,
		"\x7f\x00\x00\x01\x00\x02\xff\x00\x00\x03\x00\x04",
		b)
	require.EqualValues(t, 12, binary.Size(AnnounceResponseHeader{}))
}

func TestLongWriteUDP(t *testing.T) {
	t.Parallel()
	l, err := net.ListenUDP("udp4", nil)
	require.NoError(t, err)
	defer l.Close()
	c, err := net.DialUDP("udp", nil, l.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for msgLen := 1; ; msgLen *= 2 {
		n, err := c.Write(make([]byte, msgLen))
		if err != nil {
			if isErrMessageTooLong(err) {
				return
			}
			t.Fatalf("expected message too long error: %v", err)
		}
		if n < msgLen {
			t.FailNow()
		}
	}
}

func TestShortBinaryRead(t *testing.T) {
	var data ResponseHeader
	err := binary.Read(bytes.NewBufferString("\x00\x00\x00\x01"), binary.BigEndian, &data)
	if err != io.ErrUnexpectedEOF {
		t.FailNow()
	}
}

func TestConvertInt16ToInt(t *testing.T) {
	i := 50000
	if int(uint16(int16(i))) != 50000 {
		t.FailNow()
	}
}

func TestConnClientLogDispatchUnknownTransactionId(t *testing.T) {
	const network = "udp"
	cc, err := NewConnClient(NewConnClientOpts{
		Network: network,
	})
	qt.Assert(t, qt.IsNil(err))
	defer cc.Close()
	pc, err := net.ListenPacket(network, ":0")
	qt.Assert(t, qt.IsNil(err))
	defer pc.Close()
	ccAddr := *cc.LocalAddr().(*net.UDPAddr)
	ipAddrs, err := net.DefaultResolver.LookupIPAddr(context.Background(), "localhost")
	qt.Assert(t, qt.IsNil(err))
	ccAddr.IP = ipAddrs[0].IP
	ccAddr.Zone = ipAddrs[0].Zone
	_, err = pc.WriteTo(make([]byte, 30), &ccAddr)
	qt.Assert(t, qt.IsNil(err))
}

func TestConnectionIdMismatch(t *testing.T) {
	t.Skip("Server host returns consistent connection ID in limited tests and so isn't effective.")
	cl, err := NewConnClient(NewConnClientOpts{
		Host:    "tracker.torrent.eu.org:451",
		Network: "udp",
	})
	qt.Assert(t, qt.IsNil(err))
	defer cl.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cl.Client.shouldReconnectOverride = func() bool { return true }
	started := time.Now()
	var wg sync.WaitGroup
	for range 2 {
		ar := AnnounceRequest{
			NumWant: -1,
			Event:   2,
		}
		rand.Read(ar.InfoHash[:])
		rand.Read(ar.PeerId[:])
		wg.Go(func() {
			_, _, err := cl.Announce(ctx, ar, Options{})
			t.Logf("announce error after %v: %v", time.Since(started), err)
		})
	}
	wg.Wait()
}
