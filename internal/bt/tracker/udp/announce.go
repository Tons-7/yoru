package udp

import (
	"encoding"

	"yoru/internal/bt/dht/krpc"
	"yoru/internal/bt/tracker/shared"
	"yoru/internal/bt/types"
	"yoru/internal/bt/types/infohash"
)

type AnnounceRequest struct {
	InfoHash   infohash.T
	PeerId     types.PeerID
	Downloaded int64
	Left       int64
	Uploaded   int64
	Event      AnnounceEvent
	IPAddress  uint32
	Key        int32
	NumWant    int32
	Port       uint16
}

type AnnounceEvent = shared.AnnounceEvent

type AnnounceResponsePeers interface {
	encoding.BinaryUnmarshaler
	NodeAddrs() []krpc.NodeAddr
}
