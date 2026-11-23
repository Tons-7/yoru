package bt

import (
	"yoru/internal/bt/dht/krpc"

	"yoru/internal/bt/peer_protocol"
)

type PeerInfo struct {
	Id                 [20]byte
	Addr               PeerRemoteAddr
	Source             PeerSource
	SupportsEncryption bool
	peer_protocol.PexPeerFlags
	Trusted bool
}

func (me PeerInfo) equal(other PeerInfo) bool {
	return me.Id == other.Id &&
		me.Addr.String() == other.Addr.String() &&
		me.Source == other.Source &&
		me.SupportsEncryption == other.SupportsEncryption &&
		me.PexPeerFlags == other.PexPeerFlags &&
		me.Trusted == other.Trusted
}

func (me *PeerInfo) FromPex(na krpc.NodeAddr, fs peer_protocol.PexPeerFlags) {
	me.Addr = ipPortAddr{append([]byte(nil), na.IP...), na.Port}
	me.Source = PeerSourcePex
	if fs.Get(peer_protocol.PexPrefersEncryption) {
		me.SupportsEncryption = true
	}
	me.PexPeerFlags = fs
}

func (me PeerInfo) addr() IpPort {
	ipPort, _ := tryIpPortFromNetAddr(me.Addr)
	return IpPort{IP: ipPort.IP, Port: uint16(ipPort.Port)}
}
