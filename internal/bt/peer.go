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
	// Generic Peer-like fields. Could be WebSeed, BitTorrent over TCP, uTP or WebRTC.
	Peer struct {
		// First to ensure 64-bit alignment for atomics. See #262.
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
		// Pieces we've accepted chunks for from the peer.
		peerTouchedPieces map[pieceIndex]struct{}

		logger  log.Logger
		slogger *slog.Logger

		// Belongs in PeerConn:

		outgoing bool
		Network  string
		// Our address as the remote peer sees it (WebRTC gets it right without config hints).
		localPublicAddr peerLocalPublicAddr
		bannableAddr    Option[bannableAddr]
		// True if the connection is operating over MSE obfuscation.
		headerEncrypted bool
		cryptoMethod    mse.CryptoMethod

		lastMessageReceived time.Time
		completedHandshake  time.Time
		lastChunkSent       time.Time

		// Stuff controlled by the local peer.
		needRequestUpdate    updateRequestReason
		updateRequestsTimer  *time.Timer
		lastRequestUpdate    time.Time
		peakRequests         maxRequests
		lastBecameInterested time.Time
		priorInterest        time.Duration

		choking bool

		// Stuff controlled by the remote peer.
		peerInterested        bool
		peerChoking           bool
		PeerPrefersEncryption bool // as indicated by 'e' field in extension handshake
		// The fewest pieces the torrent can have going by this peer; useful until the info arrives.
		peerMinPieces pieceIndex

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
	PeerSourceDhtGetPeers     = "Hg" // Peers we found by searching a DHT.
	PeerSourceDhtAnnouncePeer = "Ha" // Peers that were announced to us by a DHT.
	PeerSourcePex             = "X"
	// The peer was given directly, such as through a magnet link.
	PeerSourceDirect = "M"
)

// Named reasons, as updates may act on them; only the first before an update runs counts.
const (
	peerUpdateRequestsPeerCancelReason   updateRequestReason = "Peer.cancel"
	peerUpdateRequestsRemoteRejectReason updateRequestReason = "Peer.remoteRejectedRequest"
)

// Torrent is the Peer's for its life; nil on an incoming connection before the handshake is accepted.
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

// The best guess at number of pieces in the torrent for this peer.
func (cn *Peer) bestPeerNumPieces() pieceIndex {
	if cn.t.haveInfo() {
		return cn.t.numPieces()
	}
	return cn.peerMinPieces
}

// How many pieces we think the peer has.
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
	return float64(num) / cn.totalExpectingTime().Seconds()
}

// Deprecated: Use Peer.Stats.
func (p *Peer) DownloadRate() float64 {
	return p.Stats().DownloadRate
}

