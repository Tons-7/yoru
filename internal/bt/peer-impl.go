package bt

import (
	"io"

	"github.com/RoaringBitmap/roaring/v2"

	"yoru/internal/bt/metainfo"
	pp "yoru/internal/bt/peer_protocol"
)

// What differs between peer kinds (webseeds, BitTorrent connections), on Peer's child types as older code
// expects; underscored names avoid PeerConn's. New and fixed-up methods belong in newHotPeerImpl.
type legacyPeerImpl interface {
	// Whether to tell the peer to update requests (skipped for urgent changes); really PeerConn's.
	isLowOnRequests() bool
	// Notify that the peers requests should be updated for the provided reason.
	onNeedUpdateRequests(reason updateRequestReason)

	// handleCancel initiates cancellation of a request
	handleCancel(ri RequestIndex)
	cancelAllRequests()
	connectionFlags() string
	onClose()
	onGotInfo(info *metainfo.Info)
	// Drop connection. This may be a no-op if there is no connection.
	drop()
	// Rebuke the peer
	providedBadData()
	String() string
	// Per peer-impl lines for WriteStatus.
	peerImplStatusLines() []string
	peerImplWriteStatus(w io.Writer)

	// all: the peer should have everything; known: for certain (a have-all, not haves for every piece we
	// guess there are).
	peerHasAllPieces() (all, known bool)
	peerPieces() *roaring.Bitmap
}

// Abstract methods implemented by subclasses of Peer.
type newHotPeerImpl interface {
	lastWriteUploadRate() float64
	// Bookkeeping for a chunk being received and any specific checks.
	checkReceivedChunk(ri RequestIndex, msg *pp.Message, req Request) (intended bool, err error)
	// Whether we're expecting to receive chunks because we have outstanding requests. Used for
	// example to calculate download rate.
	expectingChunks() bool
	allConnStatsImplField(*AllConnStats) *ConnStats
}
