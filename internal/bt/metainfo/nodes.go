package metainfo

import (
	"net"
	"strconv"

	"yoru/internal/bt/bencode"
)

type Node string

var _ bencode.Unmarshaler = (*Node)(nil)

func (n *Node) UnmarshalBencode(b []byte) (err error) {
	var iface any
	err = bencode.Unmarshal(b, &iface)
	if err != nil {
		return
	}
	switch v := iface.(type) {
	case string:
		*n = Node(v)
	case []any:
		if len(v) == 2 {
			host, hostOk := v[0].(string)
			port, portOk := v[1].(int64)
			if hostOk && portOk {
				*n = Node(net.JoinHostPort(host, strconv.FormatInt(port, 10)))
			}
		}
	}
	return
}
