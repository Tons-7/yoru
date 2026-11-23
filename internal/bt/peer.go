package bt

import (
	"context"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/anacrolix/chansync"
	. "github.com/anacrolix/generics"
	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2/bitmap"
	"github.com/anacrolix/missinggo/v2/panicif"
	"github.com/anacrolix/multiless"

	"yoru/internal/bt/mse"
	pp "yoru/internal/bt/peer_protocol"
	typedRoaring "yoru/internal/bt/typed-roaring"
)

type (
	Peer struct {
		_stats ConnStats

		cl *Client
		t  *Torrent

		legacyPeerImpl
		peerImpl  newHotPeerImpl
		callbacks *Callbacks

		RemoteAddr              PeerRemoteAddr
		Discovery               PeerSource
		trusted                 bool
		closed                  chansync.SetOnce
		closedCtx               context.Context
		closedCtxCancel         context.CancelFunc
		lastUsefulChunkReceived time.Time

		lastStartedExpectingToReceiveChunks time.Time
		cumulativeExpectedToReceiveChunks   time.Duration
		queue                               queueRate
		peerTouchedPieces                   map[pieceIndex]struct{}

		logger  log.Logger
		slogger *slog.Logger

		outgoing        bool
		Network         string
		localPublicAddr peerLocalPublicAddr
		bannableAddr    Option[bannableAddr]
		headerEncrypted bool
		cryptoMethod    mse.CryptoMethod

		lastMessageReceived time.Time
		completedHandshake  time.Time
		lastChunkSent       time.Time

		needRequestUpdate    updateRequestReason
		lastRequestUpdate    time.Time
		lastBecameInterested time.Time
		priorInterest        time.Duration

		choking bool

		peerInterested        bool
		peerChoking           bool
		PeerPrefersEncryption bool
		peerMinPieces         pieceIndex

		peerAllowedFast typedRoaring.Bitmap[pieceIndex]
	}

	PeerSource string

	PeerRemoteAddr interface {
		String() string
	}

	peerRequests = orderedBitmap[RequestIndex]

	updateRequestReason string
)

const (
	PeerSourceUtHolepunch     = "C"
	PeerSourceTracker         = "Tr"
	PeerSourceIncoming        = "I"
	PeerSourceDhtGetPeers     = "Hg"
	PeerSourceDhtAnnouncePeer = "Ha"
	PeerSourcePex             = "X"
	PeerSourceDirect          = "M"
)

const (
	peerUpdateRequestsPeerCancelReason     updateRequestReason = "Peer.cancel"
	peerUpdateRequestsRemoteRejectReason   updateRequestReason = "Peer.remoteRejectedRequest"
	peerUpdateRequestsCancelAnsweredReason updateRequestReason = "Peer.cancelAnswered"
)

func (p *Peer) Torrent() *Torrent {
	return p.t
}

func (p *Peer) Stats() (ret PeerStats) {
	p.locker().RLock()
	defer p.locker().RUnlock()
	ret.ConnStats = p._stats.Copy()
	ret.DownloadRate = p.downloadRate()
	ret.LastWriteUploadRate = p.peerImpl.lastWriteUploadRate()
	ret.RemotePieceCount = p.remotePieceCount()
	return
}

func (cn *Peer) updateExpectingChunks() {
	if cn.peerImpl.expectingChunks() {
		if cn.lastStartedExpectingToReceiveChunks.IsZero() {
			cn.lastStartedExpectingToReceiveChunks = time.Now()
		}
	} else {
		if !cn.lastStartedExpectingToReceiveChunks.IsZero() {
			cn.cumulativeExpectedToReceiveChunks += time.Since(cn.lastStartedExpectingToReceiveChunks)
			cn.lastStartedExpectingToReceiveChunks = time.Time{}
		}
	}
}

func (cn *Peer) locker() *lockWithDeferreds {
	return cn.t.cl.locker()
}

func (cn *Peer) bestPeerNumPieces() pieceIndex {
	if cn.t.haveInfo() {
		return cn.t.numPieces()
	}
	return cn.peerMinPieces
}

func (cn *Peer) remotePieceCount() pieceIndex {
	have := pieceIndex(cn.peerPieces().GetCardinality())
	if all, _ := cn.peerHasAllPieces(); all {
		have = cn.bestPeerNumPieces()
	}
	return have
}

func (cn *Peer) completedString() string {
	return fmt.Sprintf("%d/%d", cn.remotePieceCount(), cn.bestPeerNumPieces())
}

func eventAgeString(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return fmt.Sprintf("%.2fs ago", time.Since(t).Seconds())
}

func (cn *Peer) downloadRate() float64 {
	num := cn._stats.BytesReadUsefulData.Int64()
	if num == 0 {
		return 0
	}
	expecting := cn.totalExpectingTime().Seconds()
	if expecting <= 0 {
		return 0
	}
	return float64(num) / expecting
}

func (p *Peer) DownloadRate() float64 {
	return p.Stats().DownloadRate
}

