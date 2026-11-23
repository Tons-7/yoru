package bt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/missinggo/v2/panicif"

	"yoru/internal/bt/metainfo"
	pp "yoru/internal/bt/peer_protocol"
	"yoru/internal/bt/webseed"
)

type webseedPeer struct {
	peer           Peer
	client         webseed.Client
	activeRequests map[*webseedRequest]struct{}
	locker         sync.Locker
	hostKey        webseedHostKeyHandle
	url            webseedUrlKey

	penanceComplete time.Time
	lastCrime       error
}

func (me *webseedPeer) suspended() bool {
	return me.lastCrime != nil && time.Now().Before(me.penanceComplete)
}

func (me *webseedPeer) convict(err error, term time.Duration) {
	if me.suspended() {
		return
	}
	me.lastCrime = err
	me.penanceComplete = time.Now().Add(term)
}

func (*webseedPeer) allConnStatsImplField(stats *AllConnStats) *ConnStats {
	return &stats.WebSeeds
}

func (me *webseedPeer) cancelAllRequests() {
	for req := range me.activeRequests {
		req.Cancel("all requests cancelled", me.peer.t)
	}
}

func (me *webseedPeer) peerImplWriteStatus(w io.Writer) {}

func (me *webseedPeer) isLowOnRequests() bool {
	return false
}

func (me *webseedPeer) onNeedUpdateRequests(reason updateRequestReason) {
}

func (me *webseedPeer) expectingChunks() bool {
	return len(me.activeRequests) > 0
}

func (me *webseedPeer) checkReceivedChunk(RequestIndex, *pp.Message, Request) (bool, error) {
	return true, nil
}

func (me *webseedPeer) lastWriteUploadRate() float64 {
	return 0
}

var _ legacyPeerImpl = (*webseedPeer)(nil)

func (me *webseedPeer) peerImplStatusLines() []string {
	lines := []string{
		me.client.Url,
	}
	if me.lastCrime != nil {
		lines = append(lines, fmt.Sprintf("last crime: %v", me.lastCrime))
	}
	if me.suspended() {
		lines = append(lines, fmt.Sprintf("suspended for %v more", time.Until(me.penanceComplete)))
	}
	if len(me.activeRequests) > 0 {
		elems := make([]string, 0, len(me.activeRequests))
		for wr := range me.activeRequests {
			elems = append(elems, fmt.Sprintf("%v of [%v-%v)", wr.next, wr.begin, wr.end))
		}
		lines = append(lines, "active requests: "+strings.Join(elems, ", "))
	}
	return lines
}

func (ws *webseedPeer) String() string {
	return fmt.Sprintf("webseed peer for %q", ws.client.Url)
}

func (ws *webseedPeer) onGotInfo(info *metainfo.Info) {
	ws.client.SetInfo(info, ws.peer.t.fileSegmentsIndex.UnwrapPtr())
	ws.client.Pieces.Iterate(func(x uint32) bool {
		ws.peer.t.incPieceAvailability(pieceIndex(x))
		return true
	})
}

func (ws *webseedPeer) handleCancel(RequestIndex) {}

func (ws *webseedPeer) intoSpec(begin, end RequestIndex) webseed.RequestSpec {
	t := ws.peer.t
	start := t.requestIndexBegin(begin)
	endOff := t.requestIndexEnd(end - 1)
	return webseed.RequestSpec{Start: start, Length: endOff - start}
}

func (ws *webseedPeer) spawnRequest(begin, end RequestIndex, logger *slog.Logger) {
	extWsReq := ws.client.StartNewRequest(ws.peer.closedCtx, ws.intoSpec(begin, end), logger)
	wsReq := webseedRequest{
		logger:  logger,
		request: extWsReq,
		begin:   begin,
		next:    begin,
		end:     end,
	}
	if ws.hasOverlappingRequests(begin, end) {
		if webseed.PrintDebug {
			logger.Warn(
				"webseedPeer.spawnRequest: request overlaps existing",
				"new", &wsReq,
				"torrent", ws.peer.t)
		}
		ws.peer.t.cl.dumpCurrentWebseedRequests()
	}
	t := ws.peer.t
	cl := t.cl
	ws.activeRequests[&wsReq] = struct{}{}
	t.deferUpdateRegularTrackerAnnouncing()
	g.MakeMapIfNil(&cl.activeWebseedRequests)
	g.MapMustAssignNew(cl.activeWebseedRequests, ws.getRequestKey(&wsReq), &wsReq)
	ws.peer.updateExpectingChunks()
	panicif.Zero(ws.hostKey)
	ws.peer.t.cl.numWebSeedRequests[ws.hostKey]++
	ws.slogger().Debug(
		"starting webseed request",
		"begin", begin,
		"end", end,
		"len", end-begin,
	)
	go ws.sliceProcessor(&wsReq)
}

func (me *webseedPeer) getRequestKey(wr *webseedRequest) webseedUniqueRequestKey {
	return webseedUniqueRequestKey{
		url:        me.url,
		t:          me.peer.t,
		sliceIndex: me.peer.t.requestIndexToWebseedSliceIndex(wr.begin),
	}
}

func (me *webseedPeer) hasOverlappingRequests(begin, end RequestIndex) bool {
	for req := range me.activeRequests {
		if req.cancelled.Load() {
			continue
		}
		if begin < req.end && end > req.begin {
			return true
		}
	}
	return false
}

func (ws *webseedPeer) readChunksErrorLevel(err error, req *webseedRequest) slog.Level {
	if req.cancelled.Load() {
		return slog.LevelDebug
	}
	if ws.peer.closedCtx.Err() != nil {
		return slog.LevelDebug
	}
	if msg := err.Error(); strings.Contains(msg, "sent GOAWAY") && strings.Contains(msg, "ErrCode=ENHANCE_YOUR_CALM") {
		return slog.LevelInfo
	}
	if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
		return slog.LevelInfo
	}
	if errors.Is(err, webseed.ErrStatusOkForRangeRequest{}) {
		return slog.LevelDebug
	}
	return slog.LevelWarn
}

