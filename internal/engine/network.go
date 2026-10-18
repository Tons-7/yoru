package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"yoru/internal/bt"
	"yoru/internal/torrent"
)

// Torrents can keep to one adapter, a VPN's: listening, peer dials, trackers, DHT and web seeds all take its
// address, and with the adapter gone (the VPN dropped) the client stays down rather than use the open line.

// bindAddrs are the adapter's addresses torrents use; none is no binding. all is every address it has.
type bindAddrs struct {
	v4, v6 net.IP
	all    []net.IP
}

func (b bindAddrs) none() bool { return b.v4 == nil && b.v6 == nil }

// keeps: the adapter still has b's addresses; IPv6 privacy addresses coming and going beside them don't count.
func (now bindAddrs) keeps(b bindAddrs) bool {
	has := func(ip net.IP) bool {
		return ip == nil || slices.ContainsFunc(append([]net.IP{now.v4, now.v6}, now.all...), ip.Equal)
	}
	return has(b.v4) && has(b.v6)
}

func (b bindAddrs) String() string {
	var out []string
	for _, ip := range []net.IP{b.v4, b.v6} {
		if ip != nil {
			out = append(out, ip.String())
		}
	}
	return strings.Join(out, ", ")
}

// errNoAdapter: the chosen adapter is gone, down or without an address, so torrents wait for it.
var errNoAdapter = errors.New("not connected")

// errRestarting turns an add away while the client is down for the network; the caller tries again.
var errRestarting = fmt.Errorf("%w: torrents are waiting for the chosen network", torrent.ErrUnavailable)

// resolveAdapter finds the addresses of the adapter named; one that is down or gone has none.
func resolveAdapter(name string) (bindAddrs, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return bindAddrs{}, errNoAdapter
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return bindAddrs{}, errNoAdapter
	}
	var b bindAddrs
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || !ipnet.IP.IsGlobalUnicast() {
			continue
		}
		b.all = append(b.all, ipnet.IP)
		if v4 := ipnet.IP.To4(); v4 != nil {
			if b.v4 == nil {
				b.v4 = v4
			}
		} else if b.v6 == nil {
			b.v6 = ipnet.IP
		}
	}
	if b.none() {
		return bindAddrs{}, errNoAdapter
	}
	return b, nil
}

// local is the address to leave from for a remote of addr's family; nil when the adapter has none.
func (b bindAddrs) local(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return b.v6
		}
		return b.v4
	}
	// A name resolves to whichever family the local address asks for; IPv4 first, as most trackers have it.
	if b.v4 != nil {
		return b.v4
	}
	return b.v6
}

// dialContext dials from the adapter's address, so trackers and web seeds see only it.
func (b bindAddrs) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	ip := b.local(addr)
	if ip == nil {
		return nil, fmt.Errorf("%s: the chosen network has no address of that kind", addr)
	}
	return (&net.Dialer{LocalAddr: &net.TCPAddr{IP: ip}, Timeout: 30 * time.Second}).DialContext(ctx, network, addr)
}

// listenPacket gives a UDP tracker a socket on the adapter's address.
func (b bindAddrs) listenPacket(network, _ string) (net.PacketConn, error) {
	ip := b.v4
	if strings.HasSuffix(network, "6") || ip == nil {
		ip = b.v6
	}
	if ip == nil {
		return nil, errors.New("the chosen network has no address for UDP trackers")
	}
	return net.ListenPacket(network, net.JoinHostPort(ip.String(), "0"))
}

// applyBinding points every way out of the client at b; uPnP is off, as the router can't forward into a VPN.
// Peers are dialled from the listening sockets, TCP and uTP, which ListenHost puts on b.
func applyBinding(cfg *bt.ClientConfig, b bindAddrs) {
	if b.none() {
		return
	}
	cfg.ListenHost = func(network string) string {
		if strings.HasSuffix(network, "6") {
			return b.v6.String()
		}
		return b.v4.String()
	}
	cfg.DisableIPv4, cfg.DisableIPv6 = b.v4 == nil, b.v6 == nil
	cfg.HTTPDialContext = b.dialContext
	cfg.TrackerDialContext = b.dialContext
	cfg.TrackerListenPacket = b.listenPacket
	cfg.NoDefaultPortForwarding = true
}

// binding is what the client should be on now: nothing chosen is no binding; a chosen adapter that's gone is
// an error, which keeps the client from starting at all.
func (e *Engine) binding() (bindAddrs, error) {
	e.mu.Lock()
	adapter := e.limits.Adapter
	e.mu.Unlock()
	if adapter == "" {
		return bindAddrs{}, nil
	}
	b, err := e.resolve(adapter)
	if err != nil {
		return bindAddrs{}, fmt.Errorf("waiting for %s to connect; torrents use only that network", adapter)
	}
	return b, nil
}

// Var so tests need not wait.
var networkEvery = 5 * time.Second

