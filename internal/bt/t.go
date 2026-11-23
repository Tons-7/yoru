package bt

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/anacrolix/chansync/events"
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/missinggo/v2/pubsub"

	"yoru/internal/bt/metainfo"
)

func (t *Torrent) InfoHash() metainfo.Hash {
	return *t.canonicalShortInfohash()
}

func (t *Torrent) GotInfo() events.Done {
	return t.gotMetainfoC
}

func (t *Torrent) Info() (info *metainfo.Info) {
	t.nameMu.RLock()
	info = t.info
	t.nameMu.RUnlock()
	return
}

func (t *Torrent) NewReader() Reader {
	return t.newReader(0, t.length())
}

func (t *Torrent) newReader(offset, length int64) Reader {
	r := reader{
		mu:     t.cl.locker(),
		t:      t,
		offset: offset,
		length: length,
		ctx:    context.Background(),
	}
	r.readaheadFunc = defaultReadaheadFunc
	t.addReader(&r)
	return &r
}

type PieceStateRuns []PieceStateRun

func (me PieceStateRuns) String() (s string) {
	if len(me) > 0 {
		var sb strings.Builder
		sb.WriteString(me[0].String())
		for i := 1; i < len(me); i += 1 {
			sb.WriteByte(' ')
			sb.WriteString(me[i].String())
		}
		return sb.String()
	}
	return
}

func (t *Torrent) PieceStateRuns() (runs PieceStateRuns) {
	t.cl.rLock()
	runs = t.pieceStateRuns()
	t.cl.rUnlock()
	return
}

func (t *Torrent) PieceState(piece pieceIndex) (ps PieceState) {
	t.cl.rLock()
	ps = t.pieceState(piece)
	t.cl.rUnlock()
	return
}

func (t *Torrent) NumPieces() pieceIndex {
	return t.numPieces()
}

func (t *Torrent) PieceBytesMissing(piece int) int64 {
	t.cl.rLock()
	defer t.cl.rUnlock()

	return int64(t.pieces[piece].bytesLeft())
}

func (t *Torrent) Drop() {
	if t.closed.IsSet() {
		return
	}
	t.cl.lock()
	if t.closed.IsSet() {
		t.cl.unlock()
		return
	}
	var wg sync.WaitGroup
	t.close(&wg)
	t.cl.unlock()
	wg.Wait()
}

func (t *Torrent) BytesCompleted() int64 {
	t.cl.rLock()
	defer t.cl.rUnlock()
	return t.bytesCompleted()
}

func (t *Torrent) SubscribePieceStateChanges() *pubsub.Subscription[PieceStateChange] {
	return t.pieceStateChanges.Subscribe()
}

func (t *Torrent) Seeding() (ret bool) {
	t.cl.rLock()
	ret = t.seeding()
	t.cl.rUnlock()
	return
}

func (t *Torrent) SetDisplayName(dn string) {
	t.nameMu.Lock()
	if !t.haveInfo() {
		t.displayName = dn
	}
	t.nameMu.Unlock()
}

func (t *Torrent) Name() string {
	return t.name()
}

func (t *Torrent) Length() int64 {
	return t._length.Value
}

func (t *Torrent) Metainfo() metainfo.MetaInfo {
	t.cl.rLock()
	defer t.cl.rUnlock()
	return t.newMetaInfo()
}

func (t *Torrent) addReader(r *reader) {
	t.cl.lock()
	defer t.cl.unlock()
	if t.readers == nil {
		t.readers = make(map[*reader]struct{})
	}
	t.readers[r] = struct{}{}
	r.posChanged()
}

func (t *Torrent) deleteReader(r *reader) {
	delete(t.readers, r)
	if r.pieces == (pieceRange{}) {
		t.updateReaderPieces()
		return
	}
	t.readerPosChanged(r.pieces, pieceRange{})
}

func (t *Torrent) DownloadPieces(begin, end pieceIndex) {
	t.cl.lock()
	t.downloadPiecesLocked(begin, end)
	t.cl.unlock()
}

func (t *Torrent) downloadPiecesLocked(begin, end pieceIndex) {
	for i := begin; i < end; i++ {
		if t.pieces[i].priority.Raise(PiecePriorityNormal) {
			t.updatePiecePriority(i, "Torrent.DownloadPieces")
		}
	}
}

func (t *Torrent) CancelPieces(begin, end pieceIndex) {
	t.cl.lock()
	t.cancelPiecesLocked(begin, end, "Torrent.CancelPieces")
	t.cl.unlock()
}

func (t *Torrent) cancelPiecesLocked(begin, end pieceIndex, reason updateRequestReason) {
	for i := begin; i < end; i++ {
		p := t.piece(i)
		if p.priority == PiecePriorityNone {
			continue
		}
		p.priority = PiecePriorityNone
		t.updatePiecePriority(i, reason)
	}
}

func (t *Torrent) initFiles() {
	info := t.info
	var offset int64
	t.files = new([]*File)
	for _, fi := range t.info.UpvertedFiles() {
		*t.files = append(*t.files, &File{
			t,
			strings.Join(append([]string{info.BestName()}, fi.BestPath()...), "/"),
			offset,
			fi.Length,
			fi,
			fi.DisplayPath(info),
			PiecePriorityNone,
			fi.PiecesRoot,
		})
		offset += fi.Length
		if info.FilesArePieceAligned() {
			offset = (offset + info.PieceLength - 1) / info.PieceLength * info.PieceLength
		}
	}
}

func (t *Torrent) Files() []*File {
	return *t.files
}

func (t *Torrent) AddPeers(pp []PeerInfo) (n int) {
	t.cl.lock()
	defer t.cl.unlock()
	n = t.addPeers(pp)
	return
}

func (t *Torrent) DownloadAll() {
	t.DownloadPieces(0, t.numPieces())
}

func (t *Torrent) String() string {
	s := t.name()
	if s == "" {
		return t.canonicalShortInfohash().HexString()
	} else {
		return strconv.Quote(s)
	}
}

func (t *Torrent) AddTrackers(announceList [][]string) {
	t.cl.lock()
	defer t.cl.unlock()
	t.addTrackers(announceList)
}

func (t *Torrent) Piece(i pieceIndex) *Piece {
	return t.piece(i)
}

func (t *Torrent) PeerConns() []*PeerConn {
	t.cl.rLock()
	defer t.cl.rUnlock()
	ret := make([]*PeerConn, 0, len(t.conns))
	for c := range t.conns {
		ret = append(ret, c)
	}
	return ret
}

func (t *Torrent) WebseedPeerConns() []*Peer {
	t.cl.rLock()
	defer t.cl.rUnlock()
	ret := make([]*Peer, 0, len(t.conns))
	for _, c := range t.webSeeds {
		ret = append(ret, &c.peer)
	}
	return ret
}

func (t *Torrent) isDropped() bool {
	return !g.MapContains(t.cl.torrents, t)
}