func (ws *webseedPeer) sliceProcessor(webseedRequest *webseedRequest) {
	pprof.SetGoroutineLabels(context.Background())
	locker := ws.locker
	err := ws.readChunks(webseedRequest)
	if webseed.PrintDebug && webseedRequest.next < webseedRequest.end {
		fmt.Printf("webseed peer request %v in %v stopped reading chunks early: %v\n", webseedRequest, ws.peer.t.name(), err)
		if err == nil {
			ws.peer.t.cl.dumpCurrentWebseedRequests()
		}
	}
	webseedRequest.Close()
	if err != nil {
		level := ws.readChunksErrorLevel(err, webseedRequest)
		ws.slogger().Log(context.TODO(), level, "webseed request error", "err", err)
		select {
		case <-ws.peer.closed.Done():
		case <-time.After(time.Duration(rand.Int63n(int64(10 * time.Second)))):
		}
	}
	ws.slogger().Debug("webseed request ended")
	locker.Lock()
	ws.deleteActiveRequest(webseedRequest)
	cl := ws.peer.cl
	if err == nil && cl.numWebSeedRequests[ws.hostKey] == webseedHostRequestConcurrency/2 {
		cl.updateWebseedRequestsWithReason("webseedPeer.runRequest low water")
	} else if cl.numWebSeedRequests[ws.hostKey] == 0 {
		cl.updateWebseedRequestsWithReason("webseedPeer.runRequest zero requests")
	}
	locker.Unlock()
}

func (ws *webseedPeer) deleteActiveRequest(wr *webseedRequest) {
	g.MustDelete(ws.activeRequests, wr)
	if len(ws.activeRequests) == 0 {
		ws.peer.t.deferUpdateRegularTrackerAnnouncing()
	}
	cl := ws.peer.cl
	cl.numWebSeedRequests[ws.hostKey]--
	g.MustDelete(cl.activeWebseedRequests, ws.getRequestKey(wr))
	ws.peer.updateExpectingChunks()
}

func (ws *webseedPeer) connectionFlags() string {
	return "WS"
}

func (ws *webseedPeer) drop() {}

func (cn *webseedPeer) providedBadData() {
	cn.convict(errors.New("provided bad data"), time.Minute)
}

func (ws *webseedPeer) onClose() {
	ws.peer.t.iterPeers(func(p *Peer) {
		if p.isLowOnRequests() {
			p.onNeedUpdateRequests("webseedPeer.onClose")
		}
	})
}

func (ws *webseedPeer) wantChunk(ri RequestIndex) bool {
	return ws.peer.t.wantReceiveChunk(ri)
}

func (ws *webseedPeer) maxChunkDiscard() RequestIndex {
	return RequestIndex(int(intCeilDiv(webseed.MaxDiscardBytes, ws.peer.t.chunkSize)))
}

func (ws *webseedPeer) wantedChunksInDiscardWindow(wr *webseedRequest) bool {
	panicif.GreaterThanOrEqual(wr.next, wr.end)
	windowEnd := wr.next + ws.maxChunkDiscard()
	panicif.LessThan(windowEnd, wr.next)
	for ri := wr.next; ri < wr.end && ri <= wr.next+ws.maxChunkDiscard(); ri++ {
		if ws.wantChunk(ri) {
			return true
		}
	}
	return false
}

func (ws *webseedPeer) readChunks(wr *webseedRequest) (err error) {
	t := ws.peer.t
	buf := t.getChunkBuffer()
	defer t.putChunkBuffer(buf)
	msg := pp.Message{
		Type: pp.Piece,
	}
	for {
		reqSpec := t.requestIndexToRequest(wr.next)
		chunkLen := reqSpec.Length.Int()
		buf = buf[:chunkLen]
		var n int
		n, err = io.ReadFull(wr.request.Body, buf)
		ws.peer.readBytes(int64(n))
		reqCtxErr := context.Cause(wr.request.Context())
		if errors.Is(err, reqCtxErr) {
			err = reqCtxErr
		}
		if webseed.PrintDebug && wr.cancelled.Load() {
			fmt.Printf("webseed read %v after cancellation: %v\n", n, err)
		}
		ws.peer.locker().Lock()
		if err != nil {
			if !wr.cancelled.Load() {
				ws.convict(err, time.Minute)
			}
			ws.peer.locker().Unlock()
			err = fmt.Errorf("reading chunk: %w", err)
			return
		}
		ws.peer.doChunkReadStats(int64(n))
		msg.Piece = buf
		msg.Index = reqSpec.Index
		msg.Begin = reqSpec.Begin

		wr.next++
		err = ws.peer.receiveChunk(&msg)
		stop := err != nil || wr.next >= wr.end
		if !stop {
			if !ws.wantedChunksInDiscardWindow(wr) {
				wr.Cancel("no wanted chunks in discard window", ws.peer.t)
			}
		}
		ws.peer.locker().Unlock()

		if err != nil {
			err = fmt.Errorf("processing chunk: %w", err)
		}
		if stop {
			return
		}
	}
}

func (me *webseedPeer) peerPieces() *roaring.Bitmap {
	return &me.client.Pieces
}

func (cn *webseedPeer) peerHasAllPieces() (all, known bool) {
	if !cn.peer.t.haveInfo() {
		return true, false
	}
	return cn.client.Pieces.GetCardinality() == uint64(cn.peer.t.numPieces()), true
}

func (me *webseedPeer) slogger() *slog.Logger {
	return me.peer.slogger
}
