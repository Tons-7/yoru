package requestStrategy

import (
	"bytes"

	"github.com/anacrolix/multiless"

	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/types"
)

type (
	RequestIndex  uint32
	ChunkIndex    = RequestIndex
	Request       = types.Request
	piecePriority = types.PiecePriority
	ChunkSpec     = types.ChunkSpec
)

func pieceOrderLess(i, j *PieceRequestOrderItem) multiless.Computation {
	return multiless.New().Int(
		int(j.State.Priority), int(i.State.Priority),
	).Bool(
		j.State.Partial, i.State.Partial,
	).Int(
		i.State.Availability, j.State.Availability,
	).Int(
		i.Key.Index, j.Key.Index,
	).Lazy(func() multiless.Computation {
		a := i.Key.InfoHash.Value()
		b := j.Key.InfoHash.Value()
		return multiless.New().Cmp(bytes.Compare(a[:], b[:]))
	})
}

type RequestPieceFunc func(ih metainfo.Hash, pieceIndex int, orderState PieceRequestOrderState) bool

func GetRequestablePieces(
	input Input, pro *PieceRequestOrder,
	requestPiece RequestPieceFunc,
) bool {
	var storageLeft *int64
	if cap, ok := input.Capacity(); ok {
		storageLeft = &cap
	}
	var (
		allTorrentsUnverifiedBytes int64
		maxUnverifiedBytes         = input.MaxUnverifiedBytes()
	)
	for item := range pro.tree.Scan {
		ih := item.Key.InfoHash.Value()
		t := input.Torrent(ih)
		piece := t.Piece(item.Key.Index)
		pieceLength := t.PieceLength()
		if storageLeft != nil {
			if *storageLeft < pieceLength {
				break
			}
			*storageLeft -= pieceLength
		}
		if piece.Request() {
			if !requestPiece(ih, item.Key.Index, item.State) {
				return false
			}
			if !piece.CountUnverified() && !piece.Available() {
				continue
			}
		} else if !piece.CountUnverified() {
			continue
		}
		allTorrentsUnverifiedBytes += pieceLength
		if maxUnverifiedBytes != 0 && allTorrentsUnverifiedBytes >= maxUnverifiedBytes {
			break
		}
	}
	return true
}

type Input interface {
	Torrent(metainfo.Hash) Torrent
	Capacity() (cap int64, capped bool)
	MaxUnverifiedBytes() int64
}
