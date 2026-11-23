package peer_protocol

import (
	"net"

	"yoru/internal/bt/bencode"
)

type CompactIp net.IP

var _ bencode.Marshaler = CompactIp{}

func (me CompactIp) MarshalBencode() ([]byte, error) {
	return bencode.Marshal(func() []byte {
		if ip4 := net.IP(me).To4(); ip4 != nil {
			return ip4
		} else {
			return me
		}
	}())
}