func (cn *Peer) writeStatus(w io.Writer) {
	// \t isn't preserved in <pre> blocks?
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
	// Not set until Torrent is known.
	if p.closedCtx != nil {
		p.closedCtxCancel()
	}
	if p.updateRequestsTimer != nil {
		p.updateRequestsTimer.Stop()
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

// Peer definitely has a piece, for purposes of requesting. So it's not sufficient that we think
// they do (known=true).
func (cn *Peer) peerHasPiece(piece pieceIndex) bool {
	if all, known := cn.peerHasAllPieces(); all && known {
		return true
	}
	return cn.peerPieces().ContainsInt(piece)
}

// Per-conn write buffer; requests refill it once it drains below the low water mark.
const (
	writeBufferHighWaterLen = 1 << 15
	writeBufferLowWaterLen  = writeBufferHighWaterLen / 2
)

var (
	interestedMsgLen = len(pp.Message{Type: pp.Interested}.MustMarshalBinary())
	requestMsgLen    = len(pp.Message{Type: pp.Request}.MustMarshalBinary())
	// This is the maximum request count that could fit in the write buffer if it's at or below the
	// low water mark when we run maybeUpdateActualRequestState.
	maxLocalToRemoteRequests = (writeBufferHighWaterLen - writeBufferLowWaterLen - interestedMsgLen) / requestMsgLen
)

func (cn *Peer) totalExpectingTime() (ret time.Duration) {
	ret = cn.cumulativeExpectedToReceiveChunks
	if !cn.lastStartedExpectingToReceiveChunks.IsZero() {
		ret += time.Since(cn.lastStartedExpectingToReceiveChunks)
	}
	return
}

// Sends a message and says whether more may follow.
type messageWriter func(pp.Message) bool

// All ConnStats that include this connection. Some objects are not known until the handshake is
// complete, after which it's expected to reconcile the differences.
func (cn *Peer) modifyRelevantConnStats(f func(*ConnStats)) {
	// Every peer has basic ConnStats for now.
	f(&cn._stats)
	incAll := func(stats *ConnStats) bool {
		f(stats)
		return true
	}
	cn.upstreamConnStats()(incAll)
}

// Yields relevant upstream ConnStats. Skips Torrent if it isn't set.
func (cn *Peer) upstreamConnStats() iter.Seq[*ConnStats] {
	return func(yield func(*ConnStats) bool) {
		// PeerConn can be nil when it hasn't completed handshake.
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

// Returns whether any part of the chunk would lie outside a piece of the given length.
func chunkOverflowsPiece(cs ChunkSpec, pieceLength pp.Integer) bool {
	switch {
	default:
		return false
	case cs.Begin+cs.Length > pieceLength:
	// Check for integer overflow
	case cs.Begin > pp.IntegerMax-cs.Length:
	}
	return true
}

func runSafeExtraneous(f func()) {
	if true {
		go f()
	} else {
		f()
	}
}

func (c *Peer) doChunkReadStats(size int64) {
	c.modifyRelevantConnStats(func(cs *ConnStats) { cs.receivedChunk(size) })
}

// Handle a received chunk from a peer. TODO: Break this out into non-wire protocol specific
// handling. Avoid shoehorning into a pp.Message.
func (c *Peer) receiveChunk(msg *pp.Message) error {
	ChunksReceived.Add("total", 1)

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
	// Before returning, ideally unlocked, and only for valid chunks: hashing frees just in-bounds ones.
	defer recordBlockForSmartBan()

	if c.peerChoking {
		ChunksReceived.Add("while choked", 1)
	}

	intended, err := c.peerImpl.checkReceivedChunk(req, msg, ppReq)
	if err != nil {
		return err
	}

	cl := t.cl

	// Do we actually want this chunk?
	if t.haveChunk(ppReq) {
		ChunksReceived.Add("redundant", 1)
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

	// Marked unwritten before anything else touches it.
	piece.incrementPendingWrites()
	// Ours now, so it isn't fetched again while it waits for storage.
	piece.unpendChunkIndex(chunkIndexFromChunkSpec(ppReq.ChunkSpec, t.chunkSize))

	// Cancel pending requests for this chunk from *other* peers.
	if p := t.requestingPeer(req); p != nil {
		if p.peerPtr() == c {
			p.logger.Slogger().Error("received chunk but still pending request", "peer", p, "req", req)
			panic("should not be pending request from conn that just received it")
		}
		p.cancel(req)
	}

	err = func() error {
		cl.unlock()
		defer cl.lock()
		// Opportunistically do this here while we aren't holding the client lock.
		recordBlockForSmartBan()
		concurrentChunkWrites.Add(1)
		defer concurrentChunkWrites.Add(-1)
		// Written inline (one at a time per connection) so errors are handled here. TODO: let storage
		// write concurrently and report errors back.
		return t.writeChunk(int(msg.Index), int64(msg.Begin), msg.Piece)
	}()

	piece.decrementPendingWrites()

	if err != nil {
		c.logger.WithDefaultLevel(log.Error).Printf("writing received chunk %v: %v", req, err)
		t.pendRequest(req)
		// An update may have run during the failed write, so one is due after pending it again.
		c.onNeedUpdateRequests("Peer.receiveChunk error writing chunk")
		t.onWriteChunkErr(err)
		return nil
	}

	c.onDirtiedPiece(pieceIndex(ppReq.Index))

	// We need to ensure the piece is only queued once, so only the last chunk writer gets this job.
	if t.pieceAllDirty(pieceIndex(ppReq.Index)) && piece.pendingWrites == 0 {
		// Chunks aren't pended here, so dirty-chunk checks like haveChunk needn't know hashing
		// states; they must be pended later.
		t.queuePieceCheck(pieceIndex(ppReq.Index))
	}

	cl.event.Broadcast()
	// We do this because we've written a chunk, and may change PieceState.Partial.
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

// Returns a new Bitmap that includes bits for all pieces the peer could have based on their claims.
func (cn *Peer) newPeerPieces() *roaring.Bitmap {
	// TODO: Can we use copy on write?
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

func (p *Peer) decPeakRequests() {
	// May go negative when the update timer lowers peak below the outstanding requests.
	p.peakRequests--
}

func (p *Peer) recordBlockForSmartBan(req RequestIndex, blockData []byte) {
	if p.bannableAddr.Ok {
		p.t.smartBanCache.RecordBlock(p.bannableAddr.Value, req, blockData)
	}
}

func (p *Peer) initClosedCtx() {
	panicif.NotNil(p.closedCtx)
	p.closedCtx, p.closedCtxCancel = context.WithCancel(p.t.closedCtx)
}

// Iterates base and peer-impl specific ConnStats from all.
func (p *Peer) relevantConnStats(all *AllConnStats) iter.Seq[*ConnStats] {
	return func(yield func(*ConnStats) bool) {
		if yield(&all.ConnStats) {
			yield(p.peerImpl.allConnStatsImplField(all))
		}
	}
}
