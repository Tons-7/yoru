package bt

import (
	"cmp"
	"context"
	"fmt"
	"runtime/pprof"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/generics/heap"
	"github.com/anacrolix/multiless"

	requestStrategy "yoru/internal/bt/internal/request-strategy"
	"yoru/internal/bt/metainfo"
	typedRoaring "yoru/internal/bt/typed-roaring"
)

type (
	maxRequests = int
)

func (t *Torrent) requestStrategyPieceOrderState(i int) requestStrategy.PieceRequestOrderState {
	return requestStrategy.PieceRequestOrderState{
		Priority:     t.piece(i).purePriority(),
		Partial:      t.piecePartiallyDownloaded(i),
		Availability: t.piece(i).availability(),
	}
}

type (
	RequestIndex      = requestStrategy.RequestIndex
	chunkIndexType    = requestStrategy.ChunkIndex
	webseedSliceIndex RequestIndex
)

type desiredPeerRequests struct {
	requestIndexes []RequestIndex
	peer           *PeerConn
	pieceStates    []g.Option[requestStrategy.PieceRequestOrderState]
	readerAt       []RequestIndex
}

func (p *desiredPeerRequests) readFrom(piece pieceIndex) (RequestIndex, bool) {
	for _, r := range p.readerAt {
		if p.peer.t.pieceIndexOfRequestIndex(r) == piece {
			return r, true
		}
	}
	return 0, false
}

func (p *desiredPeerRequests) lessByValue(leftRequest, rightRequest RequestIndex) bool {
	t := p.peer.t
	leftPieceIndex := t.pieceIndexOfRequestIndex(leftRequest)
	rightPieceIndex := t.pieceIndexOfRequestIndex(rightRequest)
	ml := multiless.New()
	if p.peer.peerChoking {
		ml = ml.Bool(
			!p.peer.peerAllowedFast.Contains(leftPieceIndex),
			!p.peer.peerAllowedFast.Contains(rightPieceIndex),
		)
	}
	leftPiece := p.pieceStates[leftPieceIndex].UnwrapPtr()
	rightPiece := p.pieceStates[rightPieceIndex].UnwrapPtr()
	priority := func() PiecePriority {
		leftPriority := leftPiece.Priority
		rightPriority := rightPiece.Priority
		ml = ml.Int(
			-int(leftPriority),
			-int(rightPriority),
		)
		if !ml.Ok() {
			if leftPriority != rightPriority {
				panic("expected equal")
			}
		}
		return leftPriority
	}()
	if ml.Ok() {
		return ml.MustLess()
	}
	if priority == PiecePriorityNow {
		ml = ml.Int(leftPieceIndex, rightPieceIndex)
		if from, ok := p.readFrom(leftPieceIndex); ok && !ml.Ok() {
			ml = ml.Bool(leftRequest < from, rightRequest < from)
		}
		return multiless.EagerOrdered(ml, leftRequest, rightRequest).MustLess()
	}
	leftRequestState := t.requestState[leftRequest]
	rightRequestState := t.requestState[rightRequest]
	leftPeer := leftRequestState.peer.Value()
	rightPeer := rightRequestState.peer.Value()
	ml = ml.Bool(rightPeer == p.peer, leftPeer == p.peer)
	ml = ml.Bool(rightPeer == nil, leftPeer == nil)
	if ml.Ok() {
		return ml.MustLess()
	}
	if leftPeer != nil {
		ml = ml.Uint64(
			rightPeer.requestState.Requests.GetCardinality(),
			leftPeer.requestState.Requests.GetCardinality(),
		)
		leftLast := leftRequestState.when
		rightLast := rightRequestState.when
		if leftLast.IsZero() || rightLast.IsZero() {
			panic("expected non-zero last requested times")
		}
		ml = ml.CmpInt64(rightLast.Sub(leftLast).Nanoseconds())
	}
	if t.hasActiveWebseedRequests() {
		ml = ml.Cmp(-cmp.Compare(leftRequest, rightRequest))
	} else {
		ml = ml.Int(
			leftPiece.Availability,
			rightPiece.Availability)
		if priority == PiecePriorityReadahead {
			ml = ml.Int(leftPieceIndex, rightPieceIndex)
		} else {
			ml = ml.Int(t.pieceRequestOrder[leftPieceIndex], t.pieceRequestOrder[rightPieceIndex])
		}
		ml = multiless.EagerOrdered(ml, leftRequest, rightRequest)
	}
	return ml.MustLess()
}

