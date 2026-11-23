package bt

import (
	"bytes"
	"cmp"
	"container/heap"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"hash"
	"io"
	"iter"
	"log/slog"
	"maps"
	"math"
	"math/rand"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
	"unique"
	"unsafe"
	"weak"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/anacrolix/chansync"
	"github.com/anacrolix/chansync/events"
	. "github.com/anacrolix/generics"
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2"
	"github.com/anacrolix/missinggo/v2/bitmap"
	"github.com/anacrolix/missinggo/v2/panicif"
	"github.com/anacrolix/missinggo/v2/pubsub"
	"github.com/anacrolix/multiless"
	"golang.org/x/sync/errgroup"
	"yoru/internal/bt/dht"

	"yoru/internal/bt/bencode"
	"yoru/internal/bt/internal/check"
	"yoru/internal/bt/internal/nestedmaps"
	request_strategy "yoru/internal/bt/internal/request-strategy"
	"yoru/internal/bt/merkle"
	"yoru/internal/bt/metainfo"
	pp "yoru/internal/bt/peer_protocol"
	utHolepunch "yoru/internal/bt/peer_protocol/ut-holepunch"
	"yoru/internal/bt/segments"
	"yoru/internal/bt/storage"
	"yoru/internal/bt/tracker"
	typedRoaring "yoru/internal/bt/typed-roaring"
	"yoru/internal/bt/types/infohash"
	infohash_v2 "yoru/internal/bt/types/infohash-v2"
	"yoru/internal/bt/webseed"
)

var errTorrentClosed = errors.New("torrent closed")

type torrentSlogGroupInput struct {
	name        any
	canonicalIh shortInfohash
}

type Torrent struct {
	connStats AllConnStats
	counters  TorrentStatCounters

	cl     *Client
	logger log.Logger

	baseSlogger        *slog.Logger
	_slogger           *slog.Logger
	_slogGroup         slog.Attr
	lastSlogGroupInput torrentSlogGroupInput

	networkingEnabled      chansync.Flag
	dataDownloadDisallowed chansync.Flag
	dataUploadDisallowed   bool
	userOnWriteChunkErr    func(error)

	closed          chansync.SetOnce
	closedCtx       context.Context
	closedCtxCancel context.CancelCauseFunc
	onClose         []func()

	infoHash   g.Option[metainfo.Hash]
	infoHashV2 g.Option[infohash_v2.T]

	pieces []Piece

	pieceRequestOrder []int
	pieceStateChanges pubsub.PubSub[PieceStateChange]
	chunkSize         pp.Integer
	chunkPool         sync.Pool
	_length           Option[int64]

	storageOpener *storage.Client
	storage       *storage.Torrent
	storageLock   sync.RWMutex

	announceList metainfo.AnnounceList

	info              *metainfo.Info
	getInfoCtx        context.Context
	getInfoCtxCancel  context.CancelCauseFunc
	files             *[]*File
	fileSegmentsIndex g.Option[segments.Index]

	_chunksPerRegularPiece chunkIndexType

	webSeeds            map[webseedUrlKey]*webseedPeer
	conns               map[*PeerConn]struct{}
	maxEstablishedConns int
	halfOpen            map[string]map[outgoingConnAttemptKey]*PeerInfo

	peers                       prioritizedPeers
	trackerAnnouncers           map[torrentTrackerAnnouncerKey]torrentTrackerAnnouncer
	regularTrackerAnnounceState map[torrentTrackerAnnouncerKey]*announceState
	numDHTAnnounces             int

	nameMu      sync.RWMutex
	displayName string

	metadataBytes           []byte
	metadataCompletedChunks []bool
	metadataChanged         sync.Cond

	gotMetainfoC chan struct{}

	readers                map[*reader]struct{}
	readerAt               []RequestIndex
	_readerNowPieces       bitmap.Bitmap
	_readerReadaheadPieces bitmap.Bitmap

	_pendingPieces      roaring.Bitmap
	_completedPieces    roaring.Bitmap
	piecesQueuedForHash typedRoaring.Bitmap[pieceIndex]
	activePieceHashes   int

	connsWithAllPieces map[*Peer]struct{}

	requestState map[RequestIndex]requestState
	dirtyChunks  typedRoaring.Bitmap[RequestIndex]

	pex pexState

	complete chansync.Flag

	activeSources sync.Map
	sourceMutex   sync.Mutex

	smartBanCache smartBanCache

	requestPieceStates []g.Option[request_strategy.PieceRequestOrderState]
	requestIndexes     []RequestIndex

	disableTriggers                 bool
	initialPieceCheckDisabled       bool
	ignoreUnverifiedPieceCompletion bool

	sawInitiallyIncompleteData bool
}

type torrentTrackerAnnouncerKey struct {
	ShortInfohash shortInfohash
	url           trackerAnnouncerKey
}

func (me torrentTrackerAnnouncerKey) Compare(other torrentTrackerAnnouncerKey) int {
	return cmp.Or(
		me.ShortInfohash.Compare(other.ShortInfohash),
		cmp.Compare(me.url, other.url))
}

type trackerAnnouncerKey string

type outgoingConnAttemptKey = *PeerInfo

func (t *Torrent) length() int64 {
	return t._length.Value
}

func (t *Torrent) selectivePieceAvailabilityFromPeers(i pieceIndex) (count int) {
	t.iterPeers(func(peer *Peer) {
		if peer.closed.IsSet() {
			return
		}
		if _, ok := t.connsWithAllPieces[peer]; ok {
			return
		}
		if peer.peerHasPiece(i) {
			count++
		}
	})
	return
}

func (t *Torrent) decPieceAvailability(i pieceIndex) {
	if !t.haveInfo() {
		return
	}
	p := t.piece(i)
	if p.relativeAvailability <= 0 {
		panic(p.relativeAvailability)
	}
	p.relativeAvailability--
	t.updatePieceRequestOrderPiece(i)
}

func (t *Torrent) incPieceAvailability(i pieceIndex) {
	if t.haveInfo() {
		p := t.piece(i)
		p.relativeAvailability++
		t.updatePieceRequestOrderPiece(i)
	}
}

func (t *Torrent) readerNowPieces() bitmap.Bitmap {
	return t._readerNowPieces
}

func (t *Torrent) readerReadaheadPieces() bitmap.Bitmap {
	return t._readerReadaheadPieces
}

func (t *Torrent) ignorePieceForRequests(i pieceIndex) bool {
	return t.piece(i).ignoreForRequests()
}

func (t *Torrent) Closed() events.Done {
	return t.closed.Done()
}

func (t *Torrent) KnownSwarm() (ks []PeerInfo) {
	t.cl.rLock()
	defer t.cl.rUnlock()

	t.peers.Each(func(peer PeerInfo) {
		ks = append(ks, peer)
	})

	for _, attempts := range t.halfOpen {
		for _, peer := range attempts {
			ks = append(ks, *peer)
		}
	}

	for conn := range t.conns {
		ks = append(ks, PeerInfo{
			Id:                 conn.PeerID,
			Addr:               conn.RemoteAddr,
			Source:             conn.Discovery,
			SupportsEncryption: conn.headerEncrypted,
		})
	}

	return
}

func (t *Torrent) setChunkSize(size pp.Integer) {
	t.chunkSize = size
	t.chunkPool = sync.Pool{
		New: func() any {
			b := make([]byte, size)
			return &b
		},
	}
}

func (t *Torrent) pieceComplete(piece pieceIndex) bool {
	return t._completedPieces.Contains(bitmap.BitIndex(piece))
}

func (t *Torrent) pieceCompleteUncached(piece pieceIndex) (ret storage.Completion) {
	p := t.piece(piece)
	if t.ignoreUnverifiedPieceCompletion && p.numVerifies == 0 {
		return
	}
	if t.storage == nil {
		return storage.Completion{Complete: false, Ok: false}
	}
	return p.Storage().Completion()
}

func (t *Torrent) appendUnclosedConns(ret []*PeerConn) []*PeerConn {
	return t.appendConns(ret, func(conn *PeerConn) bool {
		return !conn.closed.IsSet()
	})
}

func (t *Torrent) appendConns(ret []*PeerConn, f func(*PeerConn) bool) []*PeerConn {
	for c := range t.conns {
		if f(c) {
			ret = append(ret, c)
		}
	}
	return ret
}

func (t *Torrent) addPeer(p PeerInfo) (added bool) {
	cl := t.cl
	if t.closed.IsSet() {
		return false
	}
	if ipAddr, ok := tryIpPortFromNetAddr(p.Addr); ok {
		if cl.badPeerIPPort(ipAddr.IP, ipAddr.Port) {
			return false
		}
	}
	if replaced, ok := t.peers.AddReturningReplacedPeer(p); ok {
		if !replaced.equal(p) {
			t.logger.WithDefaultLevel(log.Debug).Printf("added %v replacing %v", p, replaced)
			added = true
		}
	} else {
		added = true
	}
	t.openNewConns()
	for t.peers.Len() > cl.config.TorrentPeersHighWater {
		t.peers.DeleteMin()
	}
	return
}

func (t *Torrent) invalidateMetadata() {
	for i := 0; i < len(t.metadataCompletedChunks); i++ {
		t.metadataCompletedChunks[i] = false
	}
	t.nameMu.Lock()
	panicif.NotNil(t.info)
	t.nameMu.Unlock()
}

func (t *Torrent) saveMetadataPiece(index int, data []byte) {
	if t.haveInfo() {
		return
	}
	if index >= len(t.metadataCompletedChunks) {
		t.logger.Printf("%s: ignoring metadata piece %d", t, index)
		return
	}
	copy(t.metadataBytes[(1<<14)*index:], data)
	t.metadataCompletedChunks[index] = true
}

func (t *Torrent) metadataPieceCount() int {
	return (len(t.metadataBytes) + (1 << 14) - 1) / (1 << 14)
}

func (t *Torrent) haveMetadataPiece(piece int) bool {
	if piece < 0 {
		return false
	}
	if t.haveInfo() {
		return (1<<14)*piece < len(t.metadataBytes)
	} else {
		return piece < len(t.metadataCompletedChunks) && t.metadataCompletedChunks[piece]
	}
}

func (t *Torrent) metadataSize() int {
	return len(t.metadataBytes)
}

func (t *Torrent) makePieces() {
	t.pieces = make([]Piece, t.info.NumPieces())
	for i := range t.pieces {
		piece := &t.pieces[i]
		piece.t = t
		piece.index = i
		piece.noPendingWrites.L = &piece.pendingWritesMutex
		if t.info.HasV1() {
			piece.hash = (*metainfo.Hash)(unsafe.Pointer(
				unsafe.SliceData(t.info.Pieces[i*sha1.Size : (i+1)*sha1.Size])))
		}
		files := *t.files
		beginFile := pieceFirstFileIndex(piece.torrentBeginOffset(), files)
		endFile := pieceEndFileIndex(piece.torrentEndOffset(), files)
		piece.beginFile = beginFile
		piece.endFile = endFile
	}
}

func (t *Torrent) addPieceLayersLocked(layers map[string]string) (errs []error) {
	if layers == nil {
		return
	}
files:
	for _, f := range *t.files {
		if f.numPieces() <= 1 {
			continue
		}
		if !f.piecesRoot.Ok {
			err := fmt.Errorf("no piece root set for file %v", f)
			errs = append(errs, err)
			continue files
		}
		compactLayer, ok := layers[string(f.piecesRoot.Value[:])]
		var hashes [][32]byte
		if ok {
			var err error
			hashes, err = merkle.CompactLayerToSliceHashes(compactLayer)
			if err != nil {
				err = fmt.Errorf("bad piece layers for file %q: %w", f, err)
				errs = append(errs, err)
				continue files
			}
		} else {
			if f.length > t.info.PieceLength {
				t.logger.Levelf(log.Warning, "no piece layers for file %q", f)
			}
			continue files
		}
		if len(hashes) != f.numPieces() {
			errs = append(
				errs,
				fmt.Errorf("file %q: got %v hashes expected %v", f, len(hashes), f.numPieces()),
			)
			continue files
		}
		root := merkle.RootWithPadHash(hashes, metainfo.HashForPiecePad(t.info.PieceLength))
		if root != f.piecesRoot.Value {
			errs = append(errs, fmt.Errorf("%v: expected hash %x got %x", f, f.piecesRoot.Value, root))
			continue files
		}
		for i := range f.numPieces() {
			pi := f.BeginPieceIndex() + i
			p := t.piece(pi)
			p.setV2Hash(hashes[i])
		}
	}
	return
}

func (t *Torrent) AddPieceLayers(layers map[string]string) (errs []error) {
	t.cl.lock()
	defer t.cl.unlock()
	return t.addPieceLayersLocked(layers)
}

func pieceFirstFileIndex(pieceOffset int64, files []*File) int {
	for i, f := range files {
		if f.offset+f.length > pieceOffset {
			return i
		}
	}
	return 0
}

func pieceEndFileIndex(pieceEndOffset int64, files []*File) int {
	for i, f := range files {
		if f.offset >= pieceEndOffset {
			return i
		}
	}
	return len(files)
}

func (t *Torrent) cacheLength() {
	var l int64
	for _, f := range t.info.UpvertedFiles() {
		l += f.Length
	}
	t._length = Some(l)
}

