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
	// This can be made into a type-param later, will be great for testing.
	ChunkSpec = types.ChunkSpec
)

// The piece request order: priority, partial pieces, rarity, then index. TODO: a per-torrent random
// affinity, deterministic per client?
func pieceOrderLess(i, j *PieceRequestOrderItem) multiless.Computation {
	return multiless.New().Int(
		int(j.State.Priority), int(i.State.Priority),
		// TODO: Should we match on complete here to prevent churn when availability changes? (Answer: Yes).
	).Bool(
		j.State.Partial, i.State.Partial,
	).Int(
		// Open: relative availability may cost determinism, and a piece nobody has sorts first here.
		i.State.Availability, j.State.Availability,
	).Int(
		i.Key.Index, j.Key.Index,
	).Lazy(func() multiless.Computation {
		a := i.Key.InfoHash.Value()
		b := j.Key.InfoHash.Value()
		return multiless.New().Cmp(bytes.Compare(a[:], b[:]))
	})
}

// Takes each requestable piece in order and says whether to go on; it no longer says whether the piece
// counts against the unverified limit, which made the order thrash on completion.
type RequestPieceFunc func(ih metainfo.Hash, pieceIndex int, orderState PieceRequestOrderState) bool

// Calls f with requestable pieces in order. Returns false if iteration should stop.
func GetRequestablePieces(
	input Input, pro *PieceRequestOrder,
	// Given pieces that passed Piece.Request, ready to download now.
	requestPiece RequestPieceFunc,
) bool {
	// Storage capacity left for this run, keyed by the storage capacity pointer on the storage
	// TorrentImpl. A nil value means no capacity limit.
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
		// Storage limits will always apply against requestable pieces, since we need to keep the
		// highest priority pieces, even if they're complete or in an undesirable state.
		if storageLeft != nil {
			if *storageLeft < pieceLength {
				break
			}
			*storageLeft -= pieceLength
		}
		if piece.Request() {
			if !requestPiece(ih, item.Key.Index, item.State) {
				// None of its blocks are considered, so it adds no unverified bytes.
				return false
			}
			// Nobody has it, so nothing will arrive: counting it lets a swarm's missing pieces fill the
			// window and stall the pieces peers do have.
			if !piece.CountUnverified() && !piece.Available() {
				continue
			}
		} else if !piece.CountUnverified() {
			// The piece is pristine, and we're not considering it for requests.
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
	// Storage capacity, shared among all Torrents with the same storage.TorrentCapacity pointer in
	// their storage.Torrent references.
	Capacity() (cap int64, capped bool)
	// Across all the Torrents. This might be partitioned by storage capacity key now.
	MaxUnverifiedBytes() int64
}