func (cn *Peer) writeStatus(w io.Writer) {
	if cn.closed.IsSet() {
		fmt.Fprint(w, "CLOSED: ")
	}
	fmt.Fprintln(w, strings.Join(cn.peerImplStatusLines(), "\n"))
	cn.peerImplWriteStatus(w)
	fmt.Fprintf(w,
		"%d pieces touched, good chunks: %v/%v, dr: %.1f KiB/s\n",
		len(cn.peerTouchedPieces),
		&cn._stats.ChunksReadUseful,
		&cn._stats.ChunksRead,
		cn.downloadRate()/(1<<10),
	)
	fmt.Fprintf(w, "\n")
}

func (p *Peer) close() {
	if !p.closed.Set() {
		return
	}
	if p.closedCtx != nil {
		p.closedCtxCancel()
	}
	p.legacyPeerImpl.onClose()
	if p.t != nil {
		p.t.decPeerPieceAvailability(p)
	}
	for _, f := range p.callbacks.PeerClosed {
		f(p)
	}
}

func (p *Peer) Close() error {
	p.locker().Lock()
	defer p.locker().Unlock()
	p.close()
	return nil
}

func (cn *Peer) peerHasPiece(piece pieceIndex) bool {
	if all, known := cn.peerHasAllPieces(); all && known {
		return true
	}
	return cn.peerPieces().ContainsInt(piece)
}

const (
	writeBufferHighWaterLen = 1 << 15
	writeBufferLowWaterLen  = writeBufferHighWaterLen / 2
)

var (
	interestedMsgLen         = len(pp.Message{Type: pp.Interested}.MustMarshalBinary())
	requestMsgLen            = len(pp.Message{Type: pp.Request}.MustMarshalBinary())
	maxLocalToRemoteRequests = (writeBufferHighWaterLen - writeBufferLowWaterLen - interestedMsgLen) / requestMsgLen
)

func (cn *Peer) totalExpectingTime() (ret time.Duration) {
	ret = cn.cumulativeExpectedToReceiveChunks
	if !cn.lastStartedExpectingToReceiveChunks.IsZero() {
		ret += time.Since(cn.lastStartedExpectingToReceiveChunks)
	}
	return
}

type messageWriter func(pp.Message) bool

func (cn *Peer) modifyRelevantConnStats(f func(*ConnStats)) {
	f(&cn._stats)
	incAll := func(stats *ConnStats) bool {
		f(stats)
		return true
	}
	cn.upstreamConnStats()(incAll)
}

func (cn *Peer) upstreamConnStats() iter.Seq[*ConnStats] {
	return func(yield func(*ConnStats) bool) {
		if cn.t != nil {
			cn.relevantConnStats(&cn.t.connStats)(yield)
		}
		cn.relevantConnStats(&cn.cl.connStats)(yield)
	}
}

func (cn *Peer) readBytes(n int64) {
	cn.modifyRelevantConnStats(add(n, func(cs *ConnStats) *Count { return &cs.BytesRead }))
}

func (c *Peer) lastHelpful() (ret time.Time) {
	ret = c.lastUsefulChunkReceived
	if c.t.seeding() && c.lastChunkSent.After(ret) {
		ret = c.lastChunkSent
	}
	return
}

func chunkOverflowsPiece(cs ChunkSpec, pieceLength pp.Integer) bool {
	switch {
	default:
		return false
	case cs.Begin+cs.Length > pieceLength:
	case cs.Begin > pp.IntegerMax-cs.Length:
	}
	return true
}

func (c *Peer) doChunkReadStats(size int64) {
	c.modifyRelevantConnStats(func(cs *ConnStats) { cs.receivedChunk(size) })
}

