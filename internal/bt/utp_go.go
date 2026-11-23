package bt

import (
	"net"

	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/inproc"
	"yoru/internal/bt/utp"
)

func NewUtpSocket(network, addr string, _ log.Logger) (utpSocket, error) {
	if network == "" {
		network = "udp"
	}
	var pc net.PacketConn
	var err error
	if network == "inproc" {
		pc, err = inproc.ListenPacket(network, addr)
	} else {
		pc, err = net.ListenPacket(network, addr)
	}
	if err != nil {
		return nil, err
	}
	s, err := utp.NewSocketFromPacketConn(skipTransientReads{pc})
	if s == nil {
		return nil, err
	}
	return s, err
}

type skipTransientReads struct {
	net.PacketConn
}

func (me skipTransientReads) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	for skipped := 0; ; skipped++ {
		n, addr, err = me.PacketConn.ReadFrom(b)
		if err == nil || skipped >= 1000 || !transientUDPReadError(err) {
			return
		}
	}
}
