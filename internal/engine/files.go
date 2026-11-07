package engine

import (
	"errors"
	"os"
	"time"

	"yoru/internal/bt"
	"yoru/internal/bt/metainfo"
)

var errNoFile = errors.New("no such file")

var (
	freeRetry  = 20 * time.Second
	freeGiveUp = 10 * time.Minute
)

func (e *Engine) fileOf(en *entry, i int) (*bt.File, error) {
	if en.t.Info() == nil || i < 0 || i >= len(en.t.Files()) {
		return nil, errNoFile
	}
	return en.t.Files()[i], nil
}

func (e *Engine) setFileIncluded(en *entry, i int, on bool) error {
	f, err := e.fileOf(en, i)
	if err != nil {
		return err
	}
	if on {
		if _, err := os.Stat(e.names.filePath(en.t, f)); os.IsNotExist(err) {
			e.forgetPieces(en, f, false)
		}
	}
	e.mu.Lock()
	if on {
		en.selected[i] = true
	} else {
		delete(en.selected, i)
	}
	e.mu.Unlock()
	e.apply(en)
	e.refocus()
	e.saveSession()
	return nil
}

func (e *Engine) deleteFile(en *entry, i int) error {
	if err := e.setFileIncluded(en, i, false); err != nil {
		return err
	}
	f := en.t.Files()[i]
	if e.names.shared(e.names.filePath(en.t, f), en.t.InfoHash(), e.held()) {
		return nil
	}
	e.forgetPieces(en, f, true)
	err := e.freeFile(en, i)
	if inUse(err) {
		go e.retryFree(en, i)
		return nil
	}
	if errors.Is(err, errors.ErrUnsupported) {
		e.log.Info("this system can't free part of a file; the removed episode's data stays until its pack goes",
			"name", en.t.Name(), "file", i)
		return nil
	}
	return err
}

func (e *Engine) freeFile(en *entry, i int) error {
	f := en.t.Files()[i]
	path := e.names.filePath(en.t, f)
	head, tail := e.sharedEdges(en, f)
	switch {
	case head == 0 && tail == 0:
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	case head+tail >= f.Length():
		return nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	return punchHole(path, head, f.Length()-head-tail)
}

func (e *Engine) retryFree(en *entry, i int) {
	until := time.Now().Add(freeGiveUp)
	for time.Now().Before(until) {
		time.Sleep(freeRetry)
		e.mu.Lock()
		_, held := e.byID[en.id]
		back := en.selected[i]
		e.mu.Unlock()
		if !held || back {
			return
		}
		err := e.freeFile(en, i)
		if err == nil {
			e.log.Info("freed a removed episode's file once it was let go", "name", en.t.Name(), "file", i)
			return
		}
		if !inUse(err) {
			e.log.Warn("free a removed episode's file", "name", en.t.Name(), "file", i, "err", err)
			return
		}
	}
	e.log.Warn("a removed episode's file stayed open; its data stays on disk", "name", en.t.Name(), "file", i)
}

func (e *Engine) sharedEdges(en *entry, f *bt.File) (head, tail int64) {
	info := en.t.Info()
	if info == nil || f.BeginPieceIndex() >= f.EndPieceIndex() {
		return 0, 0
	}
	pieceLen := info.PieceLength
	others := e.selectedBut(en, f)
	first, last := f.BeginPieceIndex(), f.EndPieceIndex()-1
	if sharedPiece(first, others) {
		head = min(f.Length(), int64(first+1)*pieceLen-f.Offset())
	}
	if sharedPiece(last, others) {
		tail = min(f.Length(), f.Offset()+f.Length()-int64(last)*pieceLen)
	}
	return head, tail
}

func (e *Engine) selectedBut(en *entry, f *bt.File) []*bt.File {
	e.mu.Lock()
	defer e.mu.Unlock()
	var others []*bt.File
	for j, g := range en.t.Files() {
		if en.selected[j] && g != f {
			others = append(others, g)
		}
	}
	return others
}

func (e *Engine) forgetPieces(en *entry, f *bt.File, spareShared bool) {
	store := e.pieceStore()
	if store == nil {
		return
	}
	others := e.selectedBut(en, f)
	for k := f.BeginPieceIndex(); k < f.EndPieceIndex(); k++ {
		if spareShared && sharedPiece(k, others) {
			continue
		}
		store.Set(metainfo.PieceKey{InfoHash: en.t.InfoHash(), Index: k}, false)
		en.t.Piece(k).UpdateCompletion()
	}
}

func sharedPiece(k int, files []*bt.File) bool {
	for _, g := range files {
		if k >= g.BeginPieceIndex() && k < g.EndPieceIndex() {
			return true
		}
	}
	return false
}
