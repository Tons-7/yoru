package bt

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/netip"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
	"weak"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/anacrolix/chansync"
	"github.com/anacrolix/generics"
	. "github.com/anacrolix/generics"
	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2/bitmap"
	"github.com/anacrolix/missinggo/v2/panicif"
	"github.com/anacrolix/multiless"

	"golang.org/x/time/rate"

	"yoru/internal/bt/bencode"
	requestStrategy "yoru/internal/bt/internal/request-strategy"

	"yoru/internal/bt/merkle"
	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/mse"
	pp "yoru/internal/bt/peer_protocol"
	utHolepunch "yoru/internal/bt/peer_protocol/ut-holepunch"
)

type PeerStatus struct {
	Id  PeerID
	Ok  bool
	Err string
}

const maxDhtPingsPerPeer = 3

type PeerConn struct {
	Peer

	holepunchConnects *rate.Limiter
	gaveMetadataSize  bool
	pingedDhtPort     int
	dhtPings          int

	metadataRequests   []bool
	sentHaves          bitmap.Bitmap
	validReceiveChunks map[RequestIndex]int
	PeerMaxRequests    maxRequests

	protocolLogger log.Logger

	v2 bool

	connString string

	PeerID             PeerID
	PeerExtensionBytes pp.PeerExtensionBits
	PeerListenPort     int

	LocalLtepProtocolMap *LocalLtepProtocolMap

	conn net.Conn
	w    io.Writer
	r    io.Reader

	messageWriter peerConnMsgWriter

	PeerExtensionIDs map[pp.ExtensionName]pp.ExtensionNumber
	PeerClientName   atomic.Value
	uploadTimer      *time.Timer
	pex              pexConnState

	_peerPieces     roaring.Bitmap
	peerSentHaveAll bool

	requestState requestStrategy.PeerRequestState

	outstandingHolepunchingRendezvous map[netip.AddrPort]struct{}

	sentHashRequests   map[hashRequest]struct{}
	receivedHashPieces map[[32]byte][][32]byte

	unreadPeerRequests            map[Request]uint64
	readyPeerRequests             map[Request]readyPeerRequest
	peerRequestArrivals           uint64
	unreadOrder, readyOrder       requestFifo
	peerRequestDataAllocDecreased chansync.BroadcastCond
	peerRequestServerRunning      bool

	reconciledHandshakeStats bool
}

func (*PeerConn) allConnStatsImplField(stats *AllConnStats) *ConnStats {
	return &stats.PeerConns
}

func (cn *PeerConn) lastWriteUploadRate() float64 {
	cn.messageWriter.mu.Lock()
	defer cn.messageWriter.mu.Unlock()
	return cn.messageWriter.dataUploadRate
}

func (cn *PeerConn) pexStatus() string {
	if !cn.bitExtensionEnabled(pp.ExtensionBitLtep) {
		return "extended protocol disabled"
	}
	if cn.PeerExtensionIDs == nil {
		return "pending extended handshake"
	}
	if !cn.supportsExtension(pp.ExtensionNamePex) {
		return "unsupported"
	}
	return fmt.Sprintf(
		"%v conns, %v unsent events",
		len(cn.pex.remoteLiveConns),
		cn.pex.numPending(),
	)
}

func (cn *PeerConn) peerImplStatusLines() []string {
	return []string{
		cn.connString,
		fmt.Sprintf("peer id: %+q", cn.PeerID),
		fmt.Sprintf("extensions: %v", cn.PeerExtensionBytes),
		fmt.Sprintf("ltep extensions: %v", cn.PeerExtensionIDs),
		fmt.Sprintf("pex: %s", cn.pexStatus()),
		fmt.Sprintf(
			"reqq: %d+%v/(%d/%d):%d/%d, flags: %s",
			cn.requestState.Requests.GetCardinality(),
			cn.requestState.Cancelled.GetCardinality(),
			cn.nominalMaxRequests(),
			cn.PeerMaxRequests,
			cn.numPeerRequests(),
			localClientReqq,
			cn.statusFlags(),
		),
	}
}

func (cn *PeerConn) ipv6() bool {
	ip := cn.remoteIp()
	if ip.To4() != nil {
		return false
	}
	return len(ip) == net.IPv6len
}

func (cn *PeerConn) isPreferredDirection() bool {
	return bytes.Compare(cn.PeerID[:], cn.t.cl.peerID[:]) < 0 == cn.outgoing
}

func (l *PeerConn) hasPreferredNetworkOver(r *PeerConn) bool {
	var ml multiless.Computation
	ml = ml.Bool(r.isPreferredDirection(), l.isPreferredDirection())
	ml = ml.Bool(l.utp(), r.utp())
	ml = ml.Bool(r.ipv6(), l.ipv6())
	return ml.Less()
}

func (cn *PeerConn) peerHasAllPieces() (all, known bool) {
	if cn.peerSentHaveAll {
		return true, true
	}
	if !cn.t.haveInfo() {
		return false, false
	}
	return cn._peerPieces.GetCardinality() == uint64(cn.t.numPieces()), true
}

func (cn *PeerConn) onGotInfo(info *metainfo.Info) {
	cn.setNumPieces(info.NumPieces())
}

func (cn *PeerConn) setNumPieces(num pieceIndex) {
	cn._peerPieces.RemoveRange(bitmap.BitRange(num), bitmap.ToEnd)
	cn.peerPiecesChanged()
}

func (cn *PeerConn) peerPieces() *roaring.Bitmap {
	return &cn._peerPieces
}

func (cn *PeerConn) connectionFlags() string {
	var sb strings.Builder
	add := func(s string) {
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(s)
	}
	add(string(cn.Discovery))
	if cn.utp() {
		add("U")
	}
	if cn.cryptoMethod == mse.CryptoMethodRC4 {
		add("E")
	} else if cn.headerEncrypted {
		add("e")
	}
	if cn.v2 {
		add("v2")
	} else {
		add("v1")
	}
	return sb.String()
}

func (cn *PeerConn) utp() bool {
	return parseNetworkString(cn.Network).Udp
}

func (cn *PeerConn) onClose() {
	if cn.pex.IsEnabled() {
		cn.pex.Close()
	}
	if cn.uploadTimer != nil {
		cn.uploadTimer.Stop()
	}
	cn.tickleWriter()
	if cn.conn != nil {
		go cn.conn.Close()
	}
	if cb := cn.callbacks.PeerConnClosed; cb != nil {
		cb(cn)
	}
}

func (cn *PeerConn) write(msg pp.Message) bool {
	notFull := cn.messageWriter.write(msg)
	cn.wroteMsg(&msg)
	cn.tickleWriter()
	return notFull
}

func (cn *PeerConn) requestMetadataPiece(index int) {
	eID := cn.PeerExtensionIDs[pp.ExtensionNameMetadata]
	if eID == pp.ExtensionDeleteNumber {
		return
	}
	if index < len(cn.metadataRequests) && cn.metadataRequests[index] {
		return
	}
	cn.protocolLogger.WithDefaultLevel(log.Debug).Printf("requesting metadata piece %d", index)
	cn.write(pp.MetadataExtensionRequestMsg(eID, index))
	for index >= len(cn.metadataRequests) {
		cn.metadataRequests = append(cn.metadataRequests, false)
	}
	cn.metadataRequests[index] = true
}

func (cn *PeerConn) requestedMetadataPiece(index int) bool {
	return index >= 0 && index < len(cn.metadataRequests) && cn.metadataRequests[index]
}

