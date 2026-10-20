package httpTracker

import (
	"fmt"
	"net"
	"net/netip"

	"yoru/internal/bt/dht/krpc"
)

// TODO: Use netip.Addr and Option[[20]byte].
type Peer struct {
	IP   net.IP `bencode:"ip"`
	Port int    `bencode:"port"`
	ID   []byte `bencode:"peer id"`
}

func (p Peer) ToNetipAddrPort() (addrPort netip.AddrPort, ok bool) {
	addr, ok := netip.AddrFromSlice(p.IP)
	addrPort = netip.AddrPortFrom(addr, uint16(p.Port))
	return
}

func (p Peer) String() string {
	loc := net.JoinHostPort(p.IP.String(), fmt.Sprintf("%d", p.Port))
	if len(p.ID) != 0 {
		return fmt.Sprintf("%x at %s", p.ID, loc)
	} else {
		return loc
	}
}

// Set from the non-compact form in BEP 3. The tracker is untrusted: any missing or mistyped field is an error.
func (p *Peer) FromDictInterface(d map[string]any) error {
	ip, ok := d["ip"].(string)
	if !ok {
		return fmt.Errorf("peer ip is %T", d["ip"])
	}
	port, ok := d["port"].(int64)
	if !ok || port < 0 || port > 65535 {
		return fmt.Errorf("peer port is %v", d["port"])
	}
	p.IP = net.ParseIP(ip)
	if id, ok := d["peer id"].(string); ok {
		p.ID = []byte(id)
	}
	p.Port = int(port)
	return nil
}

func (p Peer) FromNodeAddr(na krpc.NodeAddr) Peer {
	p.IP = na.IP
	p.Port = na.Port
	return p
}