type desiredRequestState struct {
	Requests   desiredPeerRequests
	Interested bool
}

func (cl *Client) getRequestablePieces(key clientPieceRequestOrderKeySumType, f requestStrategy.RequestPieceFunc) {
	input := key.getRequestStrategyInput(cl)
	order := cl.pieceRequestOrder[key].pieces
	requestStrategy.GetRequestablePieces(input, order, f)
}

func (t *Torrent) getRequestablePieces(f requestStrategy.RequestPieceFunc) {
	t.cl.getRequestablePieces(t.clientPieceRequestOrderKey(), f)
}

func (p *PeerConn) getDesiredRequestState() (desired desiredRequestState) {
	t := p.t
	if !t.haveInfo() {
		return
	}
	if t.closed.IsSet() {
		return
	}
	if t.dataDownloadDisallowed.Bool() {
		return
	}
	requestHeap := desiredPeerRequests{
		peer:           p,
		pieceStates:    t.requestPieceStates,
		requestIndexes: t.requestIndexes,
		readerAt:       t.readerRequests(),
	}
	clear(requestHeap.pieceStates)
	t.logPieceRequestOrder()
	var it typedRoaring.Iterator[RequestIndex]
	t.getRequestablePieces(
		func(ih metainfo.Hash, pieceIndex int, pieceExtra requestStrategy.PieceRequestOrderState) bool {
			if ih != *t.canonicalShortInfohash() {
				return true
			}
			if !p.peerHasPiece(pieceIndex) {
				return true
			}
			requestHeap.pieceStates[pieceIndex].Set(pieceExtra)
			allowedFast := p.peerAllowedFast.Contains(pieceIndex)
			t.iterUndirtiedRequestIndexesInPiece(&it, pieceIndex, func(r requestStrategy.RequestIndex) {
				if !allowedFast {
					desired.Interested = true
					if p.peerChoking && !p.requestState.Requests.Contains(r) {
						return
					}
				}
				cancelled := &p.requestState.Cancelled
				if !cancelled.IsEmpty() && cancelled.Contains(r) {
					return
				}
				requestHeap.requestIndexes = append(requestHeap.requestIndexes, r)
			})
			return true
		},
	)
	t.assertPendingRequests()
	desired.Requests = requestHeap
	return
}

func (p *PeerConn) maybeUpdateActualRequestState() {
	if p.needRequestUpdate == "" {
		return
	}
	p.updateRequestsWithReason(p.needRequestUpdate)
}

func (p *PeerConn) updateRequestsWithReason(reason updateRequestReason) {
	if p.closed.IsSet() {
		return
	}
	if p.t.cl.config.Debug {
		p.logger.Slogger().Debug("updating requests", "reason", p.needRequestUpdate)
	}
	pprof.Do(
		context.Background(),
		pprof.Labels("update request", string(reason)),
		func(_ context.Context) {
			p.updateRequests()
		},
	)
	rejectOnly := p.needRequestUpdate == peerUpdateRequestsRemoteRejectReason
	p.needRequestUpdate = ""
	if !rejectOnly {
		p.lastRequestUpdate = time.Now()
	}
}

func (p *PeerConn) updateRequests() {
	next := p.getDesiredRequestState()
	p.applyRequestState(next)
	p.t.cacheNextRequestIndexesForReuse(next.Requests.requestIndexes)
}

func (t *Torrent) cacheNextRequestIndexesForReuse(slice []RequestIndex) {
	if cap(slice) > cap(t.requestIndexes) {
		t.requestIndexes = slice[:0]
	}
}

func (p *Peer) allowSendNotInterested() bool {
	if p.t.haveAllPieces() {
		return true
	}
	all, known := p.peerHasAllPieces()
	if all || !known {
		return false
	}
	return roaring.AndNot(p.peerPieces(), &p.t._completedPieces).IsEmpty()
}