func (cn *PeerConn) onPeerSentCancel(r Request) {
	if !cn.havePeerRequest(r) {
		return
	}
	if cn.fastEnabled() {
		cn.reject(r)
	} else {
		cn.deletePeerRequest(r)
	}
}

func (me *PeerConn) deletePeerRequest(r Request) {
	delete(me.unreadPeerRequests, r)
	me.deleteReadyPeerRequest(r)
}

func (me *PeerConn) havePeerRequest(r Request) bool {
	return MapContains(me.unreadPeerRequests, r) || MapContains(me.readyPeerRequests, r)
}

func (cn *PeerConn) choke(msg messageWriter) (more bool) {
	if cn.choking {
		return true
	}
	cn.choking = true
	more = msg(pp.Message{
		Type: pp.Choke,
	})
	if cn.fastEnabled() {
		for r := range cn.unreadPeerRequests {
			more = msg(r.ToMsg(pp.Reject)) && more
		}
		for r := range cn.readyPeerRequests {
			more = msg(r.ToMsg(pp.Reject)) && more
		}
	}
	cn.deleteAllPeerRequests()
	return
}

func (cn *PeerConn) deleteAllPeerRequests() {
	clear(cn.unreadPeerRequests)
	clear(cn.readyPeerRequests)
	cn.unreadOrder, cn.readyOrder = requestFifo{}, requestFifo{}
	cn.peerRequestDataAllocDecreased.Broadcast()
}

func (cn *PeerConn) unchoke(msg func(pp.Message) bool) bool {
	if !cn.choking {
		return true
	}
	cn.choking = false
	return msg(pp.Message{
		Type: pp.Unchoke,
	})
}

func (pc *PeerConn) writeInterested(interested bool) bool {
	return pc.write(pp.Message{
		Type: func() pp.MessageType {
			if interested {
				return pp.Interested
			} else {
				return pp.NotInterested
			}
		}(),
	})
}

func (me *PeerConn) _request(r Request) bool {
	return me.write(pp.Message{
		Type:   pp.Request,
		Index:  r.Index,
		Begin:  r.Begin,
		Length: r.Length,
	})
}

func (me *PeerConn) handleCancel(r RequestIndex) {
	me.write(makeCancelMessage(me.t.requestIndexToRequest(r)))
	if me.remoteRejectsCancels() {
		if !me.requestState.Cancelled.CheckedAdd(r) {
			panic("request already cancelled")
		}
	}
}

func (me *PeerConn) remoteRejectsCancels() bool {
	if !me.fastEnabled() {
		return false
	}
	if me.remoteIsTransmission() {
		return me.PeerID[3] >= '4'
	}
	return true
}

func (cn *PeerConn) fillWriteBuffer() {
	if cn.messageWriter.writeBuffer.Len() > writeBufferLowWaterLen {
		return
	}
	cn.requestMissingHashes()
	cn.maybeUpdateActualRequestState()
	if cn.pex.IsEnabled() {
		if flow := cn.pex.Share(cn.write); !flow {
			return
		}
	}
	cn.upload(cn.write)
}

func (cn *PeerConn) have(piece pieceIndex) {
	if cn.sentHaves.Get(bitmap.BitIndex(piece)) {
		return
	}
	cn.write(pp.Message{
		Type:  pp.Have,
		Index: pp.Integer(piece),
	})
	cn.sentHaves.Add(bitmap.BitIndex(piece))
}

func (cn *PeerConn) postBitfield() {
	if cn.sentHaves.Len() != 0 {
		panic("bitfield must be first have-related message sent")
	}
	if !cn.t.haveAnyPieces() {
		return
	}
	cn.write(pp.Message{
		Type:     pp.Bitfield,
		Bitfield: cn.t.bitfield(),
	})
	cn.sentHaves = bitmap.Bitmap{RB: cn.t._completedPieces.Clone()}
}

func (cn *PeerConn) handleOnNeedUpdateRequests() {
	cn.tickleWriter()
}

func (cn *PeerConn) raisePeerMinPieces(newMin pieceIndex) {
	if newMin > cn.peerMinPieces {
		cn.peerMinPieces = newMin
	}
}

func (cn *PeerConn) peerSentHave(piece pieceIndex) error {
	if cn.t.haveInfo() && piece >= cn.t.numPieces() || piece < 0 {
		return errors.New("invalid piece")
	}
	if cn.peerHasPiece(piece) {
		return nil
	}
	cn.raisePeerMinPieces(piece + 1)
	if !cn.peerHasPiece(piece) {
		cn.t.incPieceAvailability(piece)
	}
	cn._peerPieces.Add(uint32(piece))
	if cn.t.wantPieceIndex(piece) {
		cn.onNeedUpdateRequests("have")
	}
	cn.peerPiecesChanged()
	return nil
}

func (cn *PeerConn) peerSentBitfield(bf []bool) error {
	if len(bf)%8 != 0 {
		panic("expected bitfield length divisible by 8")
	}
	cn.raisePeerMinPieces(pieceIndex(len(bf) - 7))
	if cn.t.haveInfo() && len(bf) > int(cn.t.numPieces()) {
		bf = bf[:cn.t.numPieces()]
	}
	bm := boolSliceToBitmap(bf)
	if cn.t.haveInfo() && pieceIndex(bm.GetCardinality()) == cn.t.numPieces() {
		cn.onPeerHasAllPieces()
		return nil
	}
	if !bm.IsEmpty() {
		cn.raisePeerMinPieces(pieceIndex(bm.Maximum()) + 1)
	}
	shouldUpdateRequests := false
	if cn.peerSentHaveAll {
		if !cn.t.deleteConnWithAllPieces(&cn.Peer) {
			panic(cn)
		}
		cn.peerSentHaveAll = false
		if !cn._peerPieces.IsEmpty() {
			panic("if peer has all, we expect no individual peer pieces to be set")
		}
	} else {
		bm.Xor(&cn._peerPieces)
	}
	cn.peerSentHaveAll = false
	bm.Iterate(func(x uint32) bool {
		pi := pieceIndex(x)
		if cn._peerPieces.Contains(x) {
			cn.t.decPieceAvailability(pi)
		} else {
			if !shouldUpdateRequests && cn.t.wantPieceIndex(pieceIndex(x)) {
				shouldUpdateRequests = true
			}
			cn.t.incPieceAvailability(pieceIndex(x))
		}
		return true
	})
	cn._peerPieces.Xor(&bm)
	if shouldUpdateRequests {
		cn.onNeedUpdateRequests("bitfield")
	}
	cn.peerPiecesChanged()
	return nil
}

func (cn *PeerConn) onPeerHasAllPiecesNoTriggers() {
	t := cn.t
	if t.haveInfo() {
		cn._peerPieces.Iterate(func(x uint32) bool {
			t.decPieceAvailability(pieceIndex(x))
			return true
		})
	}
	t.addConnWithAllPieces(&cn.Peer)
	cn.peerSentHaveAll = true
	cn._peerPieces.Clear()
}

func (cn *PeerConn) onPeerHasAllPieces() {
	cn.onPeerHasAllPiecesNoTriggers()
	cn.peerHasAllPiecesTriggers()
}

func (cn *PeerConn) peerHasAllPiecesTriggers() {
	if !cn.t._pendingPieces.IsEmpty() {
		cn.onNeedUpdateRequests("Peer.onPeerHasAllPieces")
	}
	cn.peerPiecesChanged()
}