func (t *Torrent) setInfo(info *metainfo.Info) error {
	if err := validateInfo(info); err != nil {
		return fmt.Errorf("bad info: %w", err)
	}
	if t.storageOpener != nil {
		var err error
		ctx := log.ContextWithLogger(context.Background(), t.logger)
		t.storage, err = t.storageOpener.OpenTorrent(ctx, info, *t.canonicalShortInfohash())
		if err != nil {
			return fmt.Errorf("error opening torrent storage: %w", err)
		}
	}
	func() {
		t.nameMu.Lock()
		defer t.nameMu.Unlock()
		t.info = info
		panicif.True(t.fileSegmentsIndex.Set(info.FileSegmentsIndex()).Ok)
		t.getInfoCtxCancel(errors.New("got info"))
	}()
	t._chunksPerRegularPiece = chunkIndexType(intCeilDiv(pp.Integer(t.usualPieceSize()), t.chunkSize))
	t.deferUpdateComplete()
	t.displayName = ""
	t.initFiles()
	t.cacheLength()
	t.makePieces()
	return nil
}

func (t *Torrent) pieceRequestOrderKey(i int) request_strategy.PieceRequestOrderKey {
	return request_strategy.PieceRequestOrderKey{
		InfoHash: unique.Make(*t.canonicalShortInfohash()),
		Index:    i,
	}
}

func (t *Torrent) onSetInfo() {
	t.pieceRequestOrder = rand.Perm(t.numPieces())
	t.initPieceRequestOrder()
	MakeSliceWithLength(&t.requestPieceStates, t.numPieces())
	for i := range t.pieces {
		p := &t.pieces[i]
		if p.relativeAvailability != 0 {
			panic(p.relativeAvailability)
		}
		p.relativeAvailability = t.selectivePieceAvailabilityFromPeers(i)
		t.addRequestOrderPiece(i)
		t.setInitialPieceCompletionFromStorage(i)
		t.queueInitialPieceCheck(i)
	}
	t.cl.event.Broadcast()
	close(t.gotMetainfoC)
	t.updateWantPeersEvent()
	t.requestState = make(map[RequestIndex]requestState)
	panicif.Err(t.startPieceHashers())
	t.iterPeers(func(p *Peer) {
		p.onGotInfo(t.info)
		p.onNeedUpdateRequests("onSetInfo")
	})
}

func (t *Torrent) hashInfoBytes(b []byte, info *metainfo.Info) error {
	v1Hash := infohash.HashBytes(b)
	v2Hash := infohash_v2.HashBytes(b)
	cl := t.cl
	if t.infoHash.Ok && !t.infoHashV2.Ok {
		if v1Hash == t.infoHash.Value {
			if info.HasV2() {
				t.infoHashV2.Set(v2Hash)
				cl.torrentsByShortHash[*v2Hash.ToShort()] = t
			}
		} else if *v2Hash.ToShort() == t.infoHash.Value {
			if !info.HasV2() {
				return errors.New("invalid v2 info")
			}
			t.infoHashV2.Set(v2Hash)
			t.infoHash.SetNone()
			if info.HasV1() {
				cl.torrentsByShortHash[v1Hash] = t
				t.infoHash.Set(v1Hash)
			}
		} else {
			return errors.New("info bytes match neither infohash")
		}
	} else if t.infoHash.Ok && t.infoHashV2.Ok {
		if v1Hash != t.infoHash.Value {
			return errors.New("incorrect v1 infohash")
		}
		if v2Hash != t.infoHashV2.Value {
			return errors.New("incorrect v2 infohash")
		}
	} else if !t.infoHash.Ok && t.infoHashV2.Ok {
		if v2Hash != t.infoHashV2.Value {
			return errors.New("incorrect v2 infohash")
		}
		if info.HasV1() {
			t.infoHash.Set(v1Hash)
			cl.torrentsByShortHash[v1Hash] = t
		}
	} else {
		panic("no expected infohashes")
	}
	return nil
}

func (t *Torrent) setInfoBytesLocked(b []byte) (err error) {
	if !t.infoBytesMatchAHash(b) {
		return errors.New("info bytes match no expected infohash")
	}
	var info metainfo.Info
	err = bencode.Unmarshal(b, &info)
	if err != nil {
		err = fmt.Errorf("unmarshalling info bytes: %w", err)
		return
	}
	err = t.hashInfoBytes(b, &info)
	if err != nil {
		return
	}
	if t.info != nil {
		t.metadataBytes = b
		t.metadataCompletedChunks = nil
		return nil
	}
	err = t.setInfo(&info)
	if err != nil {
		return
	}
	t.metadataBytes = b
	t.metadataCompletedChunks = nil
	t.onSetInfo()
	return nil
}

func (t *Torrent) infoBytesMatchAHash(b []byte) bool {
	v1Hash := infohash.HashBytes(b)
	v2Hash := infohash_v2.HashBytes(b)
	if t.infoHash.Ok && (v1Hash == t.infoHash.Value || *v2Hash.ToShort() == t.infoHash.Value) {
		return true
	}
	return t.infoHashV2.Ok && v2Hash == t.infoHashV2.Value
}

func (t *Torrent) setInfoUnlocked(info *metainfo.Info) (err error) {
	t.cl.lock()
	defer t.cl.unlock()
	err = t.setInfo(info)
	if err != nil {
		return
	}
	t.onSetInfo()
	return
}

func (t *Torrent) haveAllMetadataPieces() bool {
	if t.haveInfo() {
		return true
	}
	if t.metadataCompletedChunks == nil {
		return false
	}
	for _, have := range t.metadataCompletedChunks {
		if !have {
			return false
		}
	}
	return true
}

func (t *Torrent) setMetadataSize(size int) (err error) {
	if t.haveInfo() {
		return
	}
	if size <= 0 || size > int(maxMetadataSize) {
		return log.WithLevel(log.Warning, errors.New("bad size"))
	}
	if len(t.metadataBytes) == size {
		return
	}
	old, done := t.metadataBytes, t.metadataCompletedChunks
	t.metadataBytes = make([]byte, size)
	t.metadataCompletedChunks = make([]bool, (size+(1<<14)-1)/(1<<14))
	for i, ok := range done {
		if end := (i + 1) << 14; ok && end <= len(old) && end <= size {
			copy(t.metadataBytes[i<<14:end], old[i<<14:end])
			t.metadataCompletedChunks[i] = true
		}
	}
	t.metadataChanged.Broadcast()
	for c := range t.conns {
		c.requestPendingMetadata()
	}
	return
}

func (t *Torrent) bestName() (_ g.Option[string]) {
	t.nameMu.RLock()
	defer t.nameMu.RUnlock()
	if t.haveInfo() {
		return g.Some(t.info.BestName())
	}
	if t.displayName != "" {
		return g.Some(t.displayName)
	}
	return
}

func (t *Torrent) name() string {
	return t.bestName().UnwrapOr("infohash:" + t.canonicalShortInfohash().HexString())
}

func (t *Torrent) pieceState(index pieceIndex) (ret PieceState) {
	p := &t.pieces[index]
	ret.Priority = p.effectivePriority()
	ret.Completion = p.completion()
	ret.QueuedForHash = p.queuedForHash()
	ret.Hashing = p.hashing
	ret.Checking = ret.QueuedForHash || ret.Hashing
	ret.Marking = p.marking
	if ret.Ok && !ret.Complete && t.piecePartiallyDownloaded(index) {
		ret.Partial = true
	}
	if t.info.HasV2() && !p.hashV2.Ok && p.hasPieceLayer() {
		ret.MissingPieceLayerHash = true
	}
	return
}

func (t *Torrent) metadataPieceSize(piece int) int {
	return metadataPieceSize(len(t.metadataBytes), piece)
}

func (t *Torrent) newMetadataExtensionMessage(c *PeerConn, msgType pp.ExtendedMetadataRequestMsgType, piece int, data []byte) pp.Message {
	return pp.Message{
		Type:       pp.Extended,
		ExtendedID: c.PeerExtensionIDs[pp.ExtensionNameMetadata],
		ExtendedPayload: append(bencode.MustMarshal(pp.ExtendedMetadataRequestMsg{
			Piece:     piece,
			TotalSize: len(t.metadataBytes),
			Type:      msgType,
		}), data...),
	}
}

type pieceAvailabilityRun struct {
	Count        pieceIndex
	Availability int
}

func (me pieceAvailabilityRun) String() string {
	return fmt.Sprintf("%v(%v)", me.Count, me.Availability)
}

func (t *Torrent) pieceAvailabilityRuns() (ret []pieceAvailabilityRun) {
	rle := missinggo.NewRunLengthEncoder(func(el any, count uint64) {
		ret = append(ret, pieceAvailabilityRun{Availability: el.(int), Count: int(count)})
	})
	for i := range t.pieces {
		rle.Append(t.pieces[i].availability(), 1)
	}
	rle.Flush()
	return
}

func (t *Torrent) pieceAvailabilityFrequencies() (freqs []int) {
	freqs = make([]int, t.numActivePeers()+1)
	for i := range t.pieces {
		freqs[t.piece(i).availability()]++
	}
	return
}

func (t *Torrent) pieceStateRuns() (ret PieceStateRuns) {
	rle := missinggo.NewRunLengthEncoder(func(el any, count uint64) {
		ret = append(ret, PieceStateRun{
			PieceState: el.(PieceState),
			Length:     int(count),
		})
	})
	for index := range t.pieces {
		rle.Append(t.pieceState(index), 1)
	}
	rle.Flush()
	return
}

func (psr PieceStateRun) String() (ret string) {
	ret = fmt.Sprintf("%d", psr.Length)
	ret += func() string {
		switch psr.Priority {
		case PiecePriorityNext:
			return "N"
		case PiecePriorityNormal:
			return "."
		case PiecePriorityReadahead:
			return "R"
		case PiecePriorityNow:
			return "!"
		case PiecePriorityHigh:
			return "H"
		default:
			return ""
		}
	}()
	if psr.Hashing {
		ret += "H"
	}
	if psr.QueuedForHash {
		ret += "Q"
	}
	if psr.Marking {
		ret += "M"
	}
	if psr.Partial {
		ret += "P"
	}
	if psr.Ok && psr.Complete {
		ret += "C"
	}
	if !psr.Ok {
		ret += "?"
	}
	if psr.MissingPieceLayerHash {
		ret += "h"
	}
	return
}

func (t *Torrent) writeStatus(w io.Writer) {
	if t.infoHash.Ok {
		fmt.Fprintf(w, "Infohash: %s\n", t.infoHash.Value.HexString())
	}
	if t.infoHashV2.Ok {
		fmt.Fprintf(w, "Infohash v2: %s\n", t.infoHashV2.Value.HexString())
	}
	fmt.Fprintf(w, "Metadata length: %d\n", t.metadataSize())
	if !t.haveInfo() {
		fmt.Fprintf(w, "Metadata have: ")
		for _, h := range t.metadataCompletedChunks {
			fmt.Fprintf(w, "%c", func() rune {
				if h {
					return 'H'
				} else {
					return '.'
				}
			}())
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "Piece request order length: %v\n", func() any {
		pro := t.getPieceRequestOrder()
		if pro == nil {
			return nil
		}
		return pro.Len()
	}())
	fmt.Fprintf(w, "Piece length: %s\n",
		func() string {
			if t.haveInfo() {
				return fmt.Sprintf("%v (%v chunks)",
					t.usualPieceSize(),
					float64(t.usualPieceSize())/float64(t.chunkSize))
			} else {
				return "no info"
			}
		}(),
	)
	if t.info != nil {
		fmt.Fprintf(w, "Num Pieces: %d (%d completed)\n", t.numPieces(), t.numPiecesCompleted())
		fmt.Fprintf(w, "Piece States: %s\n", t.pieceStateRuns())
		if false {
			fmt.Fprintf(w, "Piece availability: %v\n", strings.Join(func() (ret []string) {
				for _, run := range t.pieceAvailabilityRuns() {
					ret = append(ret, run.String())
				}
				return
			}(), " "))
		}
		fmt.Fprintf(w, "Piece availability frequency: %v\n", strings.Join(
			func() (ret []string) {
				for avail, freq := range t.pieceAvailabilityFrequencies() {
					if freq == 0 {
						continue
					}
					ret = append(ret, fmt.Sprintf("%v: %v", avail, freq))
				}
				return
			}(),
			", "))
	}
	fmt.Fprintf(w, "Reader Pieces:")
	t.forReaderOffsetPieces(func(begin, end pieceIndex) (again bool) {
		fmt.Fprintf(w, " %d:%d", begin, end)
		return true
	})
	fmt.Fprintln(w)

	fmt.Fprintf(w, "Enabled trackers:\n")
	{
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "    URL\tExtra\n")
		sortedTrackerAnnouncers := slices.SortedFunc(
			maps.Values(t.trackerAnnouncers),
			func(l, r torrentTrackerAnnouncer) int {
				lu := l.URL()
				ru := r.URL()
				var luns, runs url.URL = *lu, *ru
				luns.Scheme = ""
				runs.Scheme = ""
				var ml multiless.Computation
				ml = multiless.EagerOrdered(ml, luns.String(), runs.String())
				ml = multiless.EagerOrdered(ml, lu.String(), ru.String())
				return ml.OrderingInt()
			},
		)
		for _, ta := range sortedTrackerAnnouncers {
			fmt.Fprintf(tw, "    %q\t%v\n", ta.URL(), ta.statusLine())
		}
		tw.Flush()
	}

	fmt.Fprintf(w, "DHT Announces: %d\n", t.numDHTAnnounces)

	dumpStats(w, t.statsLocked())

	fmt.Fprintf(w, "webseeds:\n")

	t.writePeerStatuses(w, func(yield func(*Peer) bool) {
		for _, ws := range t.webSeeds {
			if !yield(&ws.peer) {
				return
			}
		}
	})

	peerConns := slices.SortedFunc(maps.Keys(t.conns), func(l, r *PeerConn) int {
		ml := multiless.New()
		lpp := g.ResultFromTuple(l.peerPriority()).ToOption()
		rpp := g.ResultFromTuple(r.peerPriority()).ToOption()
		ml = ml.Bool(lpp.Ok, rpp.Ok)
		ml = ml.Uint32(rpp.Value, lpp.Value)
		return ml.OrderingInt()
	})

	fmt.Fprintf(w, "%v peer conns:\n", len(peerConns))
	var peerIter iter.Seq[*Peer] = func(yield func(*Peer) bool) {
		for _, pc := range peerConns {
			if !yield(&pc.Peer) {
				return
			}
		}
	}
	t.writePeerStatuses(w, peerIter)
}

