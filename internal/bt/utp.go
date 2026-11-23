package bt

import (
	"context"
	"net"
)

type utpSocket interface {
	net.PacketConn
	Accept() (net.Conn, error)
	Addr() net.Addr
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}
