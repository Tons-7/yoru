package bt

import (
	"crypto"

	pp "yoru/internal/bt/peer_protocol"
)

const (
	pieceHash        = crypto.SHA1
	defaultChunkSize = 0x4000 // 16KiB

	// A chosen cap on BEP 9 "metadata_size": libtorrent-rasterbar's is 4 MiB, but real torrents reach 14 MB
	// (3597f16e239aeb8f8524a1a1c4e4725a0a96b470).
	maxMetadataSize uint32 = 16 * 1024 * 1024
)

func defaultPeerExtensionBytes() PeerExtensionBits {
	return pp.NewPeerExtensionBytes(pp.ExtensionBitDht, pp.ExtensionBitLtep, pp.ExtensionBitFast)
}