func (t *Torrent) writePeerStatuses(w io.Writer, peers iter.Seq[*Peer]) {
	var buf bytes.Buffer
	for c := range peers {
		fmt.Fprintf(w, "- ")
		buf.Reset()
		c.writeStatus(&buf)
		w.Write(bytes.TrimRight(
			bytes.ReplaceAll(buf.Bytes(), []byte("\n"), []byte("\n  ")),
			" "))
	}
}

func (t *Torrent) haveInfo() bool {
	return t.info != nil
}

func (t *Torrent) newMetaInfo() metainfo.MetaInfo {
	return metainfo.MetaInfo{
		CreationDate: time.Now().Unix(),
		Comment:      "dynamic metainfo from client",
		CreatedBy:    "Yoru",
		AnnounceList: t.announceList.Clone(),
		InfoBytes: func() []byte {
			if t.haveInfo() {
				return t.metadataBytes
			} else {
				return nil
			}
		}(),
		UrlList: func() []string {
			ret := make([]string, 0, len(t.webSeeds))
			for url := range t.webSeeds {
				ret = append(ret, url.Value())
			}
			return ret
		}(),
		PieceLayers: t.pieceLayers(),
	}
}

func (t *Torrent) BytesMissing() (n int64) {
	t.cl.rLock()
	n = t.bytesMissingLocked()
	t.cl.rUnlock()
	return
}

func (t *Torrent) bytesMissingLocked() int64 {
	return t.bytesLeft()
}

func iterFlipped(b *roaring.Bitmap, end uint64, cb func(uint32) bool) {
	roaring.Flip(b, 0, end).Iterate(cb)
}

func (t *Torrent) bytesLeft() (left int64) {
	iterFlipped(&t._completedPieces, uint64(t.numPieces()), func(x uint32) bool {
		p := t.piece(pieceIndex(x))
		left += int64(p.length() - p.numDirtyBytes())
		return true
	})
	return
}

func (t *Torrent) bytesLeftAnnounce() int64 {
	if t.haveInfo() {
		return t.bytesLeft()
	} else {
		return -1
	}
}

func (t *Torrent) piecePartiallyDownloaded(piece pieceIndex) bool {
	if t.pieceComplete(piece) {
		return false
	}
	if t.pieceAllDirty(piece) {
		return false
	}
	return t.pieces[piece].hasDirtyChunks()
}

func (t *Torrent) usualPieceSize() int {
	return int(t.info.PieceLength)
}

func (t *Torrent) numPieces() pieceIndex {
	return t.info.NumPieces()
}

func (t *Torrent) numPiecesCompleted() (num pieceIndex) {
	return pieceIndex(t._completedPieces.GetCardinality())
}

func (t *Torrent) close(wg *sync.WaitGroup) {
	panicif.False(t.closed.Set())
	t.eachShortInfohash(func(short [20]byte) {
		delete(t.cl.torrentsByShortHash, short)
	})
	t.deferUpdateRegularTrackerAnnouncing()
	t.closedCtxCancel(errTorrentClosed)
	t.getInfoCtxCancel(errTorrentClosed)
	for _, f := range t.onClose {
		f()
	}
	if t.storage != nil {
		wg.Go(func() {
			t.storageLock.Lock()
			defer t.storageLock.Unlock()
			if f := t.storage.Close; f != nil {
				err1 := f()
				if err1 != nil {
					t.logger.WithDefaultLevel(log.Warning).Printf("error closing storage: %v", err1)
				}
			}
		})
	}
	t.iterPeers(func(p *Peer) {
		p.close()
	})
	if t.storage != nil {
		t.deletePieceRequestOrder()
	}
	t.piecesQueuedForHash.Clear()
	t.assertAllPiecesRelativeAvailabilityZero()
	t.pex.Reset()
	t.cl.event.Broadcast()
	t.pieceStateChanges.Close()
	t.updateWantPeersEvent()
	g.MustDelete(t.cl.torrents, t)
	if false {
		if len(t.cl.torrents) == 0 {
			panicif.NotZero(len(t.cl.activeWebseedRequests))
		}
	}
	return
}

func (t *Torrent) assertAllPiecesRelativeAvailabilityZero() {
	for i := range t.pieces {
		p := t.piece(i)
		if p.relativeAvailability != 0 {
			if check.Enabled {
				panic(fmt.Sprintf("piece %v has relative availability %v", i, p.relativeAvailability))
			}
			t.logger.Levelf(log.Warning, "piece %v has relative availability %v on close", i, p.relativeAvailability)
			return
		}
	}
}

func (t *Torrent) requestIndexBegin(r RequestIndex) int64 {
	return t.requestOffset(t.requestIndexToRequest(r))
}

func (t *Torrent) requestIndexEnd(r RequestIndex) int64 {
	req := t.requestIndexToRequest(r)
	return t.requestOffset(req) + int64(req.Length)
}

func (t *Torrent) requestOffset(r Request) int64 {
	return torrentRequestOffset(t.length(), int64(t.usualPieceSize()), r)
}

func (t *Torrent) offsetRequest(off int64) (req Request, ok bool) {
	return torrentOffsetRequest(t.length(), t.info.PieceLength, int64(t.chunkSize), off)
}

func (t *Torrent) writeChunk(piece int, begin int64, data []byte) (err error) {
	n, err := t.piece(piece).Storage().WriteAt(data, begin)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}

func (t *Torrent) bitfield() (bf []bool) {
	bf = make([]bool, t.numPieces())
	t._completedPieces.Iterate(func(piece uint32) (again bool) {
		bf[piece] = true
		return true
	})
	return
}

func (t *Torrent) pieceNumChunks(piece pieceIndex) chunkIndexType {
	return chunkIndexType(intCeilDiv(t.pieceLength(piece), t.chunkSize))
}

func (t *Torrent) chunksPerRegularPiece() chunkIndexType {
	return t._chunksPerRegularPiece
}

func (t *Torrent) pendAllChunkSpecs(pieceIndex pieceIndex) {
	t.dirtyChunks.RemoveRange(
		uint64(t.pieceRequestIndexBegin(pieceIndex)),
		uint64(t.pieceRequestIndexBegin(pieceIndex+1)))
}

func (t *Torrent) pieceLength(piece pieceIndex) pp.Integer {
	if t.info.PieceLength == 0 {
		return 0
	}
	if t.info.FilesArePieceAligned() {
		p := t.piece(piece)
		file := p.mustGetOnlyFile()
		if piece == file.EndPieceIndex()-1 {
			return pp.Integer(file.length - (p.torrentBeginOffset() - file.offset))
		}
		return pp.Integer(t.usualPieceSize())
	}
	if piece == t.numPieces()-1 {
		ret := pp.Integer(t.length() % t.info.PieceLength)
		if ret != 0 {
			return ret
		}
	}
	return pp.Integer(t.info.PieceLength)
}

func (t *Torrent) getBlockCheckingWriterForPiece(piece pieceIndex) blockCheckingWriter {
	return blockCheckingWriter{
		cache:        &t.smartBanCache,
		requestIndex: t.pieceRequestIndexBegin(piece),
		chunkBuffer:  t.getChunkBuffer(),
	}
}

func (t *Torrent) hasSmartbanDataForPiece(piece pieceIndex) bool {
	return t.smartBanCache.HasPeerForBlocks(iterRange(t.pieceRequestIndexBegin(piece), t.pieceRequestIndexBegin(piece+1)))
}

func (t *Torrent) countBytesHashed(n int64) {
	t.counters.BytesHashed.Add(n)
	t.cl.counters.BytesHashed.Add(n)
}

func (t *Torrent) hashPiece(piece pieceIndex) (
	correct bool,
	differingPeers map[bannableAddr]struct{},
	err error,
) {
	p := t.piece(piece)
	p.waitNoPendingWrites()
	storagePiece := p.Storage()

	if p.hash != nil {
		if i, ok := storagePiece.PieceImpl.(storage.SelfHashing); ok {
			var sum metainfo.Hash
			sum, err = i.SelfHash()
			if err == nil {
				t.countBytesHashed(int64(p.length()))
			}
			correct = sum == *p.hash
			return
		}
		h := pieceHash.New()
		differingPeers, err = t.hashPieceWithSpecificHash(piece, h)
		if t.info.FilesArePieceAligned() {
			paddingLen := p.Info().V1Length() - p.Info().Length()
			written, err := io.CopyN(h, zeroReader, paddingLen)
			if written != paddingLen {
				panic(fmt.Sprintf(
					"piece %v: wrote %v bytes of padding, expected %v, error: %v",
					piece,
					written,
					paddingLen,
					err,
				))
			}
			t.countBytesHashed(written)
		}
		var sum [20]byte
		sumExactly(sum[:], h.Sum)
		correct = sum == *p.hash
	} else if p.hashV2.Ok {
		h := merkle.NewHash()
		differingPeers, err = t.hashPieceWithSpecificHash(piece, h)
		var sum [32]byte
		sumExactly(sum[:], func(b []byte) []byte {
			return h.SumMinLength(b, int(t.info.PieceLength))
		})
		correct = sum == p.hashV2.Value
	} else {
		expected := p.mustGetOnlyFile().piecesRoot.Unwrap()
		h := merkle.NewHash()
		differingPeers, err = t.hashPieceWithSpecificHash(piece, h)
		var sum [32]byte
		sumExactly(sum[:], h.Sum)
		correct = sum == expected
	}
	return
}

func sumExactly(dst []byte, sum func(b []byte) []byte) {
	n := len(sum(dst[:0]))
	if n != len(dst) {
		panic(n)
	}
}

func (t *Torrent) hashPieceWithSpecificHash(piece pieceIndex, h hash.Hash) (
	differingPeers map[bannableAddr]struct{},
	err error,
) {
	var w io.Writer = h
	if t.hasSmartbanDataForPiece(piece) {
		smartBanWriter := t.getBlockCheckingWriterForPiece(piece)
		defer func() {
			t.putChunkBuffer(smartBanWriter.chunkBuffer)
			smartBanWriter.chunkBuffer = nil
		}()
		defer func() {
			if err != nil {
				return
			}
			smartBanWriter.Flush()
			differingPeers = smartBanWriter.badPeers
		}()
		w = io.MultiWriter(h, &smartBanWriter)
	}
	p := t.piece(piece)
	storagePiece := p.Storage()
	var written int64
	written, err = storagePiece.WriteTo(w)
	t.countBytesHashed(written)
	return
}

func (t *Torrent) haveAnyPieces() bool {
	return !t._completedPieces.IsEmpty()
}

func (t *Torrent) haveAllPieces() bool {
	if !t.haveInfo() {
		return false
	}
	return t._completedPieces.GetCardinality() == bitmap.BitRange(t.numPieces())
}

func (t *Torrent) havePiece(index pieceIndex) bool {
	return t.haveInfo() && t.pieceComplete(index)
}

func (t *Torrent) maybeDropMutuallyCompletePeer(
	p *PeerConn,
) {
	if !t.cl.config.DropMutuallyCompletePeers {
		return
	}
	if !t.haveAllPieces() {
		return
	}
	if all, known := p.peerHasAllPieces(); !(known && all) {
		return
	}
	if p.useful() {
		return
	}
	p.logger.Levelf(log.Debug, "is mutually complete; dropping")
	p.drop()
}

func (t *Torrent) haveRequestIndexChunk(reqIndex RequestIndex) bool {
	return t.haveChunk(t.requestIndexToRequest(reqIndex))
}

func (t *Torrent) haveChunk(r Request) bool {
	if !t.haveInfo() {
		return false
	}
	if t.pieceComplete(pieceIndex(r.Index)) {
		return true
	}
	p := t.piece(int(r.Index))
	return !p.pendingChunk(r.ChunkSpec, t.chunkSize)
}

func chunkIndexFromChunkSpec(cs ChunkSpec, chunkSize pp.Integer) chunkIndexType {
	return chunkIndexType(cs.Begin / chunkSize)
}

func (t *Torrent) wantPieceIndex(index pieceIndex) bool {
	return !t._pendingPieces.IsEmpty() && t._pendingPieces.Contains(uint32(index))
}

