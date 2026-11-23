package bt

import (
	"io"

	"github.com/RoaringBitmap/roaring/v2"

	"yoru/internal/bt/metainfo"
	pp "yoru/internal/bt/peer_protocol"
)

type legacyPeerImpl interface {
	isLowOnRequests() bool
	onNeedUpdateRequests(reason updateRequestReason)

	handleCancel(ri RequestIndex)
	cancelAllRequests()
	connectionFlags() string
	onClose()
	onGotInfo(info *metainfo.Info)
	drop()
	providedBadData()
	String() string
	peerImplStatusLines() []string
	peerImplWriteStatus(w io.Writer)

	peerHasAllPieces() (all, known bool)
	peerPieces() *roaring.Bitmap
}

type newHotPeerImpl interface {
	lastWriteUploadRate() float64
	checkReceivedChunk(ri RequestIndex, msg *pp.Message, req Request) (intended bool, err error)
	expectingChunks() bool
	allConnStatsImplField(*AllConnStats) *ConnStats
}
