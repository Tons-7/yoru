package dht

import (
	"context"
	"crypto"
	_ "crypto/sha1"
	"errors"
	"math/rand"
	"net"
	"time"

	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2"
	"golang.org/x/time/rate"
	"yoru/internal/bt/iplist"
	"yoru/internal/bt/metainfo"

	"yoru/internal/bt/dht/bep44"
	"yoru/internal/bt/dht/krpc"
	peer_store "yoru/internal/bt/dht/peer-store"
	"yoru/internal/bt/dht/transactions"
)

func defaultQueryResendDelay() time.Duration {
	// This should be the highest reasonable UDP latency an end-user might have.
	return 2 * time.Second
}

type transactionKey = transactions.Key

type StartingNodesGetter func() ([]Addr, error)

// ServerConfig is a Server's configuration, for NewServer.
type ServerConfig struct {
	// Set NodeId Manually. Caller must ensure that if NodeId does not conform
	// to DHT Security Extensions, that NoSecurity is also set.
	NodeId krpc.ID
	Conn   net.PacketConn
	// Don't respond to queries from other nodes.
	Passive bool
	// Whether to wait for rate limiting to allow us to reply.
	WaitToReply bool

	// Called whenever the table has no good nodes (bootstrap included), usually giving the resolved
	// addresses of the "router" nodes that start a table.
	StartingNodes StartingNodesGetter
	// Disable the DHT security extension: http://www.libtorrent.org/dht_sec.html.
	NoSecurity bool
	// The IP blocklist to start with, applied before serving and bootstrapping.
	IPBlocklist iplist.Ranger
	// Used to secure the server's ID. Defaults to the Conn's LocalAddr(). Set to the IP that remote
	// nodes will see, as that IP is what they'll use to validate our ID.
	PublicIP net.IP

	// Hook received queries. Return false if you don't want to propagate to the default handlers.
	OnQuery func(query *krpc.Msg, source net.Addr) (propagate bool)
	// Called when a peer successfully announces to us.
	OnAnnouncePeer func(infoHash metainfo.Hash, ip net.IP, port int, portOk bool)
	// How long to wait before resending queries that haven't received a response. Defaults to 2s.
	// After the last send, a query is aborted after this time.
	QueryResendDelay func() time.Duration
	// TODO: Expose Peers, to return NodeInfo for received get_peers queries.
	PeerStore peer_store.Interface
	// BEP-44: Storing arbitrary data in the DHT. If not store provided, a default in-memory
	// implementation will be used.
	Store bep44.Store
	// BEP-44: expiration time with non-announced items. Two hours by default
	Exp time.Duration

	// log.Default without Debug when unset; a message with no level is given log.Debug.
	Logger log.Logger

	DefaultWant []krpc.Want

	SendLimiter *rate.Limiter
}

// ServerStats instance is returned by Server.Stats() and stores Server metrics
type ServerStats struct {
	// Nodes that answered our last query or haven't been queried yet.
	GoodNodes int
	// Count of nodes in the node table.
	Nodes int
	// Transactions awaiting a response.
	OutstandingTransactions int
	// Individual announce_peer requests that got a success response.
	SuccessfulOutboundAnnouncePeerQueries int64
	// Nodes that have been blocked.
	BadNodes                 uint
	OutboundQueriesAttempted int64
}

func jitterDuration(average time.Duration, plusMinus time.Duration) time.Duration {
	return average - plusMinus/2 + time.Duration(rand.Int63n(int64(plusMinus)))
}

type Peer = krpc.NodeAddr

var DefaultGlobalBootstrapHostPorts = []string{
	"router.utorrent.com:6881",
	"router.bittorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"dht.aelitis.com:6881",     // Vuze
	"router.silotis.us:6881",   // IPv6
	"dht.libtorrent.org:25401", // @arvidn's
	"router.bittorrent.cloud:42069",
}

// Returns the resolved addresses of the default global bootstrap nodes. Network is unused.
func GlobalBootstrapAddrs(network string) ([]Addr, error) {
	return ResolveHostPorts(DefaultGlobalBootstrapHostPorts)
}

// Resolves host:port strings to dht.Addrs, using the dht DNS resolver cache. Suitable for use with
// ServerConfig.BootstrapAddrs.
func ResolveHostPorts(hostPorts []string) (addrs []Addr, err error) {
	initDnsResolver()
	return resolveHostPorts(context.Background(), dnsResolver.LookupHost, hostPorts)
}

// ResolveHostPortsVia is ResolveHostPorts through r, uncached; nil r is the system resolver.
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
			// log.Default.WithDefaultLevel(log.Debug).Printf("error looking up %q: %v", s, err)
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

// Deprecated: Use function from krpc.
func RandomNodeID() (id krpc.ID) {
	return krpc.RandomNodeID()
}

func MakeDeterministicNodeID(public net.Addr) (id krpc.ID) {
	h := crypto.SHA1.New()
	h.Write([]byte(public.String()))
	h.Sum(id[:0:20])
	SecureNodeId(&id, missinggo.AddrIP(public))
	return
}