var peerConnSlices sync.Pool

func getPeerConnSlice(cap int) []*PeerConn {
	getInterface := peerConnSlices.Get()
	if getInterface == nil {
		return make([]*PeerConn, 0, cap)
	} else {
		return getInterface.([]*PeerConn)[:0]
	}
}

func (t *Torrent) withUnclosedConns(f func([]*PeerConn)) {
	sl := t.appendUnclosedConns(getPeerConnSlice(len(t.conns)))
	f(sl)
	peerConnSlices.Put(sl)
}

func (t *Torrent) worstBadConnFromSlice(opts worseConnLensOpts, sl []*PeerConn) *PeerConn {
	wcs := worseConnSlice{conns: sl}
	wcs.initKeys(opts)
	heap.Init(&wcs)
	for wcs.Len() != 0 {
		c := heap.Pop(&wcs).(*PeerConn)
		if opts.incomingIsBad && !c.outgoing {
			return c
		}
		if opts.outgoingIsBad && c.outgoing {
			return c
		}
		if c._stats.ChunksReadWasted.Int64() >= 6 && c._stats.ChunksReadWasted.Int64() > c._stats.ChunksReadUseful.Int64() {
			return c
		}
		if wcs.Len() >= (t.maxEstablishedConns+1)/2 {
			if time.Since(c.completedHandshake) > time.Minute {
				return c
			}
		}
	}
	return nil
}

func (t *Torrent) worstBadConn(opts worseConnLensOpts) (ret *PeerConn) {
	t.withUnclosedConns(func(ucs []*PeerConn) {
		ret = t.worstBadConnFromSlice(opts, ucs)
	})
	return
}

type PieceStateChange struct {
	Index int
	PieceState
}

func (t *Torrent) deferPublishPieceStateChange(piece pieceIndex) {
	p := t.piece(piece)
	t.cl.unlockHandlers.changedPieceStates[p] = struct{}{}
}

func (t *Torrent) pieceNumPendingChunks(piece pieceIndex) pp.Integer {
	if t.pieceComplete(piece) {
		return 0
	}
	return pp.Integer(t.pieceNumChunks(piece) - t.pieces[piece].numDirtyChunks())
}

func (t *Torrent) pieceAllDirty(piece pieceIndex) bool {
	return t.pieces[piece].allChunksDirty()
}

func (t *Torrent) updateReaderPieces() {
	t._readerNowPieces, t._readerReadaheadPieces = t.readerPiecePriorities()
}

func (t *Torrent) readerPosChanged(from, to pieceRange) {
	if from == to {
		return
	}
	t.updateReaderPieces()
	l, h := from, to
	if l.begin > h.begin {
		l, h = h, l
	}
	if l.end < h.begin {
		t.updatePiecePriorities(l.begin, l.end, "Torrent.readerPosChanged")
		t.updatePiecePriorities(h.begin, h.end, "Torrent.readerPosChanged")
	} else {
		t.updatePiecePriorities(l.begin, max(l.end, h.end), "Torrent.readerPosChanged")
	}
}

func (t *Torrent) maybeNewConns() {
	t.cl.event.Broadcast()
	t.openNewConns()
}

func (t *Torrent) updatePeerRequestsForPiece(piece pieceIndex, reason updateRequestReason) {
	if !t._pendingPieces.Contains(uint32(piece)) {
		return
	}
	urgent := t.piece(piece).purePriority() == PiecePriorityNow && t.pieceHasUnrequestedChunks(piece)
	for c := range t.conns {
		func() {
			if !urgent && !c.isLowOnRequests() {
				return
			}
			if !c.peerHasPiece(piece) {
				return
			}
			if c.requestState.Interested && c.peerChoking && !c.peerAllowedFast.Contains(piece) {
				return
			}
			c.onNeedUpdateRequests(reason)
		}()
	}
}

func (t *Torrent) onPiecePendingTriggers(piece pieceIndex) {
	t.maybeNewConns()
	t.deferPublishPieceStateChange(piece)
	t.deferUpdateRegularTrackerAnnouncing()
}

func (t *Torrent) updatePendingPieces(piece pieceIndex) bool {
	p := t.piece(piece)
	newPrio := p.effectivePriority()
	if newPrio == PiecePriorityNone && p.haveHash() {
		return t._pendingPieces.CheckedRemove(uint32(piece))
	} else {
		return t._pendingPieces.CheckedAdd(uint32(piece))
	}
}

func (t *Torrent) updatePiecePriorityNoRequests(piece pieceIndex) (updateRequests bool) {
	if !t.closed.IsSet() {
		updateRequests = t.updatePieceRequestOrderPiece(piece) && t.hasStorageCap()
	}
	if t.updatePendingPieces(piece) {
		if !t.disableTriggers {
			t.onPiecePendingTriggers(piece)
		}
		updateRequests = true
	}
	return
}

func (t *Torrent) updatePiecePriority(piece pieceIndex, reason updateRequestReason) {
	changed := t.updatePiecePriorityNoRequests(piece)
	if (changed || t.piece(piece).purePriority() == PiecePriorityNow) && !t.disableTriggers {
		t.updatePeerRequestsForPiece(piece, reason)
	}
}

func (t *Torrent) updateAllPiecePriorities(reason updateRequestReason) {
	if !t.haveInfo() {
		return
	}
	t.updatePiecePriorities(0, t.numPieces(), reason)
}

func (t *Torrent) updatePiecePriorities(begin, end pieceIndex, reason updateRequestReason) {
	t.logger.Slogger().Debug("updating piece priorities", "begin", begin, "end", end)
	for i := begin; i < end; i++ {
		t.updatePiecePriority(i, reason)
	}
	t.logPieceRequestOrder()
}

func (t *Torrent) logPieceRequestOrder() {
	level := slog.LevelDebug
	logger := t.slogger()
	if !logger.Enabled(context.Background(), level) {
		return
	}
	pro := t.getPieceRequestOrder()
	if false {
		if pro != nil {
			for item := range pro.Iter {
				t.slogger().Debug("piece request order item",
					"infohash", item.Key.InfoHash,
					"piece", item.Key.Index,
					"state", item.State)
			}
		}
	}
}

func (t *Torrent) byteRegionPieces(off, size int64) (begin, end pieceIndex) {
	if off >= t.length() {
		return
	}
	if off < 0 {
		size += off
		off = 0
	}
	if size <= 0 {
		return
	}
	begin = pieceIndex(off / t.info.PieceLength)
	end = min(pieceIndex((off+size+t.info.PieceLength-1)/t.info.PieceLength), t.info.NumPieces())
	return
}

func (t *Torrent) forReaderOffsetPieces(f func(begin, end pieceIndex) (more bool)) (all bool) {
	for r := range t.readers {
		p := r.pieces
		if p.begin >= p.end {
			continue
		}
		if !f(p.begin, p.end) {
			return false
		}
	}
	return true
}

func (t *Torrent) readerRequests() []RequestIndex {
	t.readerAt = t.readerAt[:0]
	for r := range t.readers {
		if r.pieces.begin >= r.pieces.end || r.pos >= r.length {
			continue
		}
		if req, ok := t.offsetRequest(r.offset + r.pos); ok {
			t.readerAt = append(t.readerAt, t.requestIndexFromRequest(req))
		}
	}
	return t.readerAt
}

func (t *Torrent) pendRequest(req RequestIndex) {
	t.piece(t.pieceIndexOfRequestIndex(req)).pendChunkIndex(req % t.chunksPerRegularPiece())
}

func (t *Torrent) pieceCompletionChanged(piece pieceIndex, reason updateRequestReason) {
	t.cl.event.Broadcast()
	if t.pieceComplete(piece) {
		t.onPieceCompleted(piece)
	} else {
		t.onIncompletePiece(piece)
	}
	t.updatePiecePriority(piece, reason)
}

func (t *Torrent) numReceivedConns() (ret int) {
	for c := range t.conns {
		if c.Discovery == PeerSourceIncoming {
			ret++
		}
	}
	return
}

func (t *Torrent) numOutgoingConns() (ret int) {
	for c := range t.conns {
		if c.outgoing {
			ret++
		}
	}
	return
}

func (t *Torrent) maxHalfOpen() int {
	establishedHeadroom := int64(t.maxEstablishedConns - len(t.conns))
	extraIncoming := int64(t.numReceivedConns() - t.maxEstablishedConns/2)
	return int(min(
		max(5, extraIncoming)+establishedHeadroom,
		int64(t.cl.config.HalfOpenConnsPerTorrent),
	))
}

func (t *Torrent) openNewConns() (initiated int) {
	if len(t.cl.dialers) == 0 {
		return
	}
	if !t.wantOutgoingConns() {
		return
	}
	numPeers := t.peers.Len()
	for numPeers != 0 {
		if len(t.halfOpen) >= t.maxHalfOpen() {
			return
		}
		if t.cl.numHalfOpen >= t.cl.config.TotalHalfOpenConns {
			return
		}
		p := t.peers.PopMax()
		numPeers--
		if numPeers == t.cl.config.TorrentPeersLowWater {
			t.deferUpdateRegularTrackerAnnouncing()
		}
		opts := outgoingConnOpts{
			peerInfo:                 p,
			t:                        t,
			requireRendezvous:        false,
			skipHolepunchRendezvous:  false,
			receivedHolepunchConnect: false,
			HeaderObfuscationPolicy:  t.cl.config.HeaderObfuscationPolicy,
		}
		initiateConn(opts, false)
		initiated++
		if t.cl.check.Try() {
			panicif.NotEq(numPeers, t.peers.Len())
		}
	}
	return
}

func (t *Torrent) setPieceCompletion(piece pieceIndex, uncached g.Option[bool]) {
	changed := t.setCachedPieceCompletion(piece, uncached)
	t.afterSetPieceCompletion(piece, changed)
}

func (t *Torrent) setPieceCompletionFromStorage(piece pieceIndex) bool {
	changed := t.setCachedPieceCompletionFromStorage(piece)
	t.afterSetPieceCompletion(piece, changed)
	return changed
}

func (t *Torrent) setInitialPieceCompletionFromStorage(piece pieceIndex) {
	t.setCachedPieceCompletionFromStorage(piece)
	t.afterSetPieceCompletion(piece, true)
}

func (t *Torrent) setCachedPieceCompletionFromStorage(piece pieceIndex) bool {
	uncached := t.pieceCompleteUncached(piece)
	if uncached.Err != nil {
		t.slogger().Error("error getting piece completion", "err", uncached.Err)
		t.disallowDataDownloadLocked()
	}
	return t.setCachedPieceCompletion(piece, g.OptionFromTuple(uncached.Complete, uncached.Ok))
}

func (t *Torrent) setCachedPieceCompletion(piece int, uncached g.Option[bool]) bool {
	p := t.piece(piece)
	cached := p.completion()
	cachedOpt := g.OptionFromTuple(cached.Complete, cached.Ok)
	changed := cachedOpt != uncached
	if !p.storageCompletionHasBeenOk && uncached.Ok && !uncached.Value {
		t.sawInitiallyIncompleteData = true
		t.deferUpdateRegularTrackerAnnouncing()
	}
	p.storageCompletionOk = uncached.Ok
	if !p.storageCompletionHasBeenOk {
		p.storageCompletionHasBeenOk = p.storageCompletionOk
	}
	x := uint32(piece)
	if uncached.Ok && uncached.Value {
		if t._completedPieces.CheckedAdd(x) {
			if t.haveAllPieces() {
				t.deferUpdateRegularTrackerAnnouncing()
			}
		}
	} else {
		t._completedPieces.Remove(x)
	}
	return changed

}

func (t *Torrent) updatePieceCompletion(piece pieceIndex) bool {
	return t.setPieceCompletionFromStorage(piece)
}

func (t *Torrent) afterSetPieceCompletion(piece pieceIndex, changed bool) {
	p := t.piece(piece)
	cmpl := p.completion()
	complete := cmpl.Ok && cmpl.Complete
	p.t.updatePieceRequestOrderPiece(piece)
	t.deferUpdateComplete()
	if complete && len(p.dirtiers) != 0 {
		t.logger.Printf("marked piece %v complete but still has dirtiers", piece)
	}
	if changed {
		t.pieceCompletionChanged(piece, "Torrent.updatePieceCompletion")
	}
	if complete {
		t.openNewConns()
	}
}

func (t *Torrent) readAt(b []byte, off int64) (n int, err error) {
	r := t.storageReader()
	n, err = r.ReadAt(b, off)
	panicif.Err(r.Close())
	return
}

func (t *Torrent) maybeCompleteMetadata() error {
	if t.haveInfo() {
		return nil
	}
	if !t.haveAllMetadataPieces() {
		return nil
	}
	err := t.setInfoBytesLocked(t.metadataBytes)
	if err != nil {
		t.invalidateMetadata()
		return fmt.Errorf("error setting info bytes: %w", err)
	}
	if t.cl.config.Debug {
		t.logger.Printf("%s: got metadata from peers", t)
	}
	return nil
}

func (t *Torrent) readerPiecePriorities() (now, readahead bitmap.Bitmap) {
	t.forReaderOffsetPieces(func(begin, end pieceIndex) bool {
		if end > begin {
			now.Add(bitmap.BitIndex(begin))
			readahead.AddRange(bitmap.BitRange(begin)+1, bitmap.BitRange(end))
		}
		return true
	})
	return
}

