package dialer

import (
	"context"
	"net"
)

type T interface {
	Dial(_ context.Context, addr string) (net.Conn, error)
	DialerNetwork() string
}

type WithContext interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

var Default = &net.Dialer{}

type WithNetwork struct {
	Network string
	Dialer  WithContext
}

func (me WithNetwork) DialerNetwork() string {
	return me.Network
}

func (me WithNetwork) Dial(ctx context.Context, addr string) (_ net.Conn, err error) {
	return me.Dialer.DialContext(ctx, me.Network, addr)
}
