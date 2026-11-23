package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"yoru/internal/bt"
	"yoru/internal/bt/metainfo"
	"yoru/internal/torrent"
)

type layout struct {
	base  string
	mu    sync.Mutex
	tops  map[metainfo.Hash]string
	own   map[metainfo.Hash]bool
	packs map[metainfo.Hash]map[string][]string
}

func newLayout(base string) *layout {
	return &layout{base: base, tops: map[metainfo.Hash]string{}, own: map[metainfo.Hash]bool{},
		packs: map[metainfo.Hash]map[string][]string{}}
}

func (l *layout) top(name string, hash metainfo.Hash) string {
	name = safeName(name)
	if !safeComponent(name) {
		name = hash.HexString()
	}
	if l == nil {
		return name
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if top, ok := l.tops[hash]; ok {
		return top
	}
	if l.own[hash] {
		delete(l.own, hash)
		l.tops[hash] = name
		return name
	}
	if tagged := suffixed(name, hash); l.base != "" {
		if _, err := os.Stat(filepath.Join(l.base, tagged)); err == nil {
			l.tops[hash] = tagged
			return tagged
		}
	}
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

func suffixed(name string, hash metainfo.Hash) string { return torrent.Tagged(name, hash.HexString()) }

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
	delete(l.packs, hash)
	l.mu.Unlock()
}

func (l *layout) folder(t *bt.Torrent) string {
	info := t.Info()
	if info == nil || !info.IsDir() {
		return filepath.Clean(l.base)
	}
	return filepath.Join(l.base, l.top(info.BestName(), t.InfoHash()))
}

func inTorrent(base, top string, isDir bool, path string) bool {
	if !safeComponent(top) {
		return false
	}
	base = filepath.Clean(base)
	path = filepath.Clean(path)
	own := filepath.Join(base, top)
	if !isDir {
		return path == own
	}
	return path != own && torrent.Within(own, path)
}

func safeComponent(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsRune(name, '/') && !strings.ContainsRune(name, filepath.Separator)
}

func (l *layout) owns(t *bt.Torrent, path string) bool {
	info := t.Info()
	if info == nil {
		return false
	}
	return inTorrent(l.base, l.top(info.BestName(), t.InfoHash()), info.IsDir(), path)
}

func (l *layout) filePath(t *bt.Torrent, f *bt.File) string {
	info := t.Info()
	if !info.IsDir() {
		return filepath.Join(l.base, l.top(info.BestName(), t.InfoHash()))
	}
	return filepath.Join(append([]string{l.folder(t)}, l.parts(info, t.InfoHash(), f.FileInfo())...)...)
}

func (l *layout) shared(path string, hash metainfo.Hash, others []*bt.Torrent) bool {
	for _, o := range others {
		if o.InfoHash() == hash {
			continue
		}
		if o.Info() == nil {
			if top, ok := l.claimed(o.InfoHash()); ok && safeComponent(top) && under(path, filepath.Join(l.base, top)) {
				return true
			}
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

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(windowsName(a), windowsName(b))
	}
	if runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func windowsName(p string) string {
	parts := strings.Split(filepath.Clean(p), string(filepath.Separator))
	for i, part := range parts {
		if part != "." && part != ".." {
			parts[i] = strings.TrimRight(part, ". ")
		}
	}
	return strings.Join(parts, string(filepath.Separator))
}

func under(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if samePath(path, dir) {
		return true
	}
	return len(path) > len(dir) && path[len(dir)] == filepath.Separator && samePath(path[:len(dir)], dir)
}

func (l *layout) parts(info *metainfo.Info, hash metainfo.Hash, fi metainfo.FileInfo) []string {
	if l == nil {
		return packParts(info)[pathKey(fi.BestPath())]
	}
	l.mu.Lock()
	m, ok := l.packs[hash]
	if !ok {
		m = packParts(info)
		l.packs[hash] = m
	}
	l.mu.Unlock()
	return m[pathKey(fi.BestPath())]
}

func pathKey(parts []string) string { return strings.Join(parts, "\x00") }

func packParts(info *metainfo.Info) map[string][]string {
	out := map[string][]string{}
	taken := map[string]bool{}
	for fi := range info.UpvertedFilesIter() {
		key := pathKey(fi.BestPath())
		if _, ok := out[key]; ok {
			continue
		}
		base := safePath(fi.BestPath())
		parts := base
		for n := 2; taken[foldPath(parts)]; n++ {
			parts = numbered(base, n)
		}
		taken[foldPath(parts)] = true
		out[key] = parts
	}
	return out
}

func foldPath(parts []string) string {
	p := strings.Join(parts, "/")
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(p)
	}
	return p
}

func numbered(parts []string, n int) []string {
	out := slices.Clone(parts)
	last := out[len(out)-1]
	ext := filepath.Ext(last)
	out[len(out)-1] = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(last, ext), n, ext)
	return out
}

func safePath(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = safeName(p)
	}
	return out
}

var safeName = torrent.SafeName