func (t *Torrent) needData() bool {
	if t.closed.IsSet() {
		return false
	}
	if !t.haveInfo() {
		return true
	}
	t.checkPendingPiecesMatchesRequestOrder()
	return !t._pendingPieces.IsEmpty()
}

func appendMissingStrings(old, new []string) (ret []string) {
	ret = old
new:
	for _, n := range new {
		for _, o := range old {
			if o == n {
				continue new
			}
		}
		ret = append(ret, n)
	}
	return
}

func appendMissingTrackerTiers(existing [][]string, minNumTiers int) (ret [][]string) {
	ret = existing
	for minNumTiers > len(ret) {
		ret = append(ret, nil)
	}
	return
}

func (t *Torrent) addTrackers(announceList [][]string) {
	if t.isDropped() {
		return
	}
	fullAnnounceList := &t.announceList
	t.announceList = appendMissingTrackerTiers(*fullAnnounceList, len(announceList))
	for tierIndex, trackerURLs := range announceList {
		(*fullAnnounceList)[tierIndex] = appendMissingStrings((*fullAnnounceList)[tierIndex], trackerURLs)
	}
	t.startMissingTrackerScrapers()
	t.updateWantPeersEvent()
}

func (t *Torrent) bytesCompleted() int64 {
	if !t.haveInfo() {
		return 0
	}
	return t.length() - t.bytesLeft()
}

func (t *Torrent) SetInfoBytes(b []byte) (err error) {
	t.cl.lock()
	defer t.cl.unlock()
	err = t.getClosedErr()
	if err != nil {
		return
	}
	return t.setInfoBytesLocked(b)
}

func (t *Torrent) deletePeerConn(c *PeerConn) (ret bool) {
	if !c.closed.IsSet() {
		panic("connection is not closed")
	}
	_, ret = t.conns[c]
	delete(t.conns, c)
	if ret {
		if !t.cl.config.DisablePEX {
			t.pex.Drop(c)
		}
	}
	c.deleteAllRequests("Torrent.deletePeerConn")
	if len(t.conns) == 0 {
		panicif.NotZero(len(t.requestState))
	}
	t.assertPendingRequests()
	if t.numActivePeers() == 0 && len(t.connsWithAllPieces) != 0 {
		panic(t.connsWithAllPieces)
	}
	return
}

func (t *Torrent) decPeerPieceAvailability(p *Peer) {
	if t.deleteConnWithAllPieces(p) {
		return
	}
	if !t.haveInfo() {
		return
	}
	p.peerPieces().Iterate(func(i uint32) bool {
		p.t.decPieceAvailability(pieceIndex(i))
		return true
	})
}

func (t *Torrent) assertPendingRequests() {
	if !check.Enabled {
		return
	}
}

func (t *Torrent) dropConnection(c *PeerConn) {
	t.cl.event.Broadcast()
	c.close()

	for _, cb := range c.callbacks.StatusUpdated {
		cb(StatusUpdatedEvent{
			Event:  PeerDisconnected,
			PeerId: c.PeerID,
		})
	}
	t.logger.WithDefaultLevel(log.Debug).Printf("dropping connection to %+q, sent peerconn update", c.PeerID)

	if t.deletePeerConn(c) {
		t.openNewConns()
	}
}

func (t *Torrent) wantPeers() bool {
	if t.closed.IsSet() {
		return false
	}
	return t.peers.Len() <= t.cl.config.TorrentPeersLowWater
}

func (t *Torrent) updateWantPeersEvent() {
	t.deferUpdateRegularTrackerAnnouncing()
}

func (t *Torrent) deferUpdateRegularTrackerAnnouncing() {
	t.cl.unlockHandlers.deferUpdateTorrentRegularTrackerAnnouncing(t)
}

func (t *Torrent) updateRegularTrackerAnnouncing() {
	t.cl.regularTrackerAnnounceDispatcher.updateTorrentInput(t)
}

func (t *Torrent) seeding() bool {
	cl := t.cl
	if t.closed.IsSet() {
		return false
	}
	if t.dataUploadDisallowed {
		return false
	}
	if cl.config.NoUpload {
		return false
	}
	if !cl.config.Seed {
		return false
	}
	if cl.config.StopSeedingWhenComplete && !t.needData() {
		return false
	}
	if cl.config.DisableAggressiveUpload && t.needData() {
		return false
	}
	return true
}

func (t *Torrent) logRunHandshookConn(pc *PeerConn, logAll bool, level log.Level) {
	err := t.runHandshookConn(pc)
	if err != nil || logAll {
		t.logger.WithDefaultLevel(level).Levelf(log.ErrorLevel(err), "error running handshook conn: %v", err)
	}
}

func (t *Torrent) runHandshookConnLoggingErr(pc *PeerConn) {
	t.logRunHandshookConn(pc, false, log.Debug)
}

func (t *Torrent) startScrapingTracker(_url string) {
	if _url == "" {
		return
	}
	u, err := url.Parse(_url)
	if err != nil {
		if _url[0] != '*' {
			t.logger.Levelf(log.Warning, "error parsing tracker url: %v", err)
		}
		return
	}
	switch u.Scheme {
	case "http", "https", "udp", "udp4", "udp6":
	default:
		t.logger.Levelf(log.Debug, "skipping tracker with unsupported scheme: %q", _url)
		return
	}
	if u.User != nil || u.Host == "" {
		t.logger.Levelf(log.Debug, "skipping tracker url: %q", _url)
		return
	}
	if canonical := u.String(); canonical != _url {
		if again, err := url.Parse(canonical); err == nil && again.String() == canonical {
			t.startScrapingTracker(canonical)
		}
		return
	}
	if u.Scheme == "udp" {
		u.Scheme = "udp4"
		t.startScrapingTracker(u.String())
		u.Scheme = "udp6"
		t.startScrapingTracker(u.String())
		return
	}
	announcerKey := trackerAnnouncerKey(_url)
	for ih := range t.iterShortInfohashes() {
		t.startScrapingTrackerWithInfohash(u, announcerKey, ih)
	}
}

func (t *Torrent) startScrapingTrackerWithInfohash(u *url.URL, urlStr trackerAnnouncerKey, shortInfohash [20]byte) {
	announcerKey := torrentTrackerAnnouncerKey{
		ShortInfohash: shortInfohash,
		url:           urlStr,
	}
	if _, ok := t.trackerAnnouncers[announcerKey]; ok {
		return
	}
	sl := func() torrentTrackerAnnouncer {
		switch u.Scheme {
		case "udp4":
			if t.cl.config.DisableIPv4 {
				return nil
			}
		case "udp6":
			if t.cl.config.DisableIPv6 {
				return nil
			}
		}
		if err := t.cl.startTrackerAnnouncer(u, urlStr); err != nil {
			t.logger.Levelf(log.Warning, "can't announce to %v: %v", urlStr, err)
			return nil
		}
		t.initRegularTrackerAnnounceState(announcerKey)
		return torrentRegularTrackerAnnouncer{
			u: u,
			getAnnounceState: func() announceState {
				return *t.regularTrackerAnnounceState[announcerKey]
			},
		}
	}()
	if sl == nil {
		return
	}
	g.MakeMapIfNil(&t.trackerAnnouncers)
	if g.MapInsert(t.trackerAnnouncers, announcerKey, sl).Ok {
		panic("tracker announcer already exists")
	}
}

func (t *Torrent) initRegularTrackerAnnounceState(key torrentTrackerAnnouncerKey) {
	g.MakeMapIfNil(&t.regularTrackerAnnounceState)
	t.cl.regularTrackerAnnounceDispatcher.addKey(key)
	t.regularTrackerAnnounceState[key] = t.cl.regularTrackerAnnounceDispatcher.announceStates[key]
	t.deferUpdateRegularTrackerAnnouncing()
}

func (t *Torrent) startMissingTrackerScrapers() {
	if t.cl.config.DisableTrackers {
		return
	}
	for _, tier := range t.announceList {
		for _, url := range tier {
			t.startScrapingTracker(url)
		}
	}
}

func (t *Torrent) announceRequest(
	event tracker.AnnounceEvent,
	shortInfohash [20]byte,
) tracker.AnnounceRequest {
	return tracker.AnnounceRequest{
		Event: event,
		NumWant: func() int32 {
			if t.wantPeers() && len(t.cl.dialers) > 0 {
				return 200
			} else {
				return 0
			}
		}(),
		Port:     uint16(t.cl.incomingPeerPort()),
		PeerId:   t.cl.peerID,
		InfoHash: shortInfohash,
		Key:      t.cl.announceKey(),

		Left:       t.bytesLeftAnnounce(),
		Uploaded:   t.connStats.BytesWrittenData.Int64(),
		Downloaded: t.connStats.BytesReadUsefulData.Int64(),
	}
}

func (t *Torrent) consumeDhtAnnouncePeers(pvs <-chan dht.PeersValues) {
	cl := t.cl
	for v := range pvs {
		peerInfos := make([]PeerInfo, 0, len(v.Peers))
		for _, cp := range v.Peers {
			if cp.Port == 0 {
				continue
			}
			peerInfos = append(peerInfos, PeerInfo{
				Addr:   ipPortAddr{cp.IP, cp.Port},
				Source: PeerSourceDhtGetPeers,
			})
		}
		if len(peerInfos) > 0 {
			cl.lock()
			t.addPeers(peerInfos)
			cl.unlock()

		}
	}
}

func (t *Torrent) AnnounceToDht(s DhtServer) (done <-chan struct{}, stop func(), err error) {
	var ihs [][20]byte
	t.cl.lock()
	t.eachShortInfohash(func(short [20]byte) {
		ihs = append(ihs, short)
	})
	t.cl.unlock()
	ctx, stop := context.WithCancel(context.Background())
	eg, ctx := errgroup.WithContext(ctx)
	for _, ih := range ihs {
		var ann DhtAnnounce
		ann, err = s.Announce(ih, t.cl.incomingPeerPort(), true)
		if err != nil {
			stop()
			return
		}
		eg.Go(func() error {
			return t.dhtAnnounceConsumer(ctx, ann)
		})
	}
	_done := make(chan struct{})
	done = _done
	go func() {
		defer stop()
		defer close(_done)
		eg.Wait()
	}()
	return
}

func (t *Torrent) dhtAnnounceConsumer(
	ctx context.Context,
	ps DhtAnnounce,
) (
	err error,
) {
	defer ps.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		t.consumeDhtAnnouncePeers(ps.Peers())
	}()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-done:
		return nil
	}
}

func (t *Torrent) timeboxedAnnounceToDht(s DhtServer) error {
	_, stop, err := t.AnnounceToDht(s)
	if err != nil {
		return err
	}
	select {
	case <-t.closed.Done():
	case <-time.After(15 * time.Minute):
	}
	stop()
	return nil
}

func (t *Torrent) dhtAnnouncer(s DhtServer) {
	cl := t.cl
	cl.lock()
	defer cl.unlock()
	for {
		for {
			if t.closed.IsSet() {
				return
			}
			if !t.wantAnyConns() {
				goto wait
			}
			if len(cl.dialers) == 0 && len(cl.listeners) == 0 {
				goto wait
			}
			break
		wait:
			cl.event.Wait()
		}
		func() {
			t.numDHTAnnounces++
			cl.unlock()
			defer cl.lock()
			err := t.timeboxedAnnounceToDht(s)
			if err != nil {
				t.logger.WithDefaultLevel(log.Warning).Printf("error announcing %q to DHT: %s", t, err)
				select {
				case <-time.After(5 * time.Minute):
				case <-t.closed.Done():
				}
			}
		}()
	}
}

func (t *Torrent) addPeers(peers []PeerInfo) (added int) {
	return t.addPeersIter(slices.Values(peers))
}

func (t *Torrent) addPeersIter(peers iter.Seq[PeerInfo]) (added int) {
	wantPeers := t.wantPeers()
	for p := range peers {
		if t.addPeer(p) {
			added++
		}
	}
	if t.wantPeers() != wantPeers {
		t.deferUpdateRegularTrackerAnnouncing()
	}
	return
}

func (t *Torrent) Stats() TorrentStats {
	t.cl.rLock()
	defer t.cl.rUnlock()
	return t.statsLocked()
}

func (t *Torrent) gauges() (ret TorrentGauges) {
	ret.ActivePeers = len(t.conns)
	ret.HalfOpenPeers = len(t.halfOpen)
	ret.PendingPeers = t.peers.Len()
	ret.TotalPeers = t.numTotalPeers()
	ret.ConnectedSeeders = 0
	for c := range t.conns {
		if c.closed.IsSet() {
			continue
		}
		if all, ok := c.peerHasAllPieces(); all && ok {
			ret.ConnectedSeeders++
		}
	}
	ret.PiecesComplete = t.numPiecesCompleted()
	return
}

func (t *Torrent) statsLocked() (ret TorrentStats) {
	ret.AllConnStats = t.connStats.Copy()
	ret.TorrentStatCounters = copyCountFields(&t.counters)
	ret.TorrentGauges = t.gauges()
	return
}

