package bt

import (
	"io"
	"net"
	"testing"

	"yoru/internal/bt/internal/testutil"
)

func TestPieceHashFailedBansOnlyALoneToucher(t *testing.T) {
	for _, c := range []struct {
		name    string
		touched int
		ioErr   error
		banned  bool
	}{
		{"one peer touched it", 1, nil, true},
		{"two peers touched it", 2, nil, false},
		{"the disk failed, not the peer", 1, io.ErrUnexpectedEOF, false},
	} {
		mi := testutil.GreetingMetaInfo()
		cl := newTestingClient(t)
		tt := cl.newTorrent(mi.HashInfoBytes(), badStorage{})
		tt.setChunkSize(2)
		tt.cl.lock()
		if err := tt.setInfoBytesLocked(mi.InfoBytes); err != nil {
			tt.cl.unlock()
			t.Fatal(err)
		}
		var ips []net.IP
		p := tt.piece(1)
		for i := range c.touched {
			ip := net.IPv4(10, 0, 0, byte(i+1))
			ips = append(ips, ip)
			pc := cl.newConnection(nil, newConnectionOpts{network: "tcp", remoteAddr: &net.TCPAddr{IP: ip, Port: 6881}})
			pc.setTorrent(tt)
			if p.dirtiers == nil {
				p.dirtiers = map[*Peer]struct{}{}
			}
			p.dirtiers[&pc.Peer] = struct{}{}
		}
		tt.dirtyChunks.AddRange(uint64(tt.pieceRequestIndexBegin(1)), uint64(tt.pieceRequestIndexBegin(1)+3))
		tt.pieceHashed(1, false, c.ioErr)
		for _, ip := range ips {
			if got := cl.badPeerIPPort(ip, 6881); got != c.banned {
				t.Errorf("%s: %v banned = %v, want %v", c.name, ip, got, c.banned)
			}
		}
		tt.cl.unlock()
	}
}
