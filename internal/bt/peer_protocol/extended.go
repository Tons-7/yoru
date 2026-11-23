package peer_protocol

import (
	"fmt"
	"net"
)

type (
	ExtendedHandshakeMessage struct {
		M            map[ExtensionName]ExtensionNumber `bencode:"m"`
		V            string                            `bencode:"v,omitempty"`
		Reqq         int                               `bencode:"reqq,omitempty"`
		Encryption   bool                              `bencode:"e"`
		MetadataSize int                               `bencode:"metadata_size,omitempty"`
		Port         int                               `bencode:"p,omitempty"`
		YourIp       CompactIp                         `bencode:"yourip,omitempty"`
		Ipv4         CompactIp                         `bencode:"ipv4,omitempty"`
		Ipv6         net.IP                            `bencode:"ipv6,omitempty"`
	}

	ExtensionName   string
	ExtensionNumber uint8
)

const (
	ExtensionNamePex ExtensionName = "ut_pex"

	ExtensionDeleteNumber ExtensionNumber = 0
)

func (me *ExtensionNumber) UnmarshalBinary(b []byte) error {
	if len(b) != 1 {
		return fmt.Errorf("extension number of %d bytes", len(b))
	}
	*me = ExtensionNumber(b[0])
	return nil
}
