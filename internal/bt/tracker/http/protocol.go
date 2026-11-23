package httpTracker

import (
	"fmt"

	"yoru/internal/bt/dht/krpc"

	"yoru/internal/bt/bencode"
)

type HttpResponse struct {
	FailureReason string                    `bencode:"failure reason"`
	Interval      int32                     `bencode:"interval"`
	TrackerId     string                    `bencode:"tracker id"`
	Complete      int32                     `bencode:"complete"`
	Incomplete    int32                     `bencode:"incomplete"`
	Peers         Peers                     `bencode:"peers"`
	Peers6        krpc.CompactIPv6NodeAddrs `bencode:"peers6"`
}

type Peers struct {
	List    []Peer
	Compact bool
}

func (me Peers) MarshalBencode() ([]byte, error) {
	if me.Compact {
		cnas := make([]krpc.NodeAddr, 0, len(me.List))
		for _, peer := range me.List {
			cnas = append(cnas, krpc.NodeAddr{
				IP:   peer.IP,
				Port: peer.Port,
			})
		}
		return krpc.CompactIPv4NodeAddrs(cnas).MarshalBencode()
	} else {
		return bencode.Marshal(me.List)
	}
}

var (
	_ bencode.Unmarshaler = (*Peers)(nil)
	_ bencode.Marshaler   = Peers{}
)

type dictPeer Peer

func (p *dictPeer) UnmarshalBencode(b []byte) error {
	var d map[string]any
	if err := bencode.Unmarshal(b, &d); err != nil {
		return fmt.Errorf("peer entry is not a dict: %w", err)
	}
	return (*Peer)(p).FromDictInterface(d)
}

func (me *Peers) UnmarshalBencode(b []byte) (err error) {
	if len(b) > 0 && b[0] == 'l' {
		vars.Add("http responses with list peers", 1)
		var list []dictPeer
		if err = bencode.Unmarshal(b, &list); err != nil {
			return
		}
		me.Compact = false
		for _, p := range list {
			if p.IP != nil {
				me.List = append(me.List, Peer(p))
			}
		}
		return
	}
	var v string
	if err = bencode.Unmarshal(b, &v); err != nil {
		vars.Add("http responses with unhandled peers type", 1)
		return fmt.Errorf("unsupported peers value: %w", err)
	}
	vars.Add("http responses with string peers", 1)
	var cnas krpc.CompactIPv4NodeAddrs
	err = cnas.UnmarshalBinary([]byte(v))
	if err != nil {
		return
	}
	me.Compact = true
	for _, cp := range cnas {
		me.List = append(me.List, Peer{
			IP:   cp.IP[:],
			Port: cp.Port,
		})
	}
	return
}
