package engine

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"yoru/internal/bt"
	"yoru/internal/torrent"
)

func (e *Engine) migrateLegacyNames(t *bt.Torrent) {
	info := t.Info()
	if info == nil {
		return
	}
	top := e.names.top(info.BestName(), t.InfoHash())
	for _, f := range t.Files() {
		now := e.names.filePath(t, f)
		fi := f.FileInfo()
		if !legacySafe(info.BestName(), info.IsDir(), fi.BestPath()) || !e.names.owns(t, now) {
			continue
		}
		old := legacyPath(e.opts.CacheDir, top, info.BestName(), info.IsDir(), fi.BestPath())
		if !torrent.Within(e.opts.CacheDir, old) || old == now || exists(now) || !exists(old) ||
			e.names.shared(old, t.InfoHash(), e.held()) {
			continue
		}
		if err := moveFile(old, now); err != nil {
			e.log.Warn("move a download to its safe name", "from", old, "to", now, "err", err)
			continue
		}
		e.log.Info("moved a download to its safe name", "from", old, "to", now)
	}
}

func legacySafe(best string, dir bool, parts []string) bool {
	if !legacyPart(best) {
		return false
	}
	if !dir {
		return true
	}
	for _, p := range parts {
		if !legacyPart(p) {
			return false
		}
	}
	return true
}

func legacyPart(p string) bool {
	if !safeComponent(p) || strings.ContainsRune(p, '\\') {
		return false
	}
	return runtime.GOOS != "windows" || strings.TrimRight(p, ". ") == p
}

func legacyPath(cache, top, best string, dir bool, parts []string) string {
	if top == safeName(best) {
		top = best
	}
	if !dir {
		return filepath.Join(cache, top)
	}
	return filepath.Join(append([]string{cache, top}, parts...)...)
}

func moveFile(old, now string) error {
	if err := os.MkdirAll(filepath.Dir(now), 0o755); err != nil {
		return err
	}
	if os.Rename(old, now) == nil {
		return nil
	}
	in, err := os.Open(old)
	if err != nil {
		return err
	}
	defer in.Close()
	part := now + ".part"
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(part)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(part)
		return err
	}
	if err := os.Rename(part, now); err != nil {
		os.Remove(part)
		return err
	}
	in.Close()
	return os.Remove(old)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
