package bt

import (
	"crypto"

	pp "yoru/internal/bt/peer_protocol"
)

const (
	pieceHash        = crypto.SHA1
	defaultChunkSize = 0x4000

	maxMetadataSize uint32 = 16 * 1024 * 1024
)

func defaultPeerExtensionBytes() PeerExtensionBits {
	return pp.NewPeerExtensionBytes(pp.ExtensionBitDht, pp.ExtensionBitLtep, pp.ExtensionBitFast)
}