func (t *Torrent) numTotalPeers() int {
	peers := make(map[string]struct{}, len(t.conns)+len(t.halfOpen)+t.peers.Len())
	for conn := range t.conns {
		ra := conn.RemoteAddr
		if ra == nil {
			continue
		}
		peers[ra.String()] = struct{}{}
	}
	for addr := range t.halfOpen {
		peers[addr] = struct{}{}
	}
	t.peers.Each(func(peer PeerInfo) {
		peers[peer.Addr.String()] = struct{}{}
	})
	return len(peers)
}

func (t *Torrent) addPeerConn(c *PeerConn) (err error) {
	if t.closed.IsSet() {
		return errTorrentClosed
	}
	for c0 := range t.conns {
		if c.PeerID != c0.PeerID {
			continue
		}
		if !t.cl.config.DropDuplicatePeerIds {
			continue
		}
		if c.hasPreferredNetworkOver(c0) {
			c0.close()
			t.deletePeerConn(c0)
		} else {
			return errors.New("existing connection preferred")
		}
	}
	if len(t.conns) >= t.maxEstablishedConns {
		numOutgoing := t.numOutgoingConns()
		numIncoming := len(t.conns) - numOutgoing
		c := t.worstBadConn(worseConnLensOpts{
			incomingIsBad: (numIncoming-numOutgoing > 1) && c.outgoing,
			outgoingIsBad: (numOutgoing-numIncoming > 1) && !c.outgoing,
		})
		if c == nil {
			return errors.New("don't want conn")
		}
		c.close()
		t.deletePeerConn(c)
	}
	if len(t.conns) >= t.maxEstablishedConns {
		panic(len(t.conns))
	}
	t.conns[c] = struct{}{}
	t.cl.event.Broadcast()
	if !t.cl.config.DisablePEX && !c.PeerExtensionBytes.SupportsExtended() {
		t.pex.Add(c)
	}
	return nil
}

func (t *Torrent) newConnsAllowed() bool {
	if !t.networkingEnabled.Bool() {
		return false
	}
	if t.closed.IsSet() {
		return false
	}
	if EffectiveDownloadRateLimit(t.cl.config.DownloadRateLimiter) == 0 {
		return false
	}
	if t.needData() {
		return true
	}
	return t.seeding() && t.haveAnyPieces()
}

func (t *Torrent) wantAnyConns() bool {
	if !t.newConnsAllowed() {
		return false
	}
	return len(t.conns) < t.maxEstablishedConns
}

func (t *Torrent) wantOutgoingConns() bool {
	if !t.newConnsAllowed() {
		return false
	}
	if len(t.conns) < t.maxEstablishedConns {
		return true
	}
	numIncomingConns := len(t.conns) - t.numOutgoingConns()
	return t.worstBadConn(worseConnLensOpts{
		incomingIsBad: numIncomingConns-t.numOutgoingConns() > 1,
		outgoingIsBad: false,
	}) != nil
}

func (t *Torrent) wantIncomingConns() bool {
	if !t.newConnsAllowed() {
		return false
	}
	if len(t.conns) < t.maxEstablishedConns {
		return true
	}
	numIncomingConns := len(t.conns) - t.numOutgoingConns()
	return t.worstBadConn(worseConnLensOpts{
		incomingIsBad: false,
		outgoingIsBad: t.numOutgoingConns()-numIncomingConns > 1,
	}) != nil
}

func (t *Torrent) SetMaxEstablishedConns(max int) (oldMax int) {
	t.cl.lock()
	defer t.cl.unlock()
	oldMax = t.maxEstablishedConns
	t.maxEstablishedConns = max
	wcs := worseConnSlice{
		conns: t.appendConns(nil, func(*PeerConn) bool {
			return true
		}),
	}
	wcs.initKeys(worseConnLensOpts{})
	heap.Init(&wcs)
	for len(t.conns) > t.maxEstablishedConns && wcs.Len() > 0 {
		t.dropConnection(heap.Pop(&wcs).(*PeerConn))
	}
	t.openNewConns()
	return oldMax
}

func (t *Torrent) pieceHashed(piece pieceIndex, passed bool, hashIoErr error) {
	p := t.piece(piece)
	p.numVerifies++
	p.numVerifiesCond.Broadcast()
	t.cl.event.Broadcast()
	if t.closed.IsSet() {
		return
	}

	if t.initialPieceCheckDisabled || p.numVerifies != 1 {
		if passed {
		} else {
			log.Fmsg(
				"piece %d failed hash: %d connections contributed", piece, len(p.dirtiers),
			).AddValues(t, p).LogLevel(log.Info, t.logger)
		}
	}

	p.marking = true
	t.deferPublishPieceStateChange(piece)
	defer func() {
		p.marking = false
		t.deferPublishPieceStateChange(piece)
	}()

	if passed {
		t.incrementPiecesDirtiedStats(p, (*ConnStats).incrementPiecesDirtiedGood)
		t.clearPieceTouchers(piece)
		logger := t.slogger()
		t.cl.unlock()
		p.race++
		err := p.Storage().MarkComplete()
		if err != nil {
			logger.Error("error marking piece complete", "piece", piece, "err", err)
		}
		t.cl.lock()

		if t.closed.IsSet() {
			t.cl.unlock()
			p.Storage().MarkNotComplete()
			t.cl.lock()
			return
		}
		t.pendAllChunkSpecs(piece)
		t.setPieceCompletion(piece, g.Some(true))
	} else {
		if len(p.dirtiers) != 0 && p.allChunksDirty() && hashIoErr == nil {
			t.incrementPiecesDirtiedStats(p, (*ConnStats).incrementPiecesDirtiedBad)

			bannableTouchers := make([]*Peer, 0, len(p.dirtiers))
			for c := range p.dirtiers {
				if !c.trusted {
					bannableTouchers = append(bannableTouchers, c)
				}
			}
			t.clearPieceTouchers(piece)
			slices.SortFunc(bannableTouchers, comparePeerTrust)

			if t.cl.config.Debug {
				t.logger.Printf(
					"bannable conns by trust for piece %d: %v",
					piece,
					func() (ret []connectionTrust) {
						for _, c := range bannableTouchers {
							ret = append(ret, c.trust())
						}
						return
					}(),
				)
			}

			if len(bannableTouchers) >= 1 {
				c := bannableTouchers[0]
				if len(bannableTouchers) != 1 {
					t.logger.Levelf(log.Debug, "would have banned %v for touching piece %v after failed piece check", c.remoteIp(), piece)
				} else {
					t.slogger().Info(
						"piece failed hash. banning peer",
						"piece", piece,
						"peer", c)
					c.providedBadData()
				}
			}
		}

		logger := t.slogger()
		t.cl.unlock()
		p.race++
		err := p.Storage().MarkNotComplete()
		if err != nil {
			logger.Error("error marking piece not complete", "piece", piece, "err", err)
		}
		t.cl.lock()
		if t.closed.IsSet() {
			return
		}
		t.onIncompletePiece(piece)
		t.setPieceCompletion(p.index, g.Some(false))
	}
}

func (t *Torrent) cancelRequestsForPiece(piece pieceIndex) {
	start := t.pieceRequestIndexBegin(piece)
	end := start + t.pieceNumChunks(piece)
	for ri := start; ri < end; ri++ {
		t.cancelRequest(ri)
	}
}

func (t *Torrent) onPieceCompleted(piece pieceIndex) {
	t.pendAllChunkSpecs(piece)
	t.cancelRequestsForPiece(piece)
	t.piece(piece).readerCond.Broadcast()
	for conn := range t.conns {
		conn.have(piece)
		t.maybeDropMutuallyCompletePeer(conn)
	}
}

func (t *Torrent) onIncompletePiece(piece pieceIndex) {
	if t.pieceAllDirty(piece) {
		t.pendAllChunkSpecs(piece)
	}
	if t.ignorePieceForRequests(piece) {
		return
	}
	t.iterPeers(func(conn *Peer) {
		if conn.peerHasPiece(piece) {
			conn.onNeedUpdateRequests("piece incomplete")
		}
	})
}

func (t *Torrent) startPieceHashers() error {
	if t.closed.IsSet() {
		return errTorrentClosed
	}
	for t.considerStartingHashers() {
		if !t.startSinglePieceHasher() {
			break
		}
	}
	return nil
}

func (t *Torrent) startSinglePieceHasher() bool {
	pi := t.getPieceToHash()
	if !pi.Ok {
		return false
	}
	t.startHash(pi.Value)
	go t.pieceHasher(pi.Value)
	return true
}

func (t *Torrent) pieceHasher(initial pieceIndex) {
	t.finishHash(initial)
	for {
		piOpt := t.getPieceToHash()
		if !piOpt.Ok {
			break
		}
		pi := piOpt.Value
		t.startHash(pi)
		t.cl.unlock()
		t.finishHash(pi)
	}
	t.cl.startPieceHashers()
	t.cl.unlock()
}

func (t *Torrent) startHash(pi pieceIndex) {
	p := t.piece(pi)
	t.piecesQueuedForHash.Remove(pi)
	p.hashing = true
	t.deferPublishPieceStateChange(pi)
	t.updatePiecePriority(pi, "Torrent.startHash")
	t.storageLock.RLock()
	t.activePieceHashes++
	t.cl.activePieceHashers++
}

func (t *Torrent) getPieceToHash() (ret g.Option[pieceIndex]) {
	if t.closed.IsSet() {
		return
	}
	var best PiecePriority
	for i := range t.piecesQueuedForHash.Iterate {
		p := t.piece(i)
		if p.hashing || p.marking {
			continue
		}
		if prio := p.purePriority(); !ret.Ok || prio > best {
			ret, best = g.Some(i), prio
			if prio == PiecePriorityNow {
				break
			}
		}
	}
	return
}

func (t *Torrent) dropBannedPeers() {
	t.iterPeers(func(p *Peer) {
		remoteIp := p.remoteIp()
		if remoteIp == nil {
			if p.bannableAddr.Ok {
				t.logger.WithDefaultLevel(log.Debug).Printf("can't get remote ip for peer %v", p)
			}
			return
		}
		netipAddr := netip.MustParseAddr(remoteIp.String())
		if Some(netipAddr) != p.bannableAddr {
			t.logger.WithDefaultLevel(log.Debug).Printf(
				"peer remote ip does not match its bannable addr [peer=%v, remote ip=%v, bannable addr=%v]",
				p, remoteIp, p.bannableAddr)
		}
		if _, ok := t.cl.badPeerIPs[netipAddr.Unmap()]; ok {
			p.drop()
			t.logger.WithDefaultLevel(log.Debug).Printf("dropped %v for banned remote IP %v", p, netipAddr)
		}
	})
}

func (t *Torrent) finishHash(index pieceIndex) {
	p := t.piece(index)
	correct, failedPeers, copyErr := t.hashPiece(index)
	t.storageLock.RUnlock()
	level := slog.LevelDebug
	switch copyErr {
	case nil, io.EOF:
	default:
		level = slog.LevelWarn
	}
	t.cl.lock()
	t.slogger().Log(context.Background(), level, "finished hashing piece",
		"piece", index,
		"correct", correct,
		"failedPeers", failedPeers,
		"err", copyErr)
	if correct {
		if len(failedPeers) > 0 {
			for peer := range failedPeers {
				t.cl.banPeerIP(peer.AsSlice())
				t.slogger().Info("smart banned peer", "peer", peer, "piece", index)
			}
			t.dropBannedPeers()
		}
		t.smartBanCache.ForgetBlockSeq(iterRange(t.pieceRequestIndexBegin(index), t.pieceRequestIndexBegin(index+1)))
	}
	p.hashing = false
	t.pieceHashed(index, correct, copyErr)
	t.updatePiecePriority(index, "Torrent.finishHash")
	t.activePieceHashes--
	if t.activePieceHashes == 0 {
		t.deferUpdateComplete()
	}
	t.cl.activePieceHashers--
}

func (t *Torrent) clearPieceTouchers(pi pieceIndex) {
	p := t.piece(pi)
	for c := range p.dirtiers {
		delete(c.peerTouchedPieces, pi)
		delete(p.dirtiers, c)
	}
}

func (t *Torrent) queueInitialPieceCheck(i pieceIndex) {
	if t.initialPieceCheckDisabled {
		return
	}
	p := t.piece(i)
	if p.numVerifies != 0 {
		return
	}
	if p.hashing {
		return
	}
	if p.storageCompletionOk {
		return
	}
	_, _ = t.queuePieceCheck(i)
}

func (t *Torrent) queuePieceCheck(pieceIndex pieceIndex) (targetVerifies pieceVerifyCount, err error) {
	piece := t.piece(pieceIndex)
	if !piece.haveHash() {
		err = errors.New("piece hash unknown")
		return
	}
	targetVerifies = piece.nextNovelHashCount()
	if piece.queuedForHash() {
		return
	}
	t.piecesQueuedForHash.Add(pieceIndex)
	t.deferUpdateComplete()
	t.deferPublishPieceStateChange(pieceIndex)
	t.updatePiecePriority(pieceIndex, "Torrent.queuePieceCheck")
	err = t.startPieceHashers()
	return
}

func (t *Torrent) VerifyData() error {
	return t.VerifyDataContext(context.Background())
}