// watchNetwork follows the chosen adapter: the client restarts on its new address, goes down without it, and
// comes back with it. On any network it follows the machine's addresses instead (followAddresses).
func (e *Engine) watchNetwork() {
	tick := time.NewTicker(networkEvery)
	defer tick.Stop()
	var addrs map[string]bool
	var stranded bool
	for range tick.C {
		e.mu.Lock()
		closed, adapter, bound, offline, up := e.closed, e.limits.Adapter, e.bound, e.offline, e.cl != nil
		e.mu.Unlock()
		if closed {
			return
		}
		if adapter == "" && up {
			addrs, stranded = e.followAddresses(addrs, stranded)
		}
		// Not started yet: the first request starts it, on the network as it is then.
		if adapter == "" || (!up && !offline) {
			continue
		}
		now, err := e.resolve(adapter)
		switch {
		case err != nil && up:
			e.rebind("the chosen network dropped")
		case err == nil && offline:
			if e.Ensure(context.Background()) == nil {
				e.log.Info("the chosen network is up; torrents carry on", "adapter", adapter, "address", now.String())
			}
		case err == nil && up && !now.keeps(bound):
			e.rebind("the chosen network's address changed")
		}
	}
}

// Var so tests can move the machine between networks.
var localAddrs = machineAddrs

// machineAddrs are the machine's addresses now: up, not loopback or link-local; nil if unknown.
func machineAddrs() map[string]bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && !n.IP.IsLinkLocalUnicast() && !n.IP.IsUnspecified() {
				out[n.IP.String()] = true
			}
		}
	}
	return out
}

// followAddresses drops what a network change killed, and redials once an address is up.
func (e *Engine) followAddresses(prev map[string]bool, stranded bool) (map[string]bool, bool) {
	now := localAddrs()
	if now == nil || prev == nil {
		return now, stranded
	}
	added, gone := false, false
	for a := range now {
		added = added || !prev[a]
	}
	for a := range prev {
		gone = gone || !now[a]
	}
	if !added && !gone {
		return now, stranded
	}
	if e.dropDead(prev, now) > 0 {
		stranded = true
	}
	// Not before an address is up: with no network the dials only fail.
	if stranded && added {
		e.redial()
		stranded = false
	}
	return now, stranded
}

// Var so tests can route as their machine would; nil without a route.
var routeSource = sourceFor

// sourceFor is the address the machine sends to remote from now; asking sends no packet.
func sourceFor(remote net.IP) net.IP {
	c, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: remote, Port: 9})
	if err != nil {
		return nil
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP
}

// dropDead closes TCP on a gone address, and uTP to a peer unroutable now or reached from a new address.
func (e *Engine) dropDead(prev, now map[string]bool) int {
	sources := map[string]net.IP{}
	dropped := 0
	for _, t := range e.held() {
		for _, pc := range t.PeerConns() {
			local := hostIP(pc.LocalAddr())
			if local == nil {
				continue
			}
			// Loopback and link-local are never listed, so they never count as gone.
			dead := prev[local.String()] && !now[local.String()]
			if local.IsUnspecified() {
				remote := hostIP(pc.RemoteAddr)
				if remote == nil {
					continue
				}
				src, asked := sources[remote.String()]
				if !asked {
					src = routeSource(remote)
					sources[remote.String()] = src
				}
				dead = src == nil || !(prev[src.String()] || src.IsLoopback() || src.IsLinkLocalUnicast())
			}
			if dead {
				pc.Close()
				dropped++
			}
		}
	}
	if dropped > 0 {
		e.log.Info("the network changed; dropped the connections on the old one", "connections", dropped)
	}
	return dropped
}

func hostIP(a interface{ String() string }) net.IP {
	if a == nil {
		return nil
	}
	host, _, err := net.SplitHostPort(a.String())
	if err != nil {
		return nil
	}
	return net.ParseIP(host)
}

// redial has every download wanting data try its known peers now, its backoff reset.
func (e *Engine) redial() {
	type ask struct {
		t     *bt.Torrent
		peers []bt.PeerInfo
	}
	var asks []ask
	e.mu.Lock()
	cl := e.cl
	for _, en := range e.byID {
		if en.paused || en.verifying || en.t.Info() == nil || !wantsData(en) {
			continue
		}
		en.aloneSince, en.regroupWait, en.regroups = time.Time{}, 0, 0
		a := ask{t: en.t}
		for _, r := range e.known[en.t.InfoHash()] {
			a.peers = append(a.peers, r.info)
		}
		asks = append(asks, a)
	}
	e.mu.Unlock()
	if cl == nil || len(asks) == 0 {
		return
	}
	e.log.Info("the network changed; downloads try their peers again", "downloads", len(asks))
	for _, a := range asks {
		askAround(a.t, cl, a.peers)
	}
}

// rebind closes the client and starts one on the network as it is now. Torrents keep their ids; reads in
// flight end and their players open them again.
func (e *Engine) rebind(why string) {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	e.mu.Lock()
	cl, pieces, closed := e.cl, e.pieces, e.closed
	e.mu.Unlock()
	if closed {
		return
	}
	if e.running {
		e.log.Info("restarting the torrent engine", "why", why)
		// Offline first, so no add lands on a client about to close.
		e.mu.Lock()
		e.offline = true
		e.mu.Unlock()
		e.writeSession(true)
		cl.Close()
		pieces.Close()
		e.mu.Lock()
		e.cl, e.pieces = nil, nil
		e.mu.Unlock()
		e.running = false
	}
	if err := e.start(); err != nil {
		e.log.Warn("torrents wait", "err", err)
		return
	}
	e.running = true
}

// Network says which adapter torrents keep to, the address they use, and whether they wait for it.
func (e *Engine) Network() torrent.Network {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := torrent.Network{Adapter: e.limits.Adapter}
	if n.Adapter != "" {
		n.Waiting = e.offline
		if e.cl != nil {
			n.Address = e.bound.String()
		}
	}
	return n
}