func (c *Peer) receiveChunk(msg *pp.Message) error {

	ppReq := newRequestFromMessage(msg)
	t := c.t
	err := t.checkValidReceiveChunk(ppReq)
	if err != nil {
		err = log.WithLevel(log.Warning, err)
		return err
	}
	req := c.t.requestIndexFromRequest(ppReq)

	recordBlockForSmartBan := sync.OnceFunc(func() {
		c.recordBlockForSmartBan(req, msg.Piece)
	})
	redundant := false
	defer func() {
		if !redundant {
			recordBlockForSmartBan()
		}
	}()

	intended, err := c.peerImpl.checkReceivedChunk(req, msg, ppReq)
	if err != nil {
		return err
	}

	cl := t.cl

	if t.haveChunk(ppReq) {
		redundant = true
		c.modifyRelevantConnStats(add(1, func(cs *ConnStats) *Count { return &cs.ChunksReadWasted }))
		return nil
	}

	piece := t.piece(ppReq.Index.Int())

	c.modifyRelevantConnStats(add(1, func(cs *ConnStats) *Count { return &cs.ChunksReadUseful }))
	c.modifyRelevantConnStats(add(int64(len(msg.Piece)), func(cs *ConnStats) *Count { return &cs.BytesReadUsefulData }))
	if intended {
		c.modifyRelevantConnStats(add(int64(len(msg.Piece)), func(cs *ConnStats) *Count { return &cs.BytesReadUsefulIntendedData }))
	}
	for _, f := range c.t.cl.config.Callbacks.ReceivedUsefulData {
		f(ReceivedUsefulDataEvent{c, msg})
	}
	c.lastUsefulChunkReceived = time.Now()
	c.sampleRate()

	piece.incrementPendingWrites()
	piece.unpendChunkIndex(chunkIndexFromChunkSpec(ppReq.ChunkSpec, t.chunkSize))

	if p := t.requestingPeer(req); p != nil {
		if p.peerPtr() == c {
			p.logger.Slogger().Error("received chunk but still pending request", "peer", p, "req", req)
			panic("should not be pending request from conn that just received it")
		}
		p.cancel(req)
	}

	err = func() error {
		defer cl.lock()
		cl.unlock()
		recordBlockForSmartBan()
		return t.writeChunk(int(msg.Index), int64(msg.Begin), msg.Piece)
	}()

	if err != nil {
		t.pendRequest(req)
	}
	piece.decrementPendingWrites()

	if err != nil {
		c.logger.WithDefaultLevel(log.Error).Printf("writing received chunk %v: %v", req, err)
		c.onNeedUpdateRequests("Peer.receiveChunk error writing chunk")
		t.onWriteChunkErr(err)
		return nil
	}

	c.onDirtiedPiece(pieceIndex(ppReq.Index))

	if t.pieceAllDirty(pieceIndex(ppReq.Index)) && piece.pendingWrites == 0 {
		t.queuePieceCheck(pieceIndex(ppReq.Index))
	}

	cl.event.Broadcast()
	t.deferPublishPieceStateChange(pieceIndex(ppReq.Index))

	return nil
}

func (c *Peer) onDirtiedPiece(piece pieceIndex) {
	if c.peerTouchedPieces == nil {
		c.peerTouchedPieces = make(map[pieceIndex]struct{})
	}
	c.peerTouchedPieces[piece] = struct{}{}
	ds := &c.t.pieces[piece].dirtiers
	if *ds == nil {
		*ds = make(map[*Peer]struct{})
	}
	(*ds)[c] = struct{}{}
}

func (cn *Peer) netGoodPiecesDirtied() int64 {
	return cn._stats.PiecesDirtiedGood.Int64() - cn._stats.PiecesDirtiedBad.Int64()
}

func (c *Peer) peerHasWantedPieces() bool {
	if all, _ := c.peerHasAllPieces(); all {
		return !c.t.haveAllPieces() && !c.t._pendingPieces.IsEmpty()
	}
	if !c.t.haveInfo() {
		return !c.peerPieces().IsEmpty()
	}
	return c.peerPieces().Intersects(&c.t._pendingPieces)
}

func (c *Peer) peerPriority() (peerPriority, error) {
	return bep40Priority(c.remoteIpPort(), c.localPublicAddr)
}

func (c *Peer) remoteIp() net.IP {
	host, _, _ := net.SplitHostPort(c.RemoteAddr.String())
	return net.ParseIP(host)
}

func (c *Peer) remoteIpPort() IpPort {
	ipa, _ := tryIpPortFromNetAddr(c.RemoteAddr)
	return IpPort{IP: ipa.IP, Port: uint16(ipa.Port)}
}

func (c *Peer) trust() connectionTrust {
	return connectionTrust{c.trusted, c.netGoodPiecesDirtied()}
}

type connectionTrust struct {
	Implicit            bool
	NetGoodPiecesDirted int64
}

func (l connectionTrust) Cmp(r connectionTrust) int {
	return multiless.New().Bool(l.Implicit, r.Implicit).Int64(l.NetGoodPiecesDirted, r.NetGoodPiecesDirted).OrderingInt()
}

func (cn *Peer) newPeerPieces() *roaring.Bitmap {
	ret := cn.peerPieces().Clone()
	if all, _ := cn.peerHasAllPieces(); all {
		if cn.t.haveInfo() {
			ret.AddRange(0, bitmap.BitRange(cn.t.numPieces()))
		} else {
			ret.AddRange(0, bitmap.ToEnd)
		}
	}
	return ret
}

func (p *Peer) TryAsPeerConn() (*PeerConn, bool) {
	pc, ok := p.legacyPeerImpl.(*PeerConn)
	return pc, ok
}

type peerLocalPublicAddr = IpPort

func (p *Peer) recordBlockForSmartBan(req RequestIndex, blockData []byte) {
	if p.bannableAddr.Ok {
		p.t.smartBanCache.RecordBlock(p.bannableAddr.Value, req, blockData)
	}
}

func (p *Peer) initClosedCtx() {
	panicif.NotNil(p.closedCtx)
	p.closedCtx, p.closedCtxCancel = context.WithCancel(p.t.closedCtx)
}

func (p *Peer) relevantConnStats(all *AllConnStats) iter.Seq[*ConnStats] {
	return func(yield func(*ConnStats) bool) {
		if yield(&all.ConnStats) {
			yield(p.peerImpl.allConnStatsImplField(all))
		}
	}
}
