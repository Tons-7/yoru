package engine

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"yoru/internal/bt"
	"yoru/internal/bt/metainfo"
	pp "yoru/internal/bt/peer_protocol"
	"yoru/internal/torrent"
)

// rawPeer is a bare wire connection to addr for mi, past the handshake; next reads one message.
func rawPeer(t *testing.T, addr string, mi metainfo.MetaInfo) (conn net.Conn, next func() (pp.Message, error)) {
	t.Helper()
	conn, next, err := dialPeer(addr, mi)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, next
}

func dialPeer(addr string, mi metainfo.MetaInfo) (conn net.Conn, next func() (pp.Message, error), err error) {
	if conn, err = net.DialTimeout("tcp", addr, 5*time.Second); err != nil {
		return
	}
	conn.SetDeadline(time.Now().Add(30 * time.Second))
	ih := mi.HashInfoBytes()
	var id [20]byte
	copy(id[:], "-TE0001-yoru-tests00")
	if _, err = pp.Handshake(context.Background(), conn, &ih, id, pp.PeerExtensionBits{}); err != nil {
		conn.Close()
		return
	}
	d := pp.Decoder{R: bufio.NewReader(conn), MaxLength: 256 << 10, Pool: &sync.Pool{New: func() any {
		b := make([]byte, 16<<10)
		return &b
	}}}
	return conn, func() (m pp.Message, err error) {
		err = d.Decode(&m)
		return
	}, nil
}

// A streaming peer asks in reading order, so chunks go out in the order asked, as libtorrent sends them.
func TestUploadsGoOutInRequestOrder(t *testing.T) {
	const pieceLen, chunk = 256 << 10, 16 << 10
	mi, seeds := bigFile(t, 4<<20, pieceLen, 1<<20)
	conn, next := rawPeer(t, seeds[0].String(), mi)
	if _, err := conn.Write(pp.Message{Type: pp.Interested}.MustMarshalBinary()); err != nil {
		t.Fatal(err)
	}
	if !unchokes(next) {
		t.Fatal("the seeder never unchoked")
	}
	// Scattered over the file, the way a reader that seeks asks; one write, so all are queued at once.
	asked := []int{37, 2, 90, 15, 64, 3, 128, 41, 7, 200, 99, 1}
	var out []byte
	for _, c := range asked {
		out = append(out, pp.Message{Type: pp.Request, Index: pp.Integer(c * chunk / pieceLen),
			Begin: pp.Integer(c * chunk % pieceLen), Length: chunk}.MustMarshalBinary()...)
	}
	if _, err := conn.Write(out); err != nil {
		t.Fatal(err)
	}
	var got []int
	for len(got) < len(asked) {
		m, err := next()
		if err != nil {
			t.Fatal(err)
		}
		if m.Type == pp.Piece {
			got = append(got, (int(m.Index)*pieceLen+int(m.Begin))/chunk)
		}
	}
	if !slices.Equal(got, asked) {
		t.Errorf("chunks came back %v; asked %v", got, asked)
	}
}

// unchokes reads until an unchoke, or until the connection's deadline or close says there won't be one.
func unchokes(next func() (pp.Message, error)) bool {
	for {
		m, err := next()
		if err != nil {
			return false
		}
		if m.Type == pp.Unchoke {
			return true
		}
	}
}

// Uploads follow Settings: peers are served while the episode downloads, nobody under "never" or once done.
func TestUploadChoices(t *testing.T) {
	mi, seeds := bigFile(t, 24<<20, 256<<10, 2<<20)
	e, id := seedEngine(t, mi, seeds, nil, func(*bt.ClientConfig) {})
	// A peer turned away at the handshake wasn't served either.
	served := func() bool {
		conn, next, err := dialPeer(fmt.Sprintf("127.0.0.1:%d", e.cl.LocalPort()), mi)
		if err != nil {
			return false
		}
		defer conn.Close()
		if _, err := conn.Write(pp.Message{Type: pp.Interested}.MustMarshalBinary()); err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		return unchokes(next)
	}

	if !served() {
		t.Error("while downloading, a peer asking got nothing")
	}
	e.SetLimits(torrent.Limits{Upload: torrent.UploadNever})
	if served() {
		t.Error(`with uploads set to "never", a peer was served`)
	}
	e.SetLimits(torrent.Limits{Upload: torrent.UploadWhileDownloading})
	en, _ := e.entryByID(id)
	for until := time.Now().Add(40 * time.Second); en.t.BytesMissing() > 0; time.Sleep(200 * time.Millisecond) {
		if time.Now().After(until) {
			t.Fatal("the episode never finished downloading")
		}
	}
	if served() {
		t.Error("a finished episode seeded")
	}
}
