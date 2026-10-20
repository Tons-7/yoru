package engine

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"yoru/internal/bt"
	"yoru/internal/bt/metainfo"
	"yoru/internal/torrent"
)

// layout is where each torrent's files live: under its name, or with its hash appended when another
// torrent holds that name, so two same-named releases never write to or delete each other's files.
type layout struct {
	base string
	mu   sync.Mutex
	tops map[metainfo.Hash]string
	own  map[metainfo.Hash]bool // restored with no saved name: under its own, as when it was added
}

func newLayout(base string) *layout {
	return &layout{base: base, tops: map[metainfo.Hash]string{}, own: map[metainfo.Hash]bool{}}
}

// top is the name a torrent's file or folder goes under, decided on first sight and kept after.
func (l *layout) top(name string, hash metainfo.Hash) string {
	name = safeName(name)
	if l == nil {
		return name
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if top, ok := l.tops[hash]; ok {
		return top
	}
	// Placed before these rules, its files are there: tagged now, it would download again.
	if l.own[hash] {
		delete(l.own, hash)
		l.tops[hash] = name
		return name
	}
	// Case-blind: Windows and macOS put "Show" and "show" in one folder.
	// One of Yoru's own folders would be swept with it.
	taken := torrent.OwnFolder(name)
	for other, top := range l.tops {
		if !taken && strings.EqualFold(top, name) && other != hash {
			taken = true
		}
	}
	if taken {
		name = suffixed(name, hash)
	}
	l.tops[hash] = name
	return name
}

// suffixed tags a name with the hash; the cache's cleanup derives the same, so the rule lives in torrent.
func suffixed(name string, hash metainfo.Hash) string { return torrent.Tagged(name, hash.HexString()) }

// claim records a name restored from the session, so a restart keeps every torrent where it was; with none
// saved, the torrent's own name.
func (l *layout) claim(hash metainfo.Hash, top string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	if top == "" {
		if _, ok := l.tops[hash]; !ok {
			l.own[hash] = true
		}
	} else {
		l.tops[hash] = top
	}
	l.mu.Unlock()
}

// claimed is the name a torrent holds, known before its info arrives when restored from the session.
func (l *layout) claimed(hash metainfo.Hash) (string, bool) {
	if l == nil {
		return "", false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	top, ok := l.tops[hash]
	return top, ok
}

func (l *layout) forget(hash metainfo.Hash) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.tops, hash)
	delete(l.own, hash)
	l.mu.Unlock()
}

// folder is where a torrent's files go: the cache for one file, a folder named after the torrent for several.
func (l *layout) folder(t *bt.Torrent) string {
	info := t.Info()
	if info == nil || !info.IsDir() {
		return filepath.Clean(l.base)
	}
	return filepath.Join(l.base, l.top(info.BestName(), t.InfoHash()))
}

func (l *layout) filePath(t *bt.Torrent, f *bt.File) string {
	info := t.Info()
	if !info.IsDir() {
		return filepath.Join(l.base, l.top(info.BestName(), t.InfoHash()))
	}
	fi := f.FileInfo()
	return filepath.Join(append([]string{l.folder(t)}, safePath(fi.BestPath())...)...)
}

// shared reports a path another held torrent also maps to, which a delete must leave alone.
func (l *layout) shared(path string, hash metainfo.Hash, others []*bt.Torrent) bool {
	for _, o := range others {
		if o.InfoHash() == hash || o.Info() == nil {
			continue
		}
		for _, f := range o.Files() {
			if samePath(l.filePath(o, f), path) {
				return true
			}
		}
	}
	return false
}

// samePath: one file on disk. Case-blind where the filesystem is (Windows, macOS); on Linux two names
// differing in case are two files, and treating them as one left a deleted torrent's file behind.
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// safePath is a torrent's path inside its folder, each part made safe to create.
func safePath(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = safeName(p)
	}
	return out
}

var safeName = torrent.SafeName
