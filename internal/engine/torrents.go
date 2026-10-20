package engine

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"yoru/internal/bt"
	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/storage"

	"yoru/internal/torrent"
)

// hashOf reads the info hash from a magnet link.
func hashOf(magnet string) (metainfo.Hash, []string, error) {
	m, err := metainfo.ParseMagnetUri(strings.TrimSpace(magnet))
	if err != nil {
		return metainfo.Hash{}, nil, err
	}
	return m.InfoHash, m.Trackers, nil
}

func (e *Engine) awaitInfo(ctx context.Context, t *bt.Torrent) error {
	select {
	case <-t.GotInfo():
		return nil
	case <-ctx.Done():
		return fmt.Errorf("metadata: %w", ctx.Err())
	}
}

// inspect resolves a magnet's file list without downloading any of it.
func (e *Engine) inspect(ctx context.Context, magnet string) (torrent.Detail, error) {
	hash, _, err := hashOf(magnet)
	if err != nil {
		return torrent.Detail{}, err
	}
	e.mu.Lock()
	if e.offline {
		e.mu.Unlock()
		return torrent.Detail{}, errRestarting
	}
	if en, ok := e.byHash[hash]; ok {
		e.mu.Unlock()
		if err := e.awaitInfo(ctx, en.t); err != nil {
			return torrent.Detail{}, err
		}
		return e.detail(en), nil
	}
	in, ok := e.inspected[hash]
	if !ok {
		t, err := e.cl.AddMagnet(magnet)
		if err != nil {
			e.mu.Unlock()
			return torrent.Detail{}, err
		}
		t.DisallowDataDownload()
		e.added(t)
		in = &inspection{t: t}
		e.inspected[hash] = in
	}
	in.expires = time.Now().Add(inspectionTTL)
	e.mu.Unlock()
	if !ok {
		e.refocus()
	}

	if err := e.awaitInfo(ctx, in.t); err != nil {
		return torrent.Detail{}, err
	}
	// An add may have taken the torrent over meanwhile; its selection stands.
	e.mu.Lock()
	en, managed := e.byHash[hash]
	if !managed {
		for _, f := range in.t.Files() {
			f.SetPriority(bt.PiecePriorityNone)
		}
	}
	e.mu.Unlock()
	if managed {
		return e.detail(en), nil
	}
	return detailOf(in.t, e.names, nil), nil
}

// add starts a torrent or adds to one held, selecting the files matching only, the rest as they were.
func (e *Engine) add(ctx context.Context, magnet, only string) (*entry, error) {
	hash, trackers, err := hashOf(magnet)
	if err != nil {
		return nil, err
	}
	var pattern *regexp.Regexp
	if only != "" {
		if pattern, err = regexp.Compile(only); err != nil {
			return nil, fmt.Errorf("only_files_regex: %w", err)
		}
	}

	e.mu.Lock()
	if e.offline {
		e.mu.Unlock()
		return nil, errRestarting
	}
	en, ok := e.byHash[hash]
	if !ok {
		var t *bt.Torrent
		if in, inspected := e.inspected[hash]; inspected {
			t = in.t
			delete(e.inspected, hash)
		} else if t, err = e.cl.AddMagnet(magnet); err != nil {
			e.mu.Unlock()
			return nil, err
		} else {
			e.added(t)
		}
		en = e.track(t, trackers)
	}
	e.mu.Unlock()
	if !ok {
		e.refocus()
	}

	if err := e.awaitInfo(ctx, en.t); err != nil {
		return nil, err
	}
	if !ok {
		e.forgetMissing(en.t)
	}
	e.mu.Lock()
	for i, f := range en.t.Files() {
		if pattern == nil || pattern.MatchString(fileName(en.t, f)) {
			en.selected[i] = true
		}
	}
	e.mu.Unlock()
	e.apply(en)
	e.refocus()
	e.saveSession()
	return en, nil
}