func (cn *PeerConn) onPeerSentHaveAll() error {
	cn.onPeerHasAllPieces()
	return nil
}

func (cn *PeerConn) peerSentHaveNone() error {
	if cn.peerSentHaveAll {
		cn.t.deleteConnWithAllPieces(&cn.Peer)
	} else {
		cn.t.decPeerPieceAvailability(&cn.Peer)
	}
	cn._peerPieces.Clear()
	cn.peerSentHaveAll = false
	cn.peerPiecesChanged()
	return nil
}

func (c *PeerConn) requestPendingMetadata() {
	if c.t.haveInfo() {
		return
	}
	if c.PeerExtensionIDs[pp.ExtensionNameMetadata] == 0 {
		return
	}
	var pending []int
	for index := 0; index < c.t.metadataPieceCount(); index++ {
		if !c.t.haveMetadataPiece(index) && !c.requestedMetadataPiece(index) {
			pending = append(pending, index)
		}
	}
	rand.Shuffle(len(pending), func(i, j int) { pending[i], pending[j] = pending[j], pending[i] })
	for _, i := range pending {
		c.requestMetadataPiece(i)
	}
}

func (cn *PeerConn) wroteMsg(msg *pp.Message) {
	cn.modifyRelevantConnStats(func(cs *ConnStats) { cs.wroteMsg(msg) })
}

func (cn *PeerConn) wroteBytes(n int64) {
	cn.modifyRelevantConnStats(add(n, func(cs *ConnStats) *Count { return &cs.BytesWritten }))
}

func (c *PeerConn) fastEnabled() bool {
	return c.PeerExtensionBytes.SupportsFast() && c.t.cl.config.Extensions.SupportsFast()
}

func (c *PeerConn) reject(r Request) {
	if !c.fastEnabled() {
		panic("fast not enabled")
	}
	c.write(r.ToMsg(pp.Reject))
	c.deletePeerRequest(r)
}

func (c *PeerConn) maximumPeerRequestChunkLength() (_ Option[int]) {
	uploadRateLimiter := c.t.cl.config.UploadRateLimiter
	if uploadRateLimiter.Limit() == rate.Inf {
		return
	}
	return Some(uploadRateLimiter.Burst())
}

func (me *PeerConn) numPeerRequests() int {
	return len(me.unreadPeerRequests) + len(me.readyPeerRequests)
}

func (c *PeerConn) onReadRequest(r Request, startFetch bool) error {
	if c.havePeerRequest(r) {
		if c.fastEnabled() {
			return errors.New("received duplicate request with fast enabled")
		}
		return nil
	}
	if c.choking {
		if c.fastEnabled() {
			c.reject(r)
		}
		return nil
	}
	if c.numPeerRequests() >= localClientReqq {
		if c.fastEnabled() {
			c.reject(r)
		}
		return nil
	}
	if opt := c.maximumPeerRequestChunkLength(); opt.Ok && int(r.Length) > opt.Value {
		err := fmt.Errorf("peer requested chunk too long (%v)", r.Length)
		c.protocolLogger.Levelf(log.Debug, "%v", err)
		if c.fastEnabled() {
			c.reject(r)
			return nil
		}
		return err
	}
	if !c.t.havePiece(pieceIndex(r.Index)) {
		return fmt.Errorf("peer requested piece we don't have: %v", r.Index.Int())
	}
	pieceLength := c.t.pieceLength(pieceIndex(r.Index))
	if chunkOverflowsPiece(r.ChunkSpec, pieceLength) {
		return errors.New("chunk overflows piece")
	}
	MakeMapIfNilWithCap(&c.unreadPeerRequests, localClientReqq)
	c.peerRequestArrivals++
	c.unreadPeerRequests[r] = c.peerRequestArrivals
	c.unreadOrder.push(r, c.peerRequestArrivals, len(c.unreadPeerRequests), c.unreadAlive)
	if startFetch {
		c.startPeerRequestServer()
	}
	return nil
}

func (c *PeerConn) startPeerRequestServer() {
	if !c.peerRequestServerRunning {
		go c.peerRequestServer()
		c.peerRequestServerRunning = true
	}
}

func (c *PeerConn) peerRequestServer() {
	c.locker().Lock()
again:
	if !c.closed.IsSet() {
		if r, ok := c.oldestUnreadPeerRequest(); ok {
			c.servePeerRequest(r)
			goto again
		}
	}
	panicif.False(c.peerRequestServerRunning)
	c.peerRequestServerRunning = false
	c.locker().Unlock()
}

func (c *PeerConn) peerRequestDataBuffered() (n int) {
	for r := range c.readyPeerRequests {
		n += r.Length.Int()
	}
	return
}

func (c *PeerConn) waitForDataAlloc(size int) bool {
	maxAlloc := c.t.cl.config.MaxAllocPeerRequestDataPerConn
	locker := c.locker()
	for {
		if size > maxAlloc {
			c.slogger.Warn("peer request length exceeds MaxAllocPeerRequestDataPerConn",
				"requested", size,
				"max", maxAlloc)
			return false
		}
		if c.peerRequestDataBuffered()+size <= maxAlloc {
			return true
		}
		allocDecreased := c.peerRequestDataAllocDecreased.Signaled()
		locker.Unlock()
		select {
		case <-c.closedCtx.Done():
			locker.Lock()
			return false
		case <-allocDecreased:
		}
		c.locker().Lock()
	}
}

func (me *PeerConn) deleteReadyPeerRequest(r Request) {
	v, ok := me.readyPeerRequests[r]
	if !ok {
		return
	}
	delete(me.readyPeerRequests, r)
	if len(v.data) > 0 {
		me.peerRequestDataAllocDecreased.Broadcast()
	}
}

func (c *PeerConn) servePeerRequest(r Request) {
	defer func() {
		panicif.True(MapContains(c.unreadPeerRequests, r))
	}()
	if !c.waitForDataAlloc(r.Length.Int()) {
		if MapContains(c.unreadPeerRequests, r) {
			c.useBestReject(r)
		}
		return
	}
	c.locker().Unlock()
	b, err := c.readPeerRequestData(r)
	c.locker().Lock()
	if err != nil {
		if MapContains(c.unreadPeerRequests, r) {
			c.peerRequestDataReadFailed(err, r)
		}
		return
	}
	arrival, ok := c.unreadPeerRequests[r]
	if !ok {
		c.slogger.Debug("read data for peer request but no longer wanted", "request", r)
		return
	}
	delete(c.unreadPeerRequests, r)
	c.setReadyPeerRequest(r, readyPeerRequest{arrival: arrival, data: b})
	c.tickleWriter()
}

type readyPeerRequest struct {
	arrival uint64
	data    []byte
}

func (c *PeerConn) setReadyPeerRequest(r Request, v readyPeerRequest) {
	MakeMapIfNil(&c.readyPeerRequests)
	c.readyPeerRequests[r] = v
	c.readyOrder.push(r, v.arrival, len(c.readyPeerRequests), c.readyAlive)
}

func (c *PeerConn) unreadAlive(r Request, arrival uint64) bool {
	a, ok := c.unreadPeerRequests[r]
	return ok && a == arrival
}

func (c *PeerConn) readyAlive(r Request, arrival uint64) bool {
	v, ok := c.readyPeerRequests[r]
	return ok && v.arrival == arrival
}

