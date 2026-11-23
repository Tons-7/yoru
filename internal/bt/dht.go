package bt

import (
	"fmt"
	"io"
	"net"

	"yoru/internal/bt/dht"
	"yoru/internal/bt/dht/krpc"
	peer_store "yoru/internal/bt/dht/peer-store"
)

type DhtServer interface {
	Stats() any
	ID() [20]byte
	Addr() net.Addr
	AddNode(ni krpc.NodeInfo) error
	Ping(addr *net.UDPAddr)
	Announce(hash [20]byte, port int, impliedPort bool) (DhtAnnounce, error)
	WriteStatus(io.Writer)
}

type PeerStorer interface {
	PeerStore() peer_store.Interface
}

type DhtAnnounce interface {
	Close()
	Peers() <-chan dht.PeersValues
}

type DhtServerWrapper struct {
	*dht.Server
}

func (me DhtServerWrapper) Stats() any {
	return me.Server.Stats()
}

type dhtAnnounceWrapper struct {
	*dht.Announce
}

func (me dhtAnnounceWrapper) Peers() <-chan dht.PeersValues {
	return me.Announce.Peers
}

func (me DhtServerWrapper) Announce(hash [20]byte, port int, impliedPort bool) (DhtAnnounce, error) {
	ann, err := me.Server.Announce(hash, port, impliedPort)
	return dhtAnnounceWrapper{ann}, err
}

func (me DhtServerWrapper) Ping(addr *net.UDPAddr) {
	me.Server.PingQueryInput(addr, dht.QueryInput{
		RateLimiting: dht.QueryRateLimiting{NoWaitFirst: true},
	})
}

var _ DhtServer = DhtServerWrapper{}

func writeDhtServerStatus(w io.Writer, s DhtServer) {
	dhtStats := s.Stats()
	fmt.Fprintf(w, " ID: %x\n", s.ID())
	fmt.Fprintf(w, " %+v\n", dhtStats)
}