// forgetMissing clears records of deleted files' pieces, or a regrown file would pass zero-filled holes.
func (e *Engine) forgetMissing(t *bt.Torrent) {
	pieces := e.pieceStore()
	if pieces == nil {
		return
	}
	for _, f := range t.Files() {
		if _, err := os.Stat(e.names.filePath(t, f)); err == nil {
			continue
		}
		for i := f.BeginPieceIndex(); i < f.EndPieceIndex(); i++ {
			pieces.Set(metainfo.PieceKey{InfoHash: t.InfoHash(), Index: i}, false)
		}
	}
}

// pieceStore is the running client's piece completion; nil while the client is down.
func (e *Engine) pieceStore() storage.PieceCompletion {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pieces
}

// track registers a torrent under a new id. Caller holds the lock.
func (e *Engine) track(t *bt.Torrent, trackers []string) *entry {
	id := e.keepIDs[t.InfoHash()]
	if id == 0 {
		id = e.nextID
		e.nextID++
	}
	en := &entry{id: id, t: t, trackers: trackers, selected: map[int]bool{}}
	e.byID[en.id] = en
	e.byHash[t.InfoHash()] = en
	if len(trackers) > 0 {
		t.AddTrackers([][]string{trackers})
	}
	t.SetOnWriteChunkError(func(err error) { e.writeFailed(en, err) })
	return en
}

// vanished has bt re-read the completion of a deleted file's pieces, which storage has just cleared.
func (e *Engine) vanished(hash metainfo.Hash, first, last int) {
	e.mu.Lock()
	en, ok := e.byHash[hash]
	e.mu.Unlock()
	if !ok || en.t.Info() == nil {
		return
	}
	e.log.Info("a downloaded file was deleted outside Yoru; its pieces download again", "torrent", en.t.Name())
	for i := first; i <= last && i < en.t.NumPieces(); i++ {
		en.t.Piece(i).UpdateCompletion()
	}
}

// A failed write stops the torrent's downloading; this long later it tries again, in case space was freed.
const writeRetry = time.Minute

// writeFailed stops downloading a torrent whose chunks can't be written and says so in its stats.
func (e *Engine) writeFailed(en *entry, err error) {
	if errors.Is(err, errStorageClosed) {
		return
	}
	e.mu.Lock()
	first := en.writeErr == ""
	en.writeErr = "can't write the download to disk: " + err.Error()
	en.writeErrAt = time.Now()
	e.mu.Unlock()
	if first {
		e.log.Error("chunk write failed; downloading stopped until it can retry", "torrent", en.t.Name(), "err", err)
	}
	en.t.DisallowDataDownload()
}

// retryWrites lets a torrent stopped by a write failure download again once writeRetry has passed.
func (e *Engine) retryWrites(en *entry, now time.Time) {
	e.mu.Lock()
	due := en.writeErr != "" && now.Sub(en.writeErrAt) >= writeRetry
	if due {
		en.writeErr = ""
	}
	paused := en.paused
	e.mu.Unlock()
	if due && !paused {
		en.t.AllowDataDownload()
	}
}

// apply turns the selection and pause state into file priorities.
func (e *Engine) apply(en *entry) {
	if en.t.Info() == nil {
		return
	}
	e.mu.Lock()
	selected := maps.Clone(en.selected)
	paused := en.paused
	if !paused {
		// Resumed or added again: that is the retry, now.
		en.writeErr = ""
	}
	e.mu.Unlock()
	for i, f := range en.t.Files() {
		// Paused wants nothing: a torrent wanting pieces keeps dialing peers, taking a download's budget.
		if selected[i] && !paused {
			f.SetPriority(bt.PiecePriorityNormal)
		} else {
			f.SetPriority(bt.PiecePriorityNone)
		}
	}
	if paused {
		en.t.DisallowDataDownload()
	} else {
		en.t.AllowDataDownload()
	}
	e.quieten(en)
}

func (e *Engine) setPaused(en *entry, paused bool) {
	e.mu.Lock()
	en.paused = paused
	e.mu.Unlock()
	e.apply(en)
	e.refocus()
	e.saveSession()
}