func (t *Torrent) VerifyDataContext(ctx context.Context) error {
	n := t.NumPieces()
	targets := make([]pieceVerifyCount, n)
	queue := func(i int) error {
		t.cl.lock()
		defer t.cl.unlock()
		if t.closed.IsSet() {
			return errTorrentClosed
		}
		target, err := t.queuePieceCheck(i)
		if err != nil {
			return fmt.Errorf("verifying piece %v: %w", i, err)
		}
		targets[i] = target
		return nil
	}
	for i := range min(verifyWindow, n) {
		if err := queue(i); err != nil {
			return err
		}
	}
	for i := range n {
		if err := t.piece(i).waitVerified(ctx, targets[i]); err != nil {
			return fmt.Errorf("verifying piece %v: %w", i, err)
		}
		if next := i + verifyWindow; next < n {
			if err := queue(next); err != nil {
				return err
			}
		}
	}
	return nil
}

const verifyWindow = 8

func (t *Torrent) connectingToPeerAddr(addrStr string) bool {
	return len(t.halfOpen[addrStr]) != 0
}

func (t *Torrent) hasPeerConnForAddr(x PeerRemoteAddr) bool {
	addrStr := x.String()
	for c := range t.conns {
		if ra := c.RemoteAddr; ra != nil && ra.String() == addrStr {
			return true
		}
	}
	return false
}

func (t *Torrent) getHalfOpenPath(
	addrStr string,
	attemptKey outgoingConnAttemptKey,
) nestedmaps.Path[*PeerInfo] {
	return nestedmaps.Next(nestedmaps.Next(nestedmaps.Begin(&t.halfOpen), addrStr), attemptKey)
}

func (t *Torrent) addHalfOpen(addrStr string, attemptKey *PeerInfo) {
	path := t.getHalfOpenPath(addrStr, attemptKey)
	if path.Exists() {
		panic("should be unique")
	}
	path.Set(attemptKey)
	t.cl.numHalfOpen++
}

func initiateConn(
	opts outgoingConnOpts,
	ignoreLimits bool,
) {
	t := opts.t
	peer := opts.peerInfo
	if peer.Id == t.cl.peerID {
		return
	}
	if t.cl.badPeerAddr(peer.Addr) && !peer.Trusted {
		return
	}
	addr := peer.Addr
	addrStr := addr.String()
	if !ignoreLimits {
		if t.connectingToPeerAddr(addrStr) {
			return
		}
	}
	if t.hasPeerConnForAddr(addr) {
		return
	}
	attemptKey := &peer
	t.addHalfOpen(addrStr, attemptKey)
	go t.cl.outgoingConnection(
		opts,
		attemptKey,
	)
}

func (t *Torrent) AddClientPeer(cl *Client) int {
	return t.AddPeers(func() (ps []PeerInfo) {
		for _, la := range cl.ListenAddrs() {
			ps = append(ps, PeerInfo{
				Addr:    la,
				Trusted: true,
			})
		}
		return
	}())
}

func (t *Torrent) hashingPiece(i pieceIndex) bool {
	return t.pieces[i].hashing
}

func (t *Torrent) pieceQueuedForHash(i pieceIndex) bool {
	return t.piecesQueuedForHash.Contains(i)
}

func (t *Torrent) dialTimeout() time.Duration {
	return reducedDialTimeout(t.cl.config.MinDialTimeout, t.cl.config.NominalDialTimeout, t.cl.config.HalfOpenConnsPerTorrent, t.peers.Len())
}

func (t *Torrent) piece(i int) *Piece {
	return &t.pieces[i]
}

func (t *Torrent) pieceForOffset(off int64) *Piece {
	return &t.pieces[off/t.info.PieceLength]
}

func (t *Torrent) onWriteChunkErr(err error) {
	if t.userOnWriteChunkErr != nil {
		go t.userOnWriteChunkErr(err)
		return
	}
	t.logger.WithDefaultLevel(log.Critical).Printf("default chunk write error handler: disabling data download")
	t.disallowDataDownloadLocked()
}

func (t *Torrent) UpdateRequests() {
	t.cl.lock()
	defer t.cl.unlock()
	t.iterPeers(func(p *Peer) {
		p.onNeedUpdateRequests("Torrent.UpdateRequests")
	})
}

func (t *Torrent) DisallowDataDownload() {
	t.cl.lock()
	defer t.cl.unlock()
	t.disallowDataDownloadLocked()
}

func (t *Torrent) disallowDataDownloadLocked() {
	t.dataDownloadDisallowed.Set()
	t.iterPeers(func(p *Peer) {
		p.onNeedUpdateRequests("disallow data download")
		p.cancelAllRequests()
	})
}

func (t *Torrent) AllowDataDownload() {
	t.cl.lock()
	defer t.cl.unlock()
	if !t.dataDownloadDisallowed.Clear() {
		return
	}
	t.updateAllPiecePriorities("data download allowed")
	t.iterPeers(func(p *Peer) {
		p.onNeedUpdateRequests("allow data download")
	})
}

func (t *Torrent) AllowDataUpload() {
	t.cl.lock()
	defer t.cl.unlock()
	if !t.dataUploadDisallowed {
		return
	}
	t.dataUploadDisallowed = false
	t.iterPeers(func(p *Peer) {
		p.onNeedUpdateRequests("allow data upload")
	})
}

func (t *Torrent) DisallowDataUpload() {
	t.cl.lock()
	defer t.cl.unlock()
	t.dataUploadDisallowed = true
	for c := range t.conns {
		c.onNeedUpdateRequests("disallow data upload")
	}
}

func (t *Torrent) SetOnWriteChunkError(f func(error)) {
	t.cl.lock()
	defer t.cl.unlock()
	t.userOnWriteChunkErr = f
}

func (t *Torrent) iterPeers(f func(p *Peer)) {
	for pc := range t.conns {
		f(&pc.Peer)
	}
	for _, ws := range t.webSeeds {
		f(&ws.peer)
	}
}

func (t *Torrent) callbacks() *Callbacks {
	return &t.cl.config.Callbacks
}

type AddWebSeedsOpt func(*webseed.Client)

func (t *Torrent) AddWebSeeds(urls []string, opts ...AddWebSeedsOpt) {
	t.cl.lock()
	defer t.cl.unlock()
	for _, u := range urls {
		t.addWebSeed(u, opts...)
	}
}

func (t *Torrent) addWebSeed(url string, opts ...AddWebSeedsOpt) bool {
	if t.cl.config.DisableWebseeds {
		return false
	}
	urlKey := webseedUrlKey(unique.Make(url))
	if _, ok := t.webSeeds[urlKey]; ok {
		return false
	}
	const defaultMaxRequests = 2
	ws := webseedPeer{
		peer: Peer{
			cl:         t.cl,
			t:          t,
			outgoing:   true,
			Network:    "http",
			RemoteAddr: remoteAddrFromUrl(url),
			callbacks:  t.callbacks(),
		},
		client: webseed.Client{
			HttpClient:              t.cl.httpClient,
			Url:                     url,
			MaxRequests:             defaultMaxRequests,
			ResponseBodyRateLimiter: t.cl.config.DownloadRateLimiter,
		},
		hostKey: t.deriveWebSeedHostKey(url),
		url:     urlKey,
	}
	ws.peer.initClosedCtx()
	for _, opt := range opts {
		opt(&ws.client)
	}
	setDefaultDownloadRateLimiterBurstIfZero(ws.client.ResponseBodyRateLimiter)
	ws.client.ResponseBodyWrapper = func(r io.Reader, interrupt func()) io.Reader {
		r = newIdleTimeoutReader(r, 30*time.Second, interrupt)
		return newRateLimitedReader(r, ws.client.ResponseBodyRateLimiter)
	}
	g.MakeMapWithCap(&ws.activeRequests, ws.client.MaxRequests)
	ws.locker = t.cl.locker()
	for _, f := range t.callbacks().NewPeer {
		f(&ws.peer)
	}
	ws.peer.logger = t.logger.WithContextValue(&ws).WithNames("webseed")
	ws.peer.slogger = t.slogger().With("webseed", url)
	ws.client.Logger = ws.peer.slogger
	ws.peer.legacyPeerImpl = &ws
	ws.peer.peerImpl = &ws
	if t.haveInfo() {
		ws.onGotInfo(t.info)
	}
	g.MapMustAssignNew(t.webSeeds, urlKey, &ws)
	ws.peer.onNeedUpdateRequests("Torrent.addWebSeed")
	return true
}

func (t *Torrent) deriveWebSeedHostKey(urlStr string) (ret webseedHostKeyHandle) {
	u, err := url.Parse(urlStr)
	if err != nil {
		t.slogger().Warn("error parsing webseed URL", "url", urlStr, "err", err)
		return unique.Make(webseedHostKey(urlStr))
	}
	return unique.Make(webseedHostKey(u.Hostname()))
}

func (t *Torrent) peerIsActive(p *Peer) (active bool) {
	t.iterPeers(func(p1 *Peer) {
		if p1 == p {
			active = true
		}
	})
	return
}

func (t *Torrent) requestIndexToRequest(ri RequestIndex) Request {
	index := t.pieceIndexOfRequestIndex(ri)
	return Request{
		Index:     pp.Integer(index),
		ChunkSpec: t.chunkIndexSpec(index, ri%t.chunksPerRegularPiece()),
	}
}

func (t *Torrent) requestIndexFromRequest(r Request) RequestIndex {
	return t.pieceRequestIndexBegin(pieceIndex(r.Index)) + RequestIndex(r.Begin/t.chunkSize)
}

func (t *Torrent) pieceRequestIndexBegin(piece pieceIndex) RequestIndex {
	return RequestIndex(piece) * t.chunksPerRegularPiece()
}

func (t *Torrent) deferUpdateComplete() {
	t.cl.unlockHandlers.addUpdateComplete(t)
}

func (t *Torrent) updateComplete() {
	t.complete.SetBool(t.isComplete())
}

func (t *Torrent) isComplete() bool {
	if t.activePieceHashes != 0 {
		return false
	}
	if !t.piecesQueuedForHash.IsEmpty() {
		return false
	}
	if !t.haveAllPieces() {
		return false
	}
	return true
}

func (t *Torrent) cancelRequest(r RequestIndex) *PeerConn {
	p := t.requestingPeer(r)
	if p != nil {
		p.cancel(r)
	}
	if _, ok := t.requestState[r]; ok {
		panic("expected request state to be gone")
	}
	return p
}

func (t *Torrent) requestingPeer(r RequestIndex) (ret *PeerConn) {
	state, ok := t.requestState[r]
	if !ok {
		return nil
	}
	ret = state.peer.Value()
	panicif.Nil(ret)
	return
}

func (t *Torrent) addConnWithAllPieces(p *Peer) {
	if t.connsWithAllPieces == nil {
		t.connsWithAllPieces = make(map[*Peer]struct{}, t.maxEstablishedConns)
	}
	t.connsWithAllPieces[p] = struct{}{}
}

func (t *Torrent) deleteConnWithAllPieces(p *Peer) bool {
	_, ok := t.connsWithAllPieces[p]
	delete(t.connsWithAllPieces, p)
	return ok
}

func (t *Torrent) numActivePeers() int {
	return len(t.conns) + len(t.webSeeds)
}

func (t *Torrent) hasStorageCap() bool {
	f := t.storage.Capacity
	if f == nil {
		return false
	}
	_, ok := (*f)()
	return ok
}

func (t *Torrent) pieceIndexOfRequestIndex(ri RequestIndex) pieceIndex {
	return pieceIndex(ri / t.chunksPerRegularPiece())
}

func (t *Torrent) iterUndirtiedRequestIndexesInPiece(
	reuseIter *typedRoaring.Iterator[RequestIndex],
	piece pieceIndex,
	f func(RequestIndex),
) {
	reuseIter.Initialize(&t.dirtyChunks)
	pieceRequestIndexOffset := t.pieceRequestIndexBegin(piece)
	iterBitmapUnsetInRange(
		reuseIter,
		pieceRequestIndexOffset, pieceRequestIndexOffset+t.pieceNumChunks(piece),
		f,
	)
}

func (t *Torrent) pieceHasUnrequestedChunks(piece pieceIndex) (found bool) {
	var it typedRoaring.Iterator[RequestIndex]
	t.iterUndirtiedRequestIndexesInPiece(&it, piece, func(r RequestIndex) {
		if _, ok := t.requestState[r]; !ok {
			found = true
		}
	})
	return
}

type requestState struct {
	peer weak.Pointer[PeerConn]
	when time.Time
}

func (t *Torrent) checkValidReceiveChunk(r Request) error {
	if !t.haveInfo() {
		return errors.New("torrent missing info")
	}
	if int(r.Index) >= t.numPieces() {
		return fmt.Errorf("chunk index %v, torrent num pieces %v", r.Index, t.numPieces())
	}
	pieceLength := t.pieceLength(pieceIndex(r.Index))
	if r.Begin >= pieceLength {
		return fmt.Errorf("chunk begins beyond end of piece (%v >= %v)", r.Begin, pieceLength)
	}
	if r.Begin%t.chunkSize != 0 {
		return fmt.Errorf("chunk begins off a chunk boundary (%v)", r.Begin)
	}
	if want := chunkIndexSpec(r.Begin/t.chunkSize, pieceLength, t.chunkSize).Length; r.Length != want {
		return fmt.Errorf("chunk length %v, want %v", r.Length, want)
	}
	return nil
}

func (t *Torrent) peerConnsWithDialAddrPort(target netip.AddrPort) (ret []*PeerConn) {
	for pc := range t.conns {
		dialAddr, err := pc.remoteDialAddrPort()
		if err != nil {
			continue
		}
		if dialAddr != target {
			continue
		}
		ret = append(ret, pc)
	}
	return
}

