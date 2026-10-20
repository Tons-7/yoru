package udp

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/missinggo/v2"
)

type listenPacketFunc func(network, addr string) (net.PacketConn, error)

type NewConnClientOpts struct {
	// The network to operate to use, such as "udp4", "udp", "udp6".
	Network string
	// Tracker address
	Host string
	// If non-nil, forces either IPv4 or IPv6 in the UDP tracker wire protocol.
	Ipv6 *bool
	// Logger to use for internal errors.
	Logger *slog.Logger
	// Custom function to use as a substitute for net.ListenPacket
	ListenPacket listenPacketFunc
	// Looks Host up; nil is the system resolver.
	Resolver *net.Resolver
}

// Manages a Client with a specific connection.
type ConnClient struct {
	Client    Client
	conn      net.PacketConn
	d         Dispatcher
	closeOnce sync.Once
	closeErr  error
	newOpts   NewConnClientOpts
}

func (cc *ConnClient) reader() {
	b := make([]byte, 0x800)
	for {
		n, addr, err := cc.conn.ReadFrom(b)
		if err != nil {
			// TODO: Do bad things to the dispatcher, and incoming calls to the client if we have a
			// read error.
			cc.Close()
			break
		}
		err = cc.d.Dispatch(b[:n], addr)
		if err != nil {
			cc.newOpts.Logger.Debug("error dispatching received packet",
				"source addr", addr,
				"err", err,
			)
		}
	}
}

func ipv6(opt *bool, network string, remoteAddr net.Addr) bool {
	if opt != nil {
		return *opt
	}
	switch network {
	case "udp4":
		return false
	case "udp6":
		return true
	}
	rip := missinggo.AddrIP(remoteAddr)
	return rip.To16() != nil && rip.To4() == nil
}

// Allows a UDP Client to write packets to an endpoint without knowing about the network specifics.
type clientWriter struct {
	pc       net.PacketConn
	network  string
	address  string
	resolver *net.Resolver
}

func (me clientWriter) Write(p []byte) (n int, err error) {
	addr, err := me.resolve()
	if err != nil {
		return
	}
	return me.pc.WriteTo(p, addr)
}

// resolve is net.ResolveUDPAddr through the resolver: IPv4 first unless the network names a family.
func (me clientWriter) resolve() (*net.UDPAddr, error) {
	if me.resolver == nil {
		return net.ResolveUDPAddr(me.network, me.address)
	}
	host, port, err := net.SplitHostPort(me.address)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	family := "ip"
	if strings.HasSuffix(me.network, "4") || strings.HasSuffix(me.network, "6") {
		family = "ip" + me.network[len(me.network)-1:]
	}
	ips, err := me.resolver.LookupNetIP(ctx, family, host)
	if err != nil {
		return nil, err
	}
	pn, err := me.resolver.LookupPort(ctx, "udp", port)
	if err != nil {
		return nil, err
	}
	ip := ips[0]
	if i := slices.IndexFunc(ips, netip.Addr.Is4); i >= 0 {
		ip = ips[i]
	}
	return net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip.Unmap(), uint16(pn))), nil
}

func NewConnClient(opts NewConnClientOpts) (cc *ConnClient, err error) {
	var conn net.PacketConn
	if opts.ListenPacket != nil {
		conn, err = opts.ListenPacket(opts.Network, ":0")
	} else {
		conn, err = net.ListenPacket(opts.Network, ":0")
	}
	if err != nil {
		return
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	opts.Logger.Debug("new udp tracker client packet conn", "local addr", conn.LocalAddr())
	cc = &ConnClient{
		Client: Client{
			Writer: clientWriter{
				pc:       conn,
				network:  opts.Network,
				address:  opts.Host,
				resolver: opts.Resolver,
			},
		},
		conn:    conn,
		newOpts: opts,
	}
	cc.Client.Dispatcher = &cc.d
	go cc.reader()
	return
}

// Close is safe from the reader goroutine and any caller at once.
func (cc *ConnClient) Close() error {
	cc.closeOnce.Do(func() { cc.closeErr = cc.conn.Close() })
	return cc.closeErr
}

func (cc *ConnClient) Announce(
	ctx context.Context, req AnnounceRequest, opts Options,
) (
	h AnnounceResponseHeader, nas AnnounceResponsePeers, err error,
) {
	return cc.Client.Announce(ctx, req, opts, func(addr net.Addr) bool {
		return ipv6(cc.newOpts.Ipv6, cc.newOpts.Network, addr)
	})
}

func (cc *ConnClient) LocalAddr() net.Addr {
	return cc.conn.LocalAddr()
}