// remove drops a torrent; with data, its files go too.
func (e *Engine) remove(en *entry, data bool) error {
	e.mu.Lock()
	delete(e.byID, en.id)
	delete(e.byHash, en.t.InfoHash())
	delete(e.known, en.t.InfoHash())
	e.mu.Unlock()

	var paths []string
	var pieces int
	if info := en.t.Info(); info != nil && data {
		// A file another held torrent maps to is its now, whatever the names say.
		others := e.held()
		for _, f := range en.t.Files() {
			if p := e.names.filePath(en.t, f); !e.names.shared(p, en.t.InfoHash(), others) {
				paths = append(paths, p)
			}
		}
		pieces = info.NumPieces()
	}
	hash := en.t.InfoHash()
	folder := e.names.folder(en.t)
	en.t.Drop()
	<-en.t.Closed()
	e.refocus()

	os.Remove(filepath.Join(e.opts.SessionDir, hash.HexString()+".torrent"))
	e.saveSession()
	if !data {
		e.names.forget(hash)
		return nil
	}
	// Completion would otherwise claim pieces whose bytes are gone.
	if store := e.pieceStore(); store != nil {
		if f, ok := store.(interface{ Forget(metainfo.Hash) error }); !ok || f.Forget(hash) != nil {
			for i := range pieces {
				store.Set(metainfo.PieceKey{InfoHash: hash, Index: i}, false)
			}
		}
	}
	var failed []string
	for _, p := range paths {
		switch err := os.Remove(p); {
		case err == nil || os.IsNotExist(err):
		case inUse(err):
			// A stream switching releases still reads the old file for a moment; it goes once let go.
			e.doom(p, folder, err)
		default:
			failed = append(failed, p)
		}
	}
	if folder != filepath.Clean(e.opts.CacheDir) {
		removeEmptyDirs(folder)
	}
	e.names.forget(hash)
	if len(failed) > 0 {
		return fmt.Errorf("could not remove %s", strings.Join(failed, ", "))
	}
	return nil
}

// Long enough for any reader to finish closing; past it, the file stays for the orphan cleanup.
const doomedFor = 30 * time.Minute

type doomedFile struct {
	folder string
	until  time.Time
}

func (e *Engine) doom(path, folder string, err error) {
	e.log.Info("a deleted download's file is still open; removing it once released", "path", path, "err", err)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.doomed == nil {
		e.doomed = map[string]doomedFile{}
	}
	e.doomed[path] = doomedFile{folder: folder, until: time.Now().Add(doomedFor)}
}

// reap tries the doomed files again; one a torrent maps to again, added back since, is that torrent's now.
func (e *Engine) reap(now time.Time) {
	e.mu.Lock()
	doomed := maps.Clone(e.doomed)
	e.mu.Unlock()
	if len(doomed) == 0 {
		return
	}
	held := e.held()
	for path, d := range doomed {
		claimed := e.names.shared(path, metainfo.Hash{}, held)
		if !claimed {
			switch err := os.Remove(path); {
			case err == nil || os.IsNotExist(err):
				e.log.Info("removed a deleted download's file once it was released", "path", path)
				if d.folder != filepath.Clean(e.opts.CacheDir) {
					removeEmptyDirs(d.folder)
				}
			case now.After(d.until):
				e.log.Warn("a deleted download's file stayed open; it is left on disk", "path", path, "err", err)
			default:
				continue
			}
		}
		e.mu.Lock()
		delete(e.doomed, path)
		e.mu.Unlock()
	}
}

// held lists the torrents the engine manages.
func (e *Engine) held() []*bt.Torrent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*bt.Torrent, 0, len(e.byID))
	for _, en := range e.byID {
		out = append(out, en.t)
	}
	return out
}

// removeEmptyDirs deletes dir and empty folders below it, leaving any with files.
func removeEmptyDirs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, d := range entries {
		if d.IsDir() {
			removeEmptyDirs(filepath.Join(dir, d.Name()))
		}
	}
	os.Remove(dir)
}

// fileName is a file's path inside the torrent, "/"-joined.
func fileName(t *bt.Torrent, f *bt.File) string {
	parts := components(t, f)
	return strings.Join(parts, "/")
}

func components(t *bt.Torrent, f *bt.File) []string {
	info := t.Info()
	if !info.IsDir() {
		return []string{info.BestName()}
	}
	fi := f.FileInfo()
	return slices.Clone(fi.BestPath())
}

