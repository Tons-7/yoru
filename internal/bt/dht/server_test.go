package dht

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/anacrolix/log"
	"github.com/stretchr/testify/require"

	"yoru/internal/bt/dht/krpc"
	peer_store "yoru/internal/bt/dht/peer-store"
	"yoru/internal/bt/metainfo"
)

func TestPutAndGetAreRefused(t *testing.T) {
	l := log.Default.WithNames(t.Name())
	s1 := newServer(t, l.WithNames("s1"))
	s2 := newServer(t, l.WithNames("s2"))
	s2Addr := NewAddr(s2.Addr())
	seq := int64(1)
	for _, q := range []string{"get", "put"} {
		qr := s1.Query(context.TODO(), s2Addr, q, QueryInput{MsgArgs: krpc.MsgArgs{
			ID:     s1.ID(),
			Target: krpc.ID{1},
			Seq:    &seq,
			Token:  "x",
			V:      "hello",
		}})
		require.Error(t, qr.ToError(), q)
		require.NotNil(t, qr.Reply.E, q)
		require.Equal(t, krpc.ErrorMethodUnknown.Code, qr.Reply.E.Code, q)
	}
}

func TestAnnouncePeerNeedsToken(t *testing.T) {
	type call struct {
		port   int
		portOk bool
	}
	calls := make(chan call, 4)
	cfg := NewDefaultServerConfig()
	cfg.WaitToReply = true
	cfg.Conn = mustListen("localhost:0")
	cfg.PeerStore = &peer_store.InMemory{}
	cfg.OnAnnouncePeer = func(_ metainfo.Hash, _ net.IP, port int, portOk bool) { calls <- call{port, portOk} }
	host, err := NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(host.Close)
	asker := newServer(t, log.Default.WithNames("asker"))
	otherCfg := NewDefaultServerConfig()
	otherCfg.WaitToReply = true
	otherCfg.Conn = mustListen("127.0.0.2:0")
	other, err := NewServer(otherCfg)
	require.NoError(t, err)
	t.Cleanup(other.Close)
	hostAddr := NewAddr(host.Addr())
	ih := krpc.ID{9}
	announce := func(from *Server, token string, port int, implied bool) {
		from.Query(context.TODO(), hostAddr, "announce_peer", QueryInput{MsgArgs: krpc.MsgArgs{
			ID: from.ID(), InfoHash: ih, Token: token, Port: &port, ImpliedPort: implied,
		}, NumTries: 1})
	}
	noCall := func(why string) {
		select {
		case c := <-calls:
			t.Fatalf("%s: got a call %+v", why, c)
		case <-time.After(200 * time.Millisecond):
		}
	}

	announce(asker, "made up", 1234, false)
	noCall("made-up token")

	qr := asker.Query(context.TODO(), hostAddr, "get_peers", QueryInput{MsgArgs: krpc.MsgArgs{ID: asker.ID(), InfoHash: ih}})
	require.NoError(t, qr.ToError())
	require.NotNil(t, qr.Reply.R.Token)
	token := *qr.Reply.R.Token

	announce(other, token, 1234, false)
	noCall("token made for another address")

	announce(asker, token, 1234, false)
	require.Equal(t, call{1234, true}, <-calls)

	announce(asker, token, 1, true)
	got := <-calls
	require.True(t, got.portOk)
	require.Equal(t, asker.Addr().(*net.UDPAddr).Port, got.port)
}

func newServer(t *testing.T, l log.Logger) *Server {
	cfg := NewDefaultServerConfig()
	cfg.WaitToReply = true

	cfg.Conn = mustListen("localhost:0")
	cfg.Logger = l
	s, err := NewServer(cfg)
	if err != nil {
		panic(err)
	}

	t.Cleanup(func() {
		s.Close()
	})

	return s
}