func (c *PeerConn) oldestUnreadPeerRequest() (Request, bool) {
	return c.unreadOrder.front(c.unreadAlive)
}

func (c *PeerConn) oldestReadyPeerRequest() (Request, bool) { return c.readyOrder.front(c.readyAlive) }

type requestFifo struct {
	q []requestArrival
}

type requestArrival struct {
	r       Request
	arrival uint64
}

func (f *requestFifo) push(r Request, arrival uint64, live int, alive func(Request, uint64) bool) {
	if len(f.q) > 2*live+64 {
		kept := f.q[:0]
		for _, e := range f.q {
			if alive(e.r, e.arrival) {
				kept = append(kept, e)
			}
		}
		clear(f.q[len(kept):])
		f.q = kept
	}
	f.q = append(f.q, requestArrival{r, arrival})
}

func (f *requestFifo) front(live func(Request, uint64) bool) (Request, bool) {
	for len(f.q) > 0 {
		if e := f.q[0]; live(e.r, e.arrival) {
			return e.r, true
		}
		f.q = f.q[1:]
	}
	f.q = nil
	return Request{}, false
}

func (c *PeerConn) peerRequestDataReadFailed(err error, r Request) {
	logLevel := log.Warning
	if c.t.hasStorageCap() || c.t.closed.IsSet() {
		logLevel = log.Debug
	}
	c.logger.Levelf(logLevel, "error reading chunk for peer Request %v: %v", r, err)
	if c.t.closed.IsSet() {
		return
	}
	i := pieceIndex(r.Index)
	if c.t.pieceComplete(i) {
		c.t.updatePieceCompletion(i)
	}
	c.useBestReject(r)
}

func (c *PeerConn) useBestReject(r Request) {
	if c.fastEnabled() {
		c.reject(r)
	} else {
		if c.choking {
			c.protocolLogger.WithDefaultLevel(log.Warning).Printf("already choking peer, requests might not be rejected correctly")
		}
		c.choke(c.write)
	}
}

func (c *PeerConn) readPeerRequestData(r Request) ([]byte, error) {
	b := make([]byte, r.Length)
	p := c.t.info.Piece(int(r.Index))
	n, err := c.t.readAt(b, p.Offset()+int64(r.Begin))
	if n == len(b) {
		if errors.Is(err, io.EOF) {
			err = nil
		}
	} else {
		if err == nil {
			panic("expected error")
		}
	}
	return b, err
}

func (c *PeerConn) logProtocolBehaviour(level log.Level, format string, arg ...any) {
	c.protocolLogger.WithContextText(fmt.Sprintf(
		"peer id %q, ext v %q", c.PeerID, c.PeerClientName.Load(),
	)).SkipCallers(1).Levelf(level, format, arg...)
}