func (p *PeerConn) applyRequestState(next desiredRequestState) {
	current := &p.requestState
	if !next.Interested && p.requestState.Interested {
		if !p.allowSendNotInterested() {
			next.Interested = true
		}
	}
	if !p.setInterested(next.Interested) {
		return
	}
	more := true
	orig := next.Requests.requestIndexes
	requestHeap := heap.InterfaceForSlice(
		&next.Requests.requestIndexes,
		next.Requests.lessByValue,
	)
	heap.Init(requestHeap)

	t := p.t
	cancels := 0
	if requestHeap.Len() > 0 && p.needRequestUpdate != peerUpdateRequestsRemoteRejectReason &&
		p.needRequestUpdate != peerUpdateRequestsPeerCancelReason && p.needRequestUpdate != peerUpdateRequestsCancelAnsweredReason {
		cancels = p.jumpQueue(&next.Requests, next.Requests.requestIndexes[0])
	}
	originalRequestCount := current.Requests.GetCardinality()
	for {
		if requestHeap.Len() == 0 {
			break
		}
		if cancels+maxRequests(current.Requests.GetCardinality()-originalRequestCount) >= maxLocalToRemoteRequests {
			break
		}
		numPending := maxRequests(current.Requests.GetCardinality() + current.Cancelled.GetCardinality())
		if numPending >= p.nominalMaxRequests() && (cancels == 0 ||
			maxRequests(current.Requests.GetCardinality()) >= p.nominalMaxRequests() ||
			!next.Requests.isNow(next.Requests.requestIndexes[0])) {
			break
		}
		req := heap.Pop(requestHeap)
		if cap(next.Requests.requestIndexes) != cap(orig) {
			panic("changed")
		}

		if p.needRequestUpdate == peerUpdateRequestsRemoteRejectReason {
			continue
		}
		if cancels > 0 && current.Cancelled.Contains(req) {
			continue
		}

		existing := t.requestingPeer(req)
		if existing != nil && existing != p {
			if p.needRequestUpdate == peerUpdateRequestsPeerCancelReason {
				continue
			}

			diff := int64(current.Requests.GetCardinality()) + 1 - (int64(existing.uncancelledRequests()) - 1)
			poor := diff > 1 || (diff == 1 && !p.lastUsefulChunkReceived.After(existing.lastUsefulChunkReceived))
			if !p.steals(existing, req, !poor) {
				continue
			}
			t.cancelRequest(req)
		}
		more = p.mustRequest(req)
		if !more {
			break
		}
	}
	if !more {
		panic(fmt.Sprintf(
			"couldn't fill apply entire request state [newRequests=%v]",
			current.Requests.GetCardinality()-originalRequestCount))
	}
	if p.needRequestUpdate != peerUpdateRequestsRemoteRejectReason {
		p.queue.foundNothing = current.Requests.GetCardinality() == originalRequestCount
	}
}

func (p *desiredPeerRequests) isNow(r RequestIndex) bool {
	return p.pieceStates[p.peer.t.pieceIndexOfRequestIndex(r)].UnwrapPtr().Priority == PiecePriorityNow
}

func (p *PeerConn) jumpQueue(next *desiredPeerRequests, top RequestIndex) (cancels int) {
	t := p.t
	piece := t.pieceIndexOfRequestIndex(top)
	if !next.isNow(top) || p.peerChoking && !p.peerAllowedFast.Contains(piece) {
		return 0
	}
	if holder := t.requestingPeer(top); !t.pieceHasUnrequestedChunks(piece) &&
		(holder == nil || holder == p || holder.queue.rate <= 0 || p.queue.rate <= 2*holder.queue.rate) {
		return 0
	}
	budget := maxLocalToRemoteRequests / 2
	p.requestState.Requests.IterateSnapshotDescending(func(r RequestIndex) bool {
		if t.piece(t.pieceIndexOfRequestIndex(r)).purePriority() < PiecePriorityNow {
			p.cancel(r)
			cancels++
		}
		return cancels < budget
	})
	return cancels
}
