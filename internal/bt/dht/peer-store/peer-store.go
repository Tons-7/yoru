package peer_store

import (
	"yoru/internal/bt/metainfo"

	"yoru/internal/bt/dht/krpc"
)

type InfoHash = metainfo.Hash

type Interface interface {
	AddPeer(InfoHash, krpc.NodeAddr)
	GetPeers(InfoHash) []krpc.NodeAddr
}