// diskComponents is where a file is under OutputFolder: names made safe, a single file's hash tag.
func diskComponents(t *bt.Torrent, f *bt.File, names *layout) []string {
	info := t.Info()
	if !info.IsDir() {
		return []string{names.top(info.BestName(), t.InfoHash())}
	}
	fi := f.FileInfo()
	return safePath(fi.BestPath())
}

func (e *Engine) detail(en *entry) torrent.Detail {
	e.mu.Lock()
	selected := maps.Clone(en.selected)
	e.mu.Unlock()
	return detailOf(en.t, e.names, selected)
}

func detailOf(t *bt.Torrent, names *layout, selected map[int]bool) torrent.Detail {
	d := torrent.Detail{InfoHash: t.InfoHash().HexString(), Name: t.Name(), OutputFolder: names.folder(t)}
	if t.Info() == nil {
		return d
	}
	for i, f := range t.Files() {
		d.Files = append(d.Files, torrent.File{
			Name:       fileName(t, f),
			Components: diskComponents(t, f, names),
			Length:     f.Length(),
			Included:   selected[i],
		})
	}
	return d
}

// stats reports the fields Yoru reads, in rqbit's stats/v1 shape.
func (e *Engine) stats(en *entry) torrent.Stats {
	var s torrent.Stats
	e.mu.Lock()
	selected := maps.Clone(en.selected)
	paused, verifying := en.paused, en.verifying
	// A write failure retries after writeRetry, so it is a warning: as Error, waiters would give up for good.
	s.Error, s.Warning = en.err, en.writeErr
	e.mu.Unlock()

	switch {
	case en.t.Info() == nil || verifying:
		s.State = "initializing"
	case paused:
		s.State = "paused"
	default:
		s.State = "live"
	}
	if en.t.Info() != nil {
		for i, f := range en.t.Files() {
			done := f.BytesCompleted()
			s.FileProgress = append(s.FileProgress, done)
			if selected[i] {
				s.ProgressBytes += done
				s.TotalBytes += f.Length()
			}
		}
		s.Finished = s.TotalBytes > 0 && s.ProgressBytes >= s.TotalBytes && verified(en.t, selected)
	}

	ts := en.t.Stats()
	fetched := ts.BytesReadUsefulData.Int64()
	now := time.Now()
	e.mu.Lock()
	if !en.lastAt.IsZero() {
		if dt := now.Sub(en.lastAt).Seconds(); dt >= 1 {
			en.mibps = float64(fetched-en.lastBytes) / (1 << 20) / dt
			en.lastBytes, en.lastAt = fetched, now
		}
	} else {
		en.lastBytes, en.lastAt = fetched, now
	}
	speed := en.mibps
	e.mu.Unlock()

	if s.State == "live" {
		live := &torrent.Live{}
		live.DownloadSpeed.Mbps = speed
		live.Snapshot.FetchedBytes = ts.BytesReadData.Int64()
		live.Snapshot.PeerStats.Live = ts.ActivePeers
		s.Live = live
	}
	return s
}

// fileRanges merges a file's complete pieces into the stretches of it that are on disk.
// verified: the selected files' pieces all passed their hash; BytesCompleted also counts chunks awaiting one.
func verified(t *bt.Torrent, selected map[int]bool) bool {
	for i, f := range t.Files() {
		if !selected[i] {
			continue
		}
		for _, p := range f.State() {
			if !p.Complete {
				return false
			}
		}
	}
	return true
}

func fileRanges(f *bt.File) torrent.FileRanges {
	out := torrent.FileRanges{Length: f.Length(), Ranges: []torrent.ByteRange{}}
	var at int64
	for _, p := range f.State() {
		if p.Complete {
			if n := len(out.Ranges); n > 0 && out.Ranges[n-1].To == at {
				out.Ranges[n-1].To += p.Bytes
			} else {
				out.Ranges = append(out.Ranges, torrent.ByteRange{From: at, To: at + p.Bytes})
			}
		}
		at += p.Bytes
	}
	return out
}