func (c *PeerConn) mainReadLoop() (err error) {
	defer func() {
		if err != nil {
		} else {
		}
	}()
	defer func() {
		if r := recover(); r != nil {
			c.slogger.Error("peer message handler panicked; dropping the peer", "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("handling peer message: %v", r)
		}
	}()
	t := c.t
	cl := t.cl

	baseMaxLength := 4 * pp.Integer(max(int64(t.chunkSize), defaultChunkSize))
	decoder := pp.Decoder{
		R:         bufio.NewReaderSize(c.r, 1<<17),
		MaxLength: baseMaxLength,
		Pool:      &t.chunkPool,
	}
	for {
		var msg pp.Message
		if t.haveInfo() {
			decoder.MaxLength = max(baseMaxLength, pp.Integer(1+(t.numPieces()+7)/8))
		}
		func() {
			defer cl.lock()
			cl.unlock()
			err = decoder.Decode(&msg)
			if err != nil {
				err = fmt.Errorf("decoding message: %w", err)
			}
		}()
		if cb := c.callbacks.ReadMessage; cb != nil && err == nil {
			cb(c, &msg)
		}
		if t.closed.IsSet() || c.closed.IsSet() {
			return nil
		}
		if err != nil {
			return err
		}
		c.lastMessageReceived = time.Now()
		if msg.Keepalive {
			continue
		}
		if msg.Type.FastExtension() && !c.fastEnabled() {
			return fmt.Errorf("received fast extension message (type=%v) but extension is disabled", msg.Type)
		}
		switch msg.Type {
		case pp.Choke:
			if c.peerChoking {
				break
			}
			if !c.fastEnabled() {
				c.deleteAllRequests("choked by non-fast PeerConn")
			} else {
			}
			c.peerChoking = true
			c.updateExpectingChunks()
		case pp.Unchoke:
			if !c.peerChoking {
				c.logProtocolBehaviour(log.Debug, "received unchoke when already unchoked")
				break
			}
			c.peerChoking = false
			preservedCount := 0
			c.requestState.Requests.Iterate(func(x RequestIndex) bool {
				if !c.peerAllowedFast.Contains(c.t.pieceIndexOfRequestIndex(x)) {
					preservedCount++
				}
				return true
			})
			if preservedCount != 0 {
				c.protocolLogger.Levelf(log.Debug,
					"%v requests were preserved while being choked (fast=%v)",
					preservedCount,
					c.fastEnabled())

			}
			if !c.t._pendingPieces.IsEmpty() {
				c.onNeedUpdateRequests("unchoked")
			}
			c.updateExpectingChunks()
		case pp.Interested:
			c.peerInterested = true
			c.tickleWriter()
		case pp.NotInterested:
			c.peerInterested = false
		case pp.Have:
			err = c.peerSentHave(pieceIndex(msg.Index))
		case pp.Bitfield:
			err = c.peerSentBitfield(msg.Bitfield)
		case pp.Request:
			r := newRequestFromMessage(&msg)
			err = c.onReadRequest(r, true)
			if err != nil {
				err = fmt.Errorf("on reading request %v: %w", r, err)
			}
		case pp.Piece:
			c.doChunkReadStats(int64(len(msg.Piece)))
			err = c.receiveChunk(&msg)
			t.putChunkBuffer(msg.Piece)
			msg.Piece = nil
			if err != nil {
				err = fmt.Errorf("receiving chunk: %w", err)
			}
		case pp.Cancel:
			req := newRequestFromMessage(&msg)
			c.onPeerSentCancel(req)
		case pp.Port:
			ipa, ok := tryIpPortFromNetAddr(c.RemoteAddr)
			if !ok {
				break
			}
			pingAddr := net.UDPAddr{
				IP:   ipa.IP,
				Port: ipa.Port,
			}
			if msg.Port != 0 {
				pingAddr.Port = int(msg.Port)
			}
			if pingAddr.Port == c.pingedDhtPort || c.dhtPings >= maxDhtPingsPerPeer {
				break
			}
			c.pingedDhtPort = pingAddr.Port
			c.dhtPings++
			cl.eachDhtServer(func(s DhtServer) {
				go s.Ping(&pingAddr)
			})
		case pp.Suggest:
			log.Fmsg("peer suggested piece %d", msg.Index).AddValues(c, msg.Index).LogLevel(log.Debug, c.t.logger)
			c.onNeedUpdateRequests("suggested")
		case pp.HaveAll:
			err = c.onPeerSentHaveAll()
		case pp.HaveNone:
			err = c.peerSentHaveNone()
		case pp.Reject:
			req := newRequestFromMessage(&msg)
			if !c.remoteRejectedRequest(c.t.requestIndexFromRequest(req)) {
				err = fmt.Errorf("received invalid reject for request %v", req)
				c.protocolLogger.Levelf(log.Debug, "%v", err)
			}
		case pp.AllowedFast:
			log.Fmsg("peer allowed fast: %d", msg.Index).AddValues(c).LogLevel(log.Debug, c.t.logger)
			c.onNeedUpdateRequests("PeerConn.mainReadLoop allowed fast")
		case pp.Extended:
			err = c.onReadExtendedMsg(msg.ExtendedID, msg.ExtendedPayload)
		case pp.Hashes:
			err = c.onReadHashes(&msg)
		case pp.HashRequest:
			err = c.onHashRequest(&msg)
		case pp.HashReject:
			c.protocolLogger.Levelf(log.Debug, "received unimplemented BitTorrent v2 message: %v", msg.Type)
		default:
			err = fmt.Errorf("received unknown message type: %#v", msg.Type)
		}
		if err != nil {
			return err
		}
	}
}

func (c *PeerConn) onReadExtendedMsg(id pp.ExtensionNumber, payload []byte) (err error) {
	defer func() {
		if err != nil {
			if bytes.HasPrefix(c.PeerID[:], []byte("-SD0100-")) || strings.HasPrefix(string(c.PeerID[:]), "-XL0012-") {
				err = nil
			}
		}
	}()
	t := c.t
	cl := t.cl
	{
		event := PeerConnReadExtensionMessageEvent{
			PeerConn:        c,
			ExtensionNumber: id,
			Payload:         payload,
		}
		for _, cb := range c.callbacks.PeerConnReadExtensionMessage {
			cb(event)
		}
	}
	if id == pp.HandshakeExtendedID {
		var d pp.ExtendedHandshakeMessage
		if err := bencode.Unmarshal(payload, &d); err != nil {
			return fmt.Errorf("unmarshalling extended handshake payload of %d bytes: %w", len(payload), err)
		}
		if cb := c.callbacks.ReadExtendedHandshake; cb != nil {
			cb(c, &d)
		}
		if d.Reqq != 0 {
			c.PeerMaxRequests = d.Reqq
		}
		c.PeerClientName.Store(d.V)
		if c.PeerExtensionIDs == nil {
			c.PeerExtensionIDs = make(map[pp.ExtensionName]pp.ExtensionNumber, len(d.M))
		}
		c.PeerListenPort = d.Port
		c.PeerPrefersEncryption = d.Encryption
		for name, id := range d.M {
			c.PeerExtensionIDs[name] = id
		}
		if d.MetadataSize != 0 && !c.gaveMetadataSize {
			c.gaveMetadataSize = true
			if err = t.setMetadataSize(d.MetadataSize); err != nil {
				return fmt.Errorf("setting metadata size to %d: %w", d.MetadataSize, err)
			}
		}
		c.requestPendingMetadata()
		if !t.cl.config.DisablePEX && !c.pex.handshaken {
			c.pex.handshaken = true
			t.pex.Add(c)
			c.pex.Init(c)
		}
		return nil
	}
	extensionName, builtin, err := c.LocalLtepProtocolMap.LookupId(id)
	if err != nil {
		return
	}
	if !builtin {
		return nil
	}
	switch extensionName {
	case pp.ExtensionNameMetadata:
		err := cl.gotMetadataExtensionMsg(payload, t, c)
		if err != nil {
			return fmt.Errorf("handling metadata extension message: %w", err)
		}
		return nil
	case pp.ExtensionNamePex:
		if !c.pex.IsEnabled() {
			return nil
		}
		err = c.pex.Recv(payload)
		if err != nil {
			err = fmt.Errorf("receiving pex message: %w", err)
		}
		return
	case utHolepunch.ExtensionName:
		if !c.supportsExtension(utHolepunch.ExtensionName) {
			return errors.New("ut_holepunch from a peer that never advertised it")
		}
		var msg utHolepunch.Msg
		err = msg.UnmarshalBinary(payload)
		if err != nil {
			err = fmt.Errorf("unmarshalling ut_holepunch message: %w", err)
			return
		}
		err = c.t.handleReceivedUtHolepunchMsg(msg, c)
		return
	default:
		panic(fmt.Sprintf("unhandled builtin extension protocol %q", extensionName))
	}
}

func (cn *PeerConn) setRW(rw io.ReadWriter) {
	cn.r = rw
	cn.w = rw
}

func (cn *PeerConn) rw() io.ReadWriter {
	return struct {
		io.Reader
		io.Writer
	}{cn.r, cn.w}
}

func (c *PeerConn) uploadAllowed() bool {
	if c.t.cl.config.NoUpload {
		return false
	}
	if c.t.dataUploadDisallowed {
		return false
	}
	if c.t.seeding() {
		return true
	}
	if !c.peerHasWantedPieces() {
		return false
	}
	if c._stats.BytesWrittenData.Int64() >= c._stats.BytesReadData.Int64()+100<<10 {
		return false
	}
	return true
}

func (c *PeerConn) setRetryUploadTimer(delay time.Duration) {
	if c.uploadTimer == nil {
		c.uploadTimer = time.AfterFunc(delay, c.tickleWriter)
	} else {
		c.uploadTimer.Reset(delay)
	}
}

func (c *PeerConn) upload(msg func(pp.Message) bool) bool {
another:
	for c.uploadAllowed() {
		if !c.unchoke(msg) {
			return false
		}
		if r, ok := c.oldestReadyPeerRequest(); ok {
			res := c.t.cl.config.UploadRateLimiter.ReserveN(time.Now(), int(r.Length))
			if !res.OK() {
				panic(fmt.Sprintf("upload rate limiter burst size < %d", r.Length))
			}
			delay := res.Delay()
			if delay > 0 {
				res.Cancel()
				c.setRetryUploadTimer(delay)
				return true
			}
			more := c.sendChunk(r, msg)
			if !more {
				return false
			}
			goto another
		}
		return true
	}
	return c.choke(msg)
}

func (cn *PeerConn) drop() {
	cn.t.dropConnection(cn)
}

func (cn *PeerConn) providedBadData() {
	cn.t.cl.banPeerIP(cn.remoteIp())
}

func (c *PeerConn) tickleWriter() {
	c.messageWriter.writeCond.Broadcast()
}

func (c *PeerConn) sendChunk(r Request, msg func(pp.Message) bool) (more bool) {
	b := MapMustGet(c.readyPeerRequests, r).data
	panicif.NotEq(len(b), r.Length.Int())
	c.deleteReadyPeerRequest(r)
	c.lastChunkSent = time.Now()
	return msg(pp.Message{
		Type:  pp.Piece,
		Index: r.Index,
		Begin: r.Begin,
		Piece: b,
	})
}

func (c *PeerConn) setTorrent(t *Torrent) {
	panicif.NotNil(c.t)
	c.t = t
	c.initClosedCtx()
	c.setPeerLoggers(t.logger, t.slogger())
	c.reconcileHandshakeStats()
}

func (c *PeerConn) pexPeerFlags() pp.PexPeerFlags {
	f := pp.PexPeerFlags(0)
	if c.PeerPrefersEncryption {
		f |= pp.PexPrefersEncryption
	}
	if c.outgoing {
		f |= pp.PexOutgoingConn
	}
	if c.utp() {
		f |= pp.PexSupportsUtp
	}
	return f
}

func (c *PeerConn) LocalAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

func (c *PeerConn) dialAddr() PeerRemoteAddr {
	if c.outgoing || c.PeerListenPort <= 0 || c.PeerListenPort > 65535 {
		return c.RemoteAddr
	}
	addrPort, err := addrPortFromPeerRemoteAddr(c.RemoteAddr)
	if err != nil {
		c.logger.Levelf(
			log.Warning,
			"error parsing %q for alternate dial port: %v",
			c.RemoteAddr,
			err,
		)
		return c.RemoteAddr
	}
	return netip.AddrPortFrom(addrPort.Addr(), uint16(c.PeerListenPort))
}

func (c *PeerConn) pexEvent(t pexEventType) (_ pexEvent, err error) {
	f := c.pexPeerFlags()
	dialAddr := c.dialAddr()
	addr, err := addrPortFromPeerRemoteAddr(dialAddr)
	if err != nil || !addr.IsValid() {
		err = fmt.Errorf("parsing dial addr %q: %w", dialAddr, err)
		return
	}
	return pexEvent{t, addr, f, nil}, nil
}

func (pc *PeerConn) String() string {
	return fmt.Sprintf(
		"%T %p [flags=%v id=%+q, exts=%v, v=%q]",
		pc,
		pc,
		pc.connectionFlags(),
		pc.PeerID,
		pc.PeerExtensionBytes,
		pc.PeerClientName.Load(),
	)
}

func (pc *PeerConn) PeerPieces() *roaring.Bitmap {
	pc.locker().RLock()
	defer pc.locker().RUnlock()
	return pc.newPeerPieces()
}

func (pc *PeerConn) remoteIsTransmission() bool {
	return bytes.HasPrefix(pc.PeerID[:], []byte("-TR")) && pc.PeerID[7] == '-'
}

func (pc *PeerConn) allowHolepunchConnect() bool {
	return pc.holepunchConnects == nil || pc.holepunchConnects.Allow()
}

func (pc *PeerConn) remoteDialAddrPort() (netip.AddrPort, error) {
	dialAddr := pc.dialAddr()
	return addrPortFromPeerRemoteAddr(dialAddr)
}

func (pc *PeerConn) bitExtensionEnabled(bit pp.ExtensionBit) bool {
	return pc.t.cl.config.Extensions.GetBit(bit) && pc.PeerExtensionBytes.GetBit(bit)
}

func (cn *PeerConn) peerPiecesChanged() {
	cn.t.maybeDropMutuallyCompletePeer(cn)
}

func (c *PeerConn) useful() bool {
	t := c.t
	if c.closed.IsSet() {
		return false
	}
	if !t.haveInfo() {
		return c.supportsExtension("ut_metadata")
	}
	if t.seeding() && c.peerInterested {
		return true
	}
	if c.peerHasWantedPieces() {
		return true
	}
	return false
}

func makeBuiltinLtepProtocols(pex bool) LocalLtepProtocolMap {
	ps := []pp.ExtensionName{pp.ExtensionNameMetadata, utHolepunch.ExtensionName}
	if pex {
		ps = append(ps, pp.ExtensionNamePex)
	}
	return LocalLtepProtocolMap{
		Index:      ps,
		NumBuiltin: len(ps),
	}
}

func (c *PeerConn) addBuiltinLtepProtocols() {
	c.LocalLtepProtocolMap = &c.t.cl.defaultLocalLtepProtocolMap
}

func (pc *PeerConn) WriteExtendedMessage(extName pp.ExtensionName, payload []byte) error {
	pc.locker().Lock()
	defer pc.locker().Unlock()
	id := pc.PeerExtensionIDs[extName]
	if id == 0 {
		return fmt.Errorf("peer does not support or has disabled extension %q", extName)
	}
	pc.write(pp.Message{
		Type:            pp.Extended,
		ExtendedID:      id,
		ExtendedPayload: payload,
	})
	return nil
}

func (pc *PeerConn) shouldRequestHashes() bool {
	return pc.t.haveInfo() && pc.v2 && pc.t.info.HasV2()
}

func (pc *PeerConn) requestMissingHashes() {
	if !pc.shouldRequestHashes() {
		return
	}
	info := pc.t.info
	baseLayer := pp.Integer(merkle.Log2RoundingUp(merkle.RoundUpToPowerOfTwo(
		uint((pc.t.usualPieceSize() + merkle.BlockSize - 1) / merkle.BlockSize)),
	))
	nextFileBeginPiece := 0
file:
	for _, file := range info.UpvertedFiles() {
		fileNumPieces := int((file.Length + info.PieceLength - 1) / info.PieceLength)
		if fileNumPieces <= 1 {
			continue
		}
		curFileBeginPiece := nextFileBeginPiece
		nextFileBeginPiece += fileNumPieces
		haveAllHashes := true
		for i := range fileNumPieces {
			torrentPieceIndex := curFileBeginPiece + i
			if !pc.peerHasPiece(torrentPieceIndex) {
				continue file
			}
			if !pc.t.piece(torrentPieceIndex).hashV2.Ok {
				haveAllHashes = false
			}
		}
		if haveAllHashes {
			continue
		}
		piecesRoot := file.PiecesRoot.Unwrap()
		proofLayers := pp.Integer(0)
		for index := 0; index < fileNumPieces; index += 512 {
			length := merkle.RoundUpToPowerOfTwo(uint(min(512, fileNumPieces-index)))
			if length < 2 {
				panic(length)
			}
			if length%2 != 0 {
				pc.protocolLogger.Levelf(log.Warning, "requesting odd hashes length %d", length)
			}
			msg := pp.Message{
				Type:        pp.HashRequest,
				PiecesRoot:  piecesRoot,
				BaseLayer:   baseLayer,
				Index:       pp.Integer(index),
				Length:      pp.Integer(length),
				ProofLayers: proofLayers,
			}
			hr := hashRequestFromMessage(msg)
			if generics.MapContains(pc.sentHashRequests, hr) {
				continue
			}
			pc.write(msg)
			generics.MakeMapIfNil(&pc.sentHashRequests)
			pc.sentHashRequests[hr] = struct{}{}
		}
	}
}

func (pc *PeerConn) onReadHashes(msg *pp.Message) (err error) {
	if !pc.t.haveInfo() || !pc.t.info.HasV2() {
		return errors.New("hashes for a torrent without v2 metadata")
	}
	if !generics.MapContains(pc.sentHashRequests, hashRequestFromMessage(*msg)) {
		return errors.New("hashes we did not request")
	}
	if msg.ProofLayers != 0 {
		return errors.New("proof layers not supported")
	}
	file := pc.t.getFileByPiecesRoot(msg.PiecesRoot)
	if file == nil {
		return fmt.Errorf("no file for pieces root %x", msg.PiecesRoot)
	}
	filePieceHashes := pc.receivedHashPieces[msg.PiecesRoot]
	if filePieceHashes == nil {
		filePieceHashes = make([][32]byte, file.numPieces())
		generics.MakeMapIfNil(&pc.receivedHashPieces)
		pc.receivedHashPieces[msg.PiecesRoot] = filePieceHashes
	}
	if int(msg.Index) > len(filePieceHashes) {
		return fmt.Errorf("hashes index %d past %d pieces", msg.Index, len(filePieceHashes))
	}
	copy(filePieceHashes[msg.Index:], msg.Hashes)
	root := merkle.RootWithPadHash(
		filePieceHashes,
		metainfo.HashForPiecePad(int64(pc.t.usualPieceSize())))
	expectedPiecesRoot := file.piecesRoot.Unwrap()
	if root == expectedPiecesRoot {
		pc.protocolLogger.WithNames(v2HashesLogName).Levelf(
			log.Info,
			"got piece hashes for file %v (num pieces %v)",
			file, file.numPieces())
		for filePieceIndex, peerHash := range filePieceHashes {
			torrentPieceIndex := file.BeginPieceIndex() + filePieceIndex
			pc.t.piece(torrentPieceIndex).setV2Hash(peerHash)
		}
	} else {
		pc.protocolLogger.WithNames(v2HashesLogName).Levelf(
			log.Debug,
			"peer file piece hashes root mismatch: %x != %x",
			root, expectedPiecesRoot)
	}
	return nil
}

func (pc *PeerConn) getHashes(msg *pp.Message) ([][32]byte, error) {
	if msg.ProofLayers != 0 {
		return nil, errors.New("proof layers not supported")
	}
	if msg.Length == 0 || msg.Length > 512 {
		return nil, fmt.Errorf("invalid hash request length: %d", msg.Length)
	}
	file := pc.t.getFileByPiecesRoot(msg.PiecesRoot)
	if file == nil {
		return nil, fmt.Errorf("no file for pieces root %x", msg.PiecesRoot)
	}
	beginPieceIndex := file.BeginPieceIndex()
	endPieceIndex := file.EndPieceIndex()
	length := merkle.RoundUpToPowerOfTwo(uint(endPieceIndex - beginPieceIndex))
	if uint64(msg.Index)+uint64(msg.Length) > uint64(length) {
		return nil, errors.New("invalid hash range")
	}

	hashes := make([][32]byte, msg.Length)
	padHash := metainfo.HashForPiecePad(int64(pc.t.usualPieceSize()))
	for i := range hashes {
		torrentPieceIndex := beginPieceIndex + int(msg.Index) + i
		if torrentPieceIndex >= endPieceIndex {
			hashes[i] = padHash
			continue
		}
		piece := pc.t.piece(torrentPieceIndex)
		switch {
		case piece.hashV2.Ok:
			hashes[i] = piece.hashV2.Value
		case !piece.hasPieceLayer():
			hashes[i] = piece.mustGetOnlyFile().piecesRoot.Unwrap()
		default:
			return nil, fmt.Errorf("hash for piece %d is not known", torrentPieceIndex)
		}
	}
	return hashes, nil
}

func (pc *PeerConn) onHashRequest(msg *pp.Message) error {
	if !pc.t.haveInfo() || !pc.t.info.HasV2() {
		return errors.New("torrent has no v2 metadata")
	}
	if pc.messageWriter.backedUp() {
		return nil
	}

	resp := pp.Message{
		PiecesRoot:  msg.PiecesRoot,
		BaseLayer:   msg.BaseLayer,
		Index:       msg.Index,
		Length:      msg.Length,
		ProofLayers: msg.ProofLayers,
	}

	hashes, err := pc.getHashes(msg)
	if err != nil {
		pc.protocolLogger.WithNames(v2HashesLogName).Levelf(log.Debug, "error getting hashes: %v", err)
		resp.Type = pp.HashReject
		pc.write(resp)
		return nil
	}

	resp.Type = pp.Hashes
	resp.Hashes = hashes
	pc.write(resp)
	return nil
}

type hashRequest struct {
	piecesRoot                            [32]byte
	baseLayer, index, length, proofLayers pp.Integer
}

func hashRequestFromMessage(m pp.Message) hashRequest {
	return hashRequest{
		piecesRoot:  m.PiecesRoot,
		baseLayer:   m.BaseLayer,
		index:       m.Index,
		length:      m.Length,
		proofLayers: m.ProofLayers,
	}
}

func (me *PeerConn) peerPtr() *Peer {
	return &me.Peer
}

func (me *PeerConn) setPeerLoggers(a log.Logger, s *slog.Logger) {
	me.Peer.logger = a.WithDefaultLevel(log.Warning).WithContextText(fmt.Sprintf("%T %p", me, me))
	me.Peer.slogger = s.With(fmt.Sprintf("%T", me), fmt.Sprintf("%p", me))
	me.protocolLogger = me.logger.WithNames(protocolLoggingName)
}

func (p *PeerConn) initRequestState() {
	p.requestState.Requests = &peerRequests{}
}

func (cn *PeerConn) expectingChunks() bool {
	if cn.requestState.Requests.IsEmpty() {
		return false
	}
	if !cn.requestState.Interested {
		return false
	}
	if !cn.peerChoking {
		return true
	}
	haveAllowedFastRequests := false
	cn.peerAllowedFast.Iterate(func(i pieceIndex) bool {
		haveAllowedFastRequests = roaringBitmapRangeCardinality[RequestIndex](
			cn.requestState.Requests,
			cn.t.pieceRequestIndexBegin(i),
			cn.t.pieceRequestIndexBegin(i+1),
		) == 0
		return !haveAllowedFastRequests
	})
	return haveAllowedFastRequests
}

func (cn *PeerConn) cumInterest() time.Duration {
	ret := cn.priorInterest
	if cn.requestState.Interested {
		ret += time.Since(cn.lastBecameInterested)
	}
	return ret
}

func (cn *PeerConn) supportsExtension(ext pp.ExtensionName) bool {
	_, ok := cn.PeerExtensionIDs[ext]
	return ok
}

func (cn *PeerConn) statusFlags() (ret string) {
	c := func(b byte) {
		ret += string([]byte{b})
	}
	if cn.requestState.Interested {
		c('i')
	}
	if cn.choking {
		c('c')
	}
	c(':')
	ret += cn.connectionFlags()
	c(':')
	if cn.peerInterested {
		c('i')
	}
	if cn.peerChoking {
		c('c')
	}
	return
}

func (cn *PeerConn) iterContiguousPieceRequests(f func(piece pieceIndex, count int)) {
	var last Option[pieceIndex]
	var count int
	next := func(item Option[pieceIndex]) {
		if item == last {
			count++
		} else {
			if count != 0 {
				f(last.Value, count)
			}
			last = item
			count = 1
		}
	}
	cn.requestState.Requests.Iterate(func(requestIndex requestStrategy.RequestIndex) bool {
		next(Some(cn.t.pieceIndexOfRequestIndex(requestIndex)))
		return true
	})
	next(None[pieceIndex]())
}

func (cn *PeerConn) peerImplWriteStatus(w io.Writer) {
	prio, err := cn.peerPriority()
	prioStr := fmt.Sprintf("%08x", prio)
	if err != nil {
		prioStr += ": " + err.Error()
	}
	fmt.Fprintf(w, "bep40-prio: %v\n", prioStr)
	fmt.Fprintf(w, "last msg: %s, connected: %s, last helpful: %s, itime: %s, etime: %s\n",
		eventAgeString(cn.lastMessageReceived),
		eventAgeString(cn.completedHandshake),
		eventAgeString(cn.lastHelpful()),
		cn.cumInterest(),
		cn.totalExpectingTime(),
	)
	fmt.Fprintf(w,
		"%s completed, chunks uploaded: %v\n",
		cn.completedString(),
		&cn._stats.ChunksWritten,
	)
	fmt.Fprintf(w, "requested pieces:")
	cn.iterContiguousPieceRequests(func(piece pieceIndex, count int) {
		fmt.Fprintf(w, " %v(%v)", piece, count)
	})
}

func (cn *PeerConn) setInterested(interested bool) bool {
	if cn.requestState.Interested == interested {
		return true
	}
	cn.requestState.Interested = interested
	if interested {
		cn.lastBecameInterested = time.Now()
	} else if !cn.lastBecameInterested.IsZero() {
		cn.priorInterest += time.Since(cn.lastBecameInterested)
	}
	cn.updateExpectingChunks()
	return cn.writeInterested(interested)
}

func (cn *PeerConn) shouldRequest(r RequestIndex) error {
	err := cn.t.checkValidReceiveChunk(cn.t.requestIndexToRequest(r))
	if err != nil {
		return err
	}
	pi := cn.t.pieceIndexOfRequestIndex(r)
	if cn.requestState.Cancelled.Contains(r) {
		return errors.New("request is cancelled and waiting acknowledgement")
	}
	if !cn.peerHasPiece(pi) {
		return errors.New("requesting piece peer doesn't have")
	}
	if !cn.t.peerIsActive(cn.peerPtr()) {
		panic("requesting but not in active conns")
	}
	if cn.closed.IsSet() {
		panic("requesting when connection is closed")
	}
	if cn.t.hashingPiece(pi) {
		panic("piece is being hashed")
	}
	p := cn.t.piece(pi)
	if p.marking {
		panic("piece is being marked")
	}
	if cn.t.pieceQueuedForHash(pi) {
		panic("piece is queued for hash")
	}
	if cn.peerChoking && !cn.peerAllowedFast.Contains(pi) {
		if !cn.requestState.Requests.Contains(r) {
			panic("peer choking and piece not allowed fast")
		}
	}
	return nil
}

func (cn *PeerConn) mustRequest(r RequestIndex) bool {
	more, err := cn.request(r)
	if err != nil {
		panic(err)
	}
	return more
}

func (cn *PeerConn) request(r RequestIndex) (more bool, err error) {
	if err := cn.shouldRequest(r); err != nil {
		panic(err)
	}
	if cn.requestState.Requests.Contains(r) {
		return true, nil
	}
	if maxRequests(cn.requestState.Requests.GetCardinality()) >= cn.nominalMaxRequests() {
		return true, errors.New("too many outstanding requests")
	}
	cn.requestState.Requests.Add(r)
	if cn.validReceiveChunks == nil {
		cn.validReceiveChunks = make(map[RequestIndex]int)
	}
	cn.validReceiveChunks[r]++
	cn.t.requestState[r] = requestState{
		peer: weak.Make(cn),
		when: time.Now(),
	}
	cn.updateExpectingChunks()
	ppReq := cn.t.requestIndexToRequest(r)
	for _, f := range cn.callbacks.SentRequest {
		f(PeerRequestEvent{cn.peerPtr(), ppReq})
	}
	return cn._request(ppReq), nil
}

func (me *PeerConn) cancel(r RequestIndex) {
	if !me.deleteRequest(r) {
		panic("request not existing should have been guarded")
	}
	me.handleCancel(r)
	if me.isLowOnRequests() {
		me.onNeedUpdateRequests(peerUpdateRequestsPeerCancelReason)
	}
}

func (cn *PeerConn) onNeedUpdateRequests(reason updateRequestReason) {
	if cn.needRequestUpdate != "" {
		if cn.needRequestUpdate == peerUpdateRequestsRemoteRejectReason || cn.needRequestUpdate == peerUpdateRequestsPeerCancelReason {
			cn.needRequestUpdate = reason
		}
		return
	}
	cn.needRequestUpdate = reason
	cn.locker().Defer(cn.handleOnNeedUpdateRequests)
}

func (c *PeerConn) remoteRejectedRequest(r RequestIndex) bool {
	reason := peerUpdateRequestsRemoteRejectReason
	if !c.deleteRequest(r) {
		if !c.requestState.Cancelled.CheckedRemove(r) {
			return false
		}
		if c.requestState.Requests.IsEmpty() {
			reason = peerUpdateRequestsCancelAnsweredReason
		}
	}
	if c.isLowOnRequests() {
		c.onNeedUpdateRequests(reason)
	}
	c.decExpectedChunkReceive(r)
	return true
}

func (c *PeerConn) decExpectedChunkReceive(r RequestIndex) {
	count := c.validReceiveChunks[r]
	if count == 1 {
		delete(c.validReceiveChunks, r)
	} else if count > 1 {
		c.validReceiveChunks[r] = count - 1
	} else {
		panic(r)
	}
}

func (c *PeerConn) deleteRequest(r RequestIndex) bool {
	if !c.requestState.Requests.CheckedRemove(r) {
		return false
	}
	for _, f := range c.callbacks.DeletedRequest {
		f(PeerRequestEvent{c.peerPtr(), c.t.requestIndexToRequest(r)})
	}
	c.updateExpectingChunks()
	if c.t.requestingPeer(r) != c {
		panic("only one peer should have a given request at a time")
	}
	delete(c.t.requestState, r)
	return true
}

func (c *PeerConn) deleteAllRequests(reason updateRequestReason) {
	if c.requestState.Requests.IsEmpty() {
		return
	}
	c.requestState.Requests.IterateSnapshot(func(x RequestIndex) bool {
		if !c.deleteRequest(x) {
			panic("request should exist")
		}
		return true
	})
	c.assertNoRequests()
	c.t.iterPeers(func(p *Peer) {
		if p.isLowOnRequests() {
			p.onNeedUpdateRequests(reason)
		}
	})
}

func (c *PeerConn) assertNoRequests() {
	if !c.requestState.Requests.IsEmpty() {
		panic(c.requestState.Requests.GetCardinality())
	}
}

func (c *PeerConn) cancelAllRequests() {
	c.requestState.Requests.IterateSnapshot(func(x RequestIndex) bool {
		c.cancel(x)
		return true
	})
	c.assertNoRequests()
}

func (p *PeerConn) uncancelledRequests() uint64 {
	return p.requestState.Requests.GetCardinality()
}

func (c *PeerConn) checkReceivedChunk(req RequestIndex, msg *pp.Message, ppReq Request) (intended bool, err error) {
	if c.validReceiveChunks[req] <= 0 {
		err = errors.New("received unexpected chunk")
		return
	}
	c.decExpectedChunkReceive(req)

	{
		if c.requestState.Requests.Contains(req) {
			for _, f := range c.callbacks.ReceivedRequested {
				f(PeerMessageEvent{c.peerPtr(), msg})
			}
		}
		if c.deleteRequest(req) || c.requestState.Cancelled.CheckedRemove(req) {
			intended = true
			if c.isLowOnRequests() {
				c.onNeedUpdateRequests("Peer.receiveChunk deleted request")
			}
		} else {
		}
	}

	return
}

func (c *PeerConn) reconcileHandshakeStats() {
	panicif.True(c.reconciledHandshakeStats)
	if c._stats != (ConnStats{
		BytesWritten: c._stats.BytesWritten,
		BytesRead:    c._stats.BytesRead,
	}) {
		panic("bad stats")
	}
	c.relevantConnStats(&c.t.connStats)(func(cs *ConnStats) bool {
		cs.BytesRead.Add(c._stats.BytesRead.Int64())
		cs.BytesWritten.Add(c._stats.BytesWritten.Int64())
		return true
	})
	c.reconciledHandshakeStats = true
}