func wrapUtHolepunchMsgForPeerConn(
	recipient *PeerConn,
	msg utHolepunch.Msg,
) (pp.Message, bool) {
	id, ok := recipient.PeerExtensionIDs[utHolepunch.ExtensionName]
	if !ok {
		return pp.Message{}, false
	}
	extendedPayload, err := msg.MarshalBinary()
	if err != nil {
		return pp.Message{}, false
	}
	return pp.Message{
		Type:            pp.Extended,
		ExtendedID:      id,
		ExtendedPayload: extendedPayload,
	}, true
}

func sendUtHolepunchMsg(
	pc *PeerConn,
	msgType utHolepunch.MsgType,
	addrPort netip.AddrPort,
	errCode utHolepunch.ErrCode,
) {
	holepunchMsg := utHolepunch.Msg{
		MsgType:  msgType,
		AddrPort: addrPort,
		ErrCode:  errCode,
	}
	ppMsg, ok := wrapUtHolepunchMsgForPeerConn(pc, holepunchMsg)
	if !ok {
		return
	}
	pc.write(ppMsg)
}

func localScope(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsUnspecified()
}

func (t *Torrent) handleReceivedUtHolepunchMsg(msg utHolepunch.Msg, sender *PeerConn) error {
	switch msg.MsgType {
	case utHolepunch.Rendezvous:
		t.logger.Printf("got holepunch rendezvous request for %v from %p", msg.AddrPort, sender)
		sendMsg := sendUtHolepunchMsg
		senderAddrPort, err := sender.remoteDialAddrPort()
		if err != nil {
			sender.logger.Levelf(
				log.Warning,
				"error getting ut_holepunch rendezvous sender's dial address: %v",
				err,
			)
			sendMsg(sender, utHolepunch.Error, msg.AddrPort, utHolepunch.NoSuchPeer)
			return nil
		}
		targets := t.peerConnsWithDialAddrPort(msg.AddrPort)
		if len(targets) == 0 {
			sendMsg(sender, utHolepunch.Error, msg.AddrPort, utHolepunch.NotConnected)
			return nil
		}
		for _, pc := range targets {
			if !pc.supportsExtension(utHolepunch.ExtensionName) {
				sendMsg(sender, utHolepunch.Error, msg.AddrPort, utHolepunch.NoSupport)
				continue
			}
			sendMsg(sender, utHolepunch.Connect, msg.AddrPort, 0)
			sendMsg(pc, utHolepunch.Connect, senderAddrPort, 0)
		}
		return nil
	case utHolepunch.Connect:
		holepunchAddr := msg.AddrPort
		t.logger.Printf("got holepunch connect request for %v from %p", holepunchAddr, sender)
		if !holepunchAddr.IsValid() || t.cl.numHalfOpen >= t.cl.config.TotalHalfOpenConns ||
			len(t.halfOpen) >= t.cl.config.HalfOpenConnsPerTorrent {
			return nil
		}
		senderIP, _ := netip.AddrFromSlice(sender.remoteIp())
		if localScope(holepunchAddr.Addr()) && !localScope(senderIP) {
			return nil
		}
		if !sender.allowHolepunchConnect() {
			return nil
		}
		if g.MapContains(t.cl.undialableWithoutHolepunch, holepunchAddr) {
			setAdd(&t.cl.undialableWithoutHolepunchDialedAfterHolepunchConnect, holepunchAddr)
			if g.MapContains(t.cl.accepted, holepunchAddr) {
				setAdd(&t.cl.probablyOnlyConnectedDueToHolepunch, holepunchAddr)
			}
		}
		opts := outgoingConnOpts{
			peerInfo: PeerInfo{
				Addr:         msg.AddrPort,
				Source:       PeerSourceUtHolepunch,
				PexPeerFlags: sender.pex.remoteLiveConns[msg.AddrPort].UnwrapOrZeroValue(),
			},
			t:                        t,
			skipHolepunchRendezvous:  true,
			receivedHolepunchConnect: true,
			HeaderObfuscationPolicy:  t.cl.config.HeaderObfuscationPolicy,
		}
		initiateConn(opts, true)
		return nil
	case utHolepunch.Error:
		t.logger.Levelf(log.Debug, "received ut_holepunch error message from %v: %v", sender, msg.ErrCode)
		return nil
	default:
		return fmt.Errorf("unhandled msg type %v", msg.MsgType)
	}
}

func (t *Torrent) trySendHolepunchRendezvous(addrPort netip.AddrPort) error {
	rzsSent := 0
	for pc := range t.conns {
		if !pc.supportsExtension(utHolepunch.ExtensionName) {
			continue
		}
		if pc.supportsExtension(pp.ExtensionNamePex) {
			if !g.MapContains(pc.pex.remoteLiveConns, addrPort) {
				continue
			}
		}
		t.logger.Levelf(log.Debug, "sent ut_holepunch rendezvous message to %v for %v", pc, addrPort)
		sendUtHolepunchMsg(pc, utHolepunch.Rendezvous, addrPort, 0)
		rzsSent++
	}
	if rzsSent == 0 {
		return errors.New("no eligible relays")
	}
	return nil
}

func (t *Torrent) getDialTimeoutUnlocked() time.Duration {
	cl := t.cl
	cl.rLock()
	defer cl.rUnlock()
	return t.dialTimeout()
}

func (t *Torrent) canonicalShortInfohash() *infohash.T {
	if t.infoHash.Ok {
		return &t.infoHash.Value
	}
	return t.infoHashV2.UnwrapPtr().ToShort()
}

func (t *Torrent) iterShortInfohashes() iter.Seq[shortInfohash] {
	return func(yield func(shortInfohash) bool) {
		more := true
		t.eachShortInfohash(func(short [20]byte) {
			if more {
				more = yield(short)
			}
		})
	}
}

func (t *Torrent) eachShortInfohash(each func(short [20]byte)) {
	if t.infoHash.Value == *t.infoHashV2.Value.ToShort() {
		panic("v1 and v2 info hashes should not be the same")
	}
	if t.infoHash.Ok {
		each(t.infoHash.Value)
	}
	if t.infoHashV2.Ok {
		v2Short := *t.infoHashV2.Value.ToShort()
		each(v2Short)
	}
}

func (t *Torrent) getFileByPiecesRoot(hash [32]byte) *File {
	if t.files == nil {
		return nil
	}
	for _, f := range *t.files {
		if f.piecesRoot.Ok && f.piecesRoot.Value == hash {
			return f
		}
	}
	return nil
}

func (t *Torrent) pieceLayers() (pieceLayers map[string]string) {
	if t.files == nil {
		return
	}
	files := *t.files
	g.MakeMapWithCap(&pieceLayers, len(files))
file:
	for _, f := range files {
		if !f.piecesRoot.Ok {
			continue
		}
		key := f.piecesRoot.Value
		var value strings.Builder
		for i := f.BeginPieceIndex(); i < f.EndPieceIndex(); i++ {
			hashOpt := t.piece(i).hashV2
			if !hashOpt.Ok {
				continue file
			}
			value.Write(hashOpt.Value[:])
		}
		if value.Len() == 0 {
			continue
		}
		pieceLayers[string(key[:])] = value.String()
	}
	return
}

func (t *Torrent) Complete() chansync.ReadOnlyFlag {
	return &t.complete
}

func (t *Torrent) processSlogGroupInput(latest torrentSlogGroupInput) {
	t._slogGroup = slog.Group("torrent",
		"name", latest.name,
		"ih", latest.canonicalIh)
	t._slogger = t.baseSlogger.With(t._slogGroup)
	t.lastSlogGroupInput = latest
}

func (t *Torrent) updateSlogGroup() {
	latest := t.makeSlogGroupInput()
	if t._slogger == nil || latest != t.lastSlogGroupInput {
		t.processSlogGroupInput(latest)
	}
}

func (t *Torrent) slogger() *slog.Logger {
	t.updateSlogGroup()
	return t._slogger
}

func (t *Torrent) makeSlogGroupInput() torrentSlogGroupInput {
	var name any
	opt := t.bestName()
	if opt.Ok {
		name = opt.Value
	}
	return torrentSlogGroupInput{
		name:        name,
		canonicalIh: *t.canonicalShortInfohash(),
	}
}

func (t *Torrent) slogGroup() slog.Attr {
	t.updateSlogGroup()
	return t._slogGroup
}

func (t *Torrent) getChunkBuffer() []byte {
	b := *t.chunkPool.Get().(*[]byte)
	b = b[:t.chunkSize.Int()]
	return b
}

func (t *Torrent) putChunkBuffer(b []byte) {
	panicif.NotEq(cap(b), t.chunkSize.Int())
	t.chunkPool.Put(&b)
}

func (t *Torrent) withSlogger(base *slog.Logger) *slog.Logger {
	return base.With(slog.Group(
		"torrent",
		"ih", *t.canonicalShortInfohash()))
}

func (t *Torrent) wantReceiveChunk(reqIndex RequestIndex) bool {
	if t.checkValidReceiveChunk(t.requestIndexToRequest(reqIndex)) != nil {
		return false
	}
	pi := t.pieceIndexOfRequestIndex(reqIndex)
	if t.ignorePieceForRequests(pi) {
		return false
	}
	if t.haveRequestIndexChunk(reqIndex) {
		return false
	}
	return true
}

func (t *Torrent) getClosedErr() error {
	if t.closed.IsSet() {
		return errTorrentClosed
	}
	return nil
}

func (t *Torrent) considerStartingHashers() bool {
	if t.storage == nil {
		return false
	}
	if t.activePieceHashes >= t.cl.config.PieceHashersPerTorrent {
		return false
	}
	if !t.cl.canStartPieceHashers() {
		return false
	}
	if t.piecesQueuedForHash.IsEmpty() {
		return false
	}
	return true
}

func (t *Torrent) getFile(fileIndex int) *File {
	return (*t.files)[fileIndex]
}

func (t *Torrent) expandPieceRangeToFullFiles(beginPieceIndex, endPieceIndex pieceIndex) (expandedBegin, expandedEnd pieceIndex) {
	if beginPieceIndex == endPieceIndex {
		return beginPieceIndex, endPieceIndex
	}
	firstFile := t.getFile(t.piece(beginPieceIndex).beginFile)
	lastFile := t.getFile(t.piece(endPieceIndex-1).endFile - 1)
	expandedBegin = firstFile.BeginPieceIndex()
	expandedEnd = lastFile.EndPieceIndex()
	return
}

func (t *Torrent) filesInPieceRangeMightBePartial(begin, end pieceIndex) bool {
	begin, end = t.expandPieceRangeToFullFiles(begin, end)
	return t.piecesMightBePartial(begin, end)
}

func (t *Torrent) filesInRequestRangeMightBePartial(beginRequest, endRequest RequestIndex) bool {
	if beginRequest >= endRequest {
		return false
	}
	beginPiece := t.pieceIndexOfRequestIndex(beginRequest)
	endPiece := pieceIndex(intCeilDiv(endRequest, t.chunksPerRegularPiece()))
	return t.filesInPieceRangeMightBePartial(beginPiece, endPiece)
}

func (t *Torrent) piecesMightBePartial(beginPieceIndex, endPieceIndex int) bool {
	if t.dirtyChunks.IntersectsWithInterval(
		uint64(t.pieceRequestIndexBegin(beginPieceIndex)),
		uint64(t.pieceRequestIndexBegin(endPieceIndex)),
	) {
		return true
	}
	var r roaring.Bitmap
	r.AddRange(uint64(beginPieceIndex), uint64(endPieceIndex))
	switch t._completedPieces.AndCardinality(&r) {
	case 0, uint64(endPieceIndex - beginPieceIndex):
		return false
	default:
		return true
	}
}

func (t *Torrent) hasActiveWebseedRequests() bool {
	for _, p := range t.webSeeds {
		for req := range p.activeRequests {
			if !req.cancelled.Load() {
				return true
			}
		}
	}
	return false
}

func (t *Torrent) incrementPiecesDirtiedStats(p *Piece, inc func(stats *ConnStats) bool) {
	if len(p.dirtiers) == 0 {
		return
	}
	distinctUpstreamConnStats := make(map[*ConnStats]struct{}, 6)
	for c := range p.dirtiers {
		inc(&c._stats)
		count := 0
		for cs := range c.upstreamConnStats() {
			distinctUpstreamConnStats[cs] = struct{}{}
			count++
		}
		panicif.NotEq(count, 4)
	}
	panicif.GreaterThan(len(distinctUpstreamConnStats), 6)
	maps.Keys(distinctUpstreamConnStats)(inc)
}

func (t *Torrent) maxEndRequest() RequestIndex {
	return RequestIndex(intCeilDiv(uint64(t.length()), t.chunkSize.Uint64()))
}

func (p *Torrent) chunkIndexSpec(piece pieceIndex, chunk chunkIndexType) ChunkSpec {
	return chunkIndexSpec(pp.Integer(chunk), p.pieceLength(piece), p.chunkSize)
}

func (t *Torrent) progressUnitFloat() float64 {
	if !t.haveInfo() {
		return math.NaN()
	}
	return 1 - float64(t.bytesMissingLocked())/float64(t.info.TotalLength())
}
