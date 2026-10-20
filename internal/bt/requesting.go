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
	// Since we have to store all the requests in memory, we can't reasonably exceed what could be
	// indexed with the memory space available.
	maxRequests = int
)

func (t *Torrent) requestStrategyPieceOrderState(i int) requestStrategy.PieceRequestOrderState {
	t.slogger().Debug("requestStrategyPieceOrderState", "pieceIndex", i)
	return requestStrategy.PieceRequestOrderState{
		Priority:     t.piece(i).purePriority(),
		Partial:      t.piecePartiallyDownloaded(i),
		Availability: t.piece(i).availability(),
	}
}

type (
	// A request index is a chunk indexed across the entire torrent. It's a single integer and can
	// be converted to a protocol request. TODO: This should be private.
	RequestIndex = requestStrategy.RequestIndex
	// This is request index but per-piece.
	chunkIndexType    = requestStrategy.ChunkIndex
	webseedSliceIndex RequestIndex
)

type desiredPeerRequests struct {
	requestIndexes []RequestIndex
	peer           *PeerConn
	pieceStates    []g.Option[requestStrategy.PieceRequestOrderState]
	// Where each reader is: a Now piece is fetched from there on, then what's before it.
	readerAt []RequestIndex
}

// readFrom is the chunk a reader in this piece waits on.
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
	// Requests that can't be served now go last, kept unless something better comes: with fast, a choke's
	// requests can still be good on the unchoke.
	if p.peer.peerChoking {
		ml = ml.Bool(
			!p.peer.peerAllowedFast.Contains(leftPieceIndex),
			!p.peer.peerAllowedFast.Contains(rightPieceIndex),
		)
	}
	leftPiece := p.pieceStates[leftPieceIndex].UnwrapPtr()
	rightPiece := p.pieceStates[rightPieceIndex].UnwrapPtr()
	// Putting this first means we can steal requests from lesser-performing peers for our first few
	// new requests.
	priority := func() PiecePriority {
		// The order's own priority, as Torrent.PiecePriority would look the piece up again.
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
	// Peers serve in the order asked: a piece at a time, each from its reader on and then its start, whoever
	// holds the chunks, so the chunk a reader waits on is taken over from a slow peer before the rest.
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
	// Prefer chunks already requested from this peer.
	ml = ml.Bool(rightPeer == p.peer, leftPeer == p.peer)
	// Prefer unrequested chunks.
	ml = ml.Bool(rightPeer == nil, leftPeer == nil)
	if ml.Ok() {
		return ml.MustLess()
	}
	if leftPeer != nil {
		// The right peer should also be set, or we'd have resolved the computation by now.
		ml = ml.Uint64(
			rightPeer.requestState.Requests.GetCardinality(),
			leftPeer.requestState.Requests.GetCardinality(),
		)
		// Could either of the lastRequested be Zero? That's what checking an existing peer is for.
		leftLast := leftRequestState.when
		rightLast := rightRequestState.when
		if leftLast.IsZero() || rightLast.IsZero() {
			panic("expected non-zero last requested times")
		}
		// The latest requested first: served in received order (as Transmission does), it waits longest, so
		// it is the best to cancel.
		ml = ml.CmpInt64(rightLast.Sub(leftLast).Nanoseconds())
	}
	// Any webseed requests on the Torrent mean it's prioritised enough to compete with peers' (they could
	// be filtered to the pieces compared).
	if t.hasActiveWebseedRequests() {
		// The highest index first, as webseeds take the lowest: peers and webseeds meet in the middle.
		ml = ml.Cmp(-cmp.Compare(leftRequest, rightRequest))
	} else {
		ml = ml.Int(
			leftPiece.Availability,
			rightPiece.Availability)
		if priority == PiecePriorityReadahead {
			// TODO: for readahead, distance from the reader would stop earlier reads starving later ones.
			ml = ml.Int(leftPieceIndex, rightPieceIndex)
		} else {
			ml = ml.Int(t.pieceRequestOrder[leftPieceIndex], t.pieceRequestOrder[rightPieceIndex])
		}
		ml = multiless.EagerOrdered(ml, leftRequest, rightRequest)
	}
	// Indexes in order, for storage writes; the heap holds no duplicates, so this always decides.
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

// The best-case request state (capacity, earlier pieces, low availability first), regardless of what
// this or other peers have requested; that is handled later.
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
	// Caller-provided allocation for roaring bitmap iteration.
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
					// Requesting needs interest. TODO: interest whenever their pieces (less allowed fast)
					// overlap what readers or pending pieces still need.
					desired.Interested = true
					// Requested when unchoked, or kept from before the choke and still unanswered (fast).
					if p.peerChoking && !p.requestState.Requests.Contains(r) {
						// We can't request this right now.
						return
					}
				}
				cancelled := &p.requestState.Cancelled
				if !cancelled.IsEmpty() && cancelled.Contains(r) {
					// Can't re-request while awaiting acknowledgement.
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

// Update requests if there's a reason assigned.
func (p *PeerConn) maybeUpdateActualRequestState() {
	if p.needRequestUpdate == "" {
		return
	}
	p.updateRequestsWithReason(p.needRequestUpdate)
}

// Updates requests right now with the given reason. Clobbers any deferred reason if there was one.
// Does all the necessary checks and includes profiler tags to assign the overhead.
func (p *PeerConn) updateRequestsWithReason(reason updateRequestReason) {
	if p.closed.IsSet() {
		return
	}
	if reason == peerUpdateRequestsTimerReason {
		since := time.Since(p.lastRequestUpdate)
		if since < updateRequestsTimerDuration {
			panic(since)
		}
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
	// Is there any chance we should leave this to run again, and have the caller clear it if they
	// called with this reason?
	p.needRequestUpdate = ""
	if !rejectOnly {
		p.lastRequestUpdate = time.Now()
	}
	if enableUpdateRequestsTimer {
		p.updateRequestsTimer.Reset(updateRequestsTimerDuration)
	}
}

func (p *PeerConn) updateRequests() {
	next := p.getDesiredRequestState()
	p.applyRequestState(next)
	p.t.cacheNextRequestIndexesForReuse(next.Requests.requestIndexes)
}

func (t *Torrent) cacheNextRequestIndexesForReuse(slice []RequestIndex) {
	// The slice comes back smaller when getDesiredRequestState returns early.
	if cap(slice) > cap(t.requestIndexes) {
		t.requestIndexes = slice[:0]
	}
}

// Whether "not interested" may go to the peer: qBittorrent seems to punish it while streaming needs
// nothing for a moment.
func (p *Peer) allowSendNotInterested() bool {
	// Except for caching, we're not likely to lose pieces very soon.
	if p.t.haveAllPieces() {
		return true
	}
	all, known := p.peerHasAllPieces()
	if all || !known {
		return false
	}
	// Allow losing interest if we have all the pieces the peer has.
	return roaring.AndNot(p.peerPieces(), &p.t._completedPieces).IsEmpty()
}

// Sends next to the peer, stealing from other peers and shuffling within the preferred order. Cancels
// happen synchronously elsewhere; only their pending acknowledgements are tracked here.
func (p *PeerConn) applyRequestState(next desiredRequestState) {
	current := &p.requestState
	// Make interest sticky
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
	if requestHeap.Len() > 0 && p.needRequestUpdate != peerUpdateRequestsRemoteRejectReason {
		cancels = p.jumpQueue(&next.Requests, next.Requests.requestIndexes[0])
	}
	originalRequestCount := current.Requests.GetCardinality()
	for {
		if requestHeap.Len() == 0 {
			break
		}
		// Cancels and requests share the write buffer, which has room for maxLocalToRemoteRequests.
		if cancels+maxRequests(current.Requests.GetCardinality()-originalRequestCount) >= maxLocalToRemoteRequests {
			break
		}
		numPending := maxRequests(current.Requests.GetCardinality() + current.Cancelled.GetCardinality())
		// Cancels not yet acknowledged still count, except against a Now piece: they're gone from ahead of it.
		if numPending >= p.nominalMaxRequests() && (cancels == 0 ||
			maxRequests(current.Requests.GetCardinality()) >= p.nominalMaxRequests() ||
			!next.Requests.isNow(next.Requests.requestIndexes[0])) {
			break
		}
		req := heap.Pop(requestHeap)
		if cap(next.Requests.requestIndexes) != cap(orig) {
			panic("changed")
		}

		// No new requests on a reject, which would keep asking a perhaps dead peer: one with room unchokes,
		// which brings it back.
		if p.needRequestUpdate == peerUpdateRequestsRemoteRejectReason {
			continue
		}
		// jumpQueue cancelled it in this pass; it waits for the peer's reject before it can be asked again.
		if cancels > 0 && current.Cancelled.Contains(req) {
			continue
		}

		existing := t.requestingPeer(req)
		if existing != nil && existing != p {
			// No stealing on a cancel (from t.cancelRequest below): the loser would steal it straight back.
			if p.needRequestUpdate == peerUpdateRequestsPeerCancelReason {
				continue
			}

			// Don't steal from the poor.
			diff := int64(current.Requests.GetCardinality()) + 1 - (int64(existing.uncancelledRequests()) - 1)
			// Steal a request that leaves us with one more request than the existing peer
			// connection if the stealer more recently received a chunk.
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
		// The write buffer was judged able to take the maximum: peak requests mustn't hang on what it held.
		panic(fmt.Sprintf(
			"couldn't fill apply entire request state [newRequests=%v]",
			current.Requests.GetCardinality()-originalRequestCount))
	}
	newPeakRequests := maxRequests(current.Requests.GetCardinality() - originalRequestCount)
	// A reject-triggered pass adds nothing by design; calling that "found nothing" would hold off
	// refills for retryEmptyAfter just as a seek's cancels are being acknowledged.
	if p.needRequestUpdate != peerUpdateRequestsRemoteRejectReason {
		p.queue.foundNothing = newPeakRequests == 0
	}
	p.peakRequests = newPeakRequests
}

func (p *desiredPeerRequests) isNow(r RequestIndex) bool {
	return p.pieceStates[p.peer.t.pieceIndexOfRequestIndex(r)].UnwrapPtr().Priority == PiecePriorityNow
}

// jumpQueue: a Now piece (a reader waits on it) not yet asked of anyone, or held by a peer half as quick,
// goes ahead of this peer's queue. Peers serve in the order asked, so lesser requests are cancelled first.
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
	// At most half the write buffer, so the requests behind them still fit. Farthest first: on a deep
	// queue, what survives ahead of the Now chunks should be the readahead needed right after them.
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

// This could be set to 10s to match the unchoke/request update interval recommended by some
// specifications. I've set it shorter to trigger it more often for testing for now.
const (
	updateRequestsTimerDuration = 3 * time.Second
	enableUpdateRequestsTimer   = false
)
