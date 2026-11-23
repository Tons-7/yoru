package requestStrategy

import (
	"iter"

	typedRoaring "yoru/internal/bt/typed-roaring"
)

type PeerRequestState struct {
	Interested bool
	Requests   PeerRequests
	Cancelled  typedRoaring.Bitmap[RequestIndex]
}

type PeerRequests interface {
	IsEmpty() bool
	GetCardinality() uint64
	Contains(RequestIndex) bool
	Add(RequestIndex)
	Rank(RequestIndex) uint64
	Iterate(func(RequestIndex) bool) (all bool)
	Iterator() iter.Seq[RequestIndex]
	CheckedRemove(RequestIndex) bool
	IterateSnapshot(func(RequestIndex) bool)
	IterateSnapshotDescending(func(RequestIndex) bool)
}
