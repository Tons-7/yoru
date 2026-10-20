package dht

import (
	"net"

	"github.com/anacrolix/missinggo/v2"

	"yoru/internal/bt/dht/krpc"
)

// A node's address. String is called a lot, so it's quick; no Network, so it isn't a net.Addr: OS calls
// need the address underneath.
type Addr interface {
	Raw() net.Addr
	Port() int
	IP() net.IP
	String() string
	KRPC() krpc.NodeAddr
}

// Speeds up some of the commonly called Addr methods.
type cachedAddr struct {
	raw  net.Addr
	port int
	ip   net.IP
	s    string
}

func (ca cachedAddr) String() string {
	return ca.s
}

func (ca cachedAddr) KRPC() krpc.NodeAddr {
	return krpc.NodeAddr{
		IP:   ca.ip,
		Port: ca.port,
	}
}

func (ca cachedAddr) IP() net.IP {
	return ca.ip
}

func (ca cachedAddr) Port() int {
	return ca.port
}

func (ca cachedAddr) Raw() net.Addr {
	return ca.raw
}

func NewAddr(raw net.Addr) Addr {
	return cachedAddr{
		raw:  raw,
		s:    raw.String(),
		ip:   missinggo.AddrIP(raw),
		port: missinggo.AddrPort(raw),
	}
}
