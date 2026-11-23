package dht

import (
	"context"
	_ "crypto/sha1"
	"errors"
	"net"
	"time"

	"github.com/anacrolix/log"
	"golang.org/x/time/rate"
	"yoru/internal/bt/metainfo"

	"yoru/internal/bt/dht/krpc"
	peer_store "yoru/internal/bt/dht/peer-store"
	"yoru/internal/bt/dht/transactions"
)

func defaultQueryResendDelay() time.Duration {
	return 2 * time.Second
}

type transactionKey = transactions.Key

type StartingNodesGetter func() ([]Addr, error)

type ServerConfig struct {
	NodeId      krpc.ID
	Conn        net.PacketConn
	Passive     bool
	WaitToReply bool

	StartingNodes StartingNodesGetter
	NoSecurity    bool
	PublicIP      net.IP

	OnQuery          func(query *krpc.Msg, source net.Addr) (propagate bool)
	OnAnnouncePeer   func(infoHash metainfo.Hash, ip net.IP, port int, portOk bool)
	QueryResendDelay func() time.Duration
	PeerStore        peer_store.Interface

	Logger log.Logger

	DefaultWant []krpc.Want

	SendLimiter *rate.Limiter
}

type ServerStats struct {
	GoodNodes                             int
	Nodes                                 int
	OutstandingTransactions               int
	SuccessfulOutboundAnnouncePeerQueries int64
	BadNodes                              uint
	OutboundQueriesAttempted              int64
}

type Peer = krpc.NodeAddr

var DefaultGlobalBootstrapHostPorts = []string{
	"router.utorrent.com:6881",
	"router.bittorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"dht.aelitis.com:6881",
	"router.silotis.us:6881",
	"dht.libtorrent.org:25401",
	"router.bittorrent.cloud:42069",
}

func GlobalBootstrapAddrs(network string) ([]Addr, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	addrs, err := ResolveHostPortsVia(ctx, net.DefaultResolver, DefaultGlobalBootstrapHostPorts)
	return AddrsForNetwork(network, addrs), err
}

func AddrsForNetwork(network string, addrs []Addr) []Addr {
	if network != "udp4" && network != "udp6" {
		return addrs
	}
	kept := addrs[:0]
	for _, a := range addrs {
		if (a.IP().To4() != nil) == (network == "udp4") {
			kept = append(kept, a)
		}
	}
	return kept
}

func ResolveHostPortsVia(ctx context.Context, r *net.Resolver, hostPorts []string) ([]Addr, error) {
	return resolveHostPorts(ctx, r.LookupHost, hostPorts)
}

func resolveHostPorts(
	ctx context.Context, lookup func(context.Context, string) ([]string, error), hostPorts []string,
) (addrs []Addr, err error) {
	for _, s := range hostPorts {
		host, port, err := net.SplitHostPort(s)
		if err != nil {
			panic(err)
		}
		hostAddrs, err := lookup(ctx, host)
		if err != nil {
			continue
		}
		for _, a := range hostAddrs {
			ua, err := net.ResolveUDPAddr("udp", net.JoinHostPort(a, port))
			if err != nil {
				log.Printf("error resolving %q: %v", a, err)
				continue
			}
			addrs = append(addrs, NewAddr(ua))
		}
	}
	if len(addrs) == 0 {
		err = errors.New("nothing resolved")
	}
	return
}

func RandomNodeID() (id krpc.ID) {
	return krpc.RandomNodeID()
}
