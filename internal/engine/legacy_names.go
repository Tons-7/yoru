package engine

import (
	"io"
	"os"
	"path/filepath"

	"yoru/internal/bt"
)

// migrateLegacyNames moves files written before safe names to where they are looked for now, or they'd download
// again. On Windows "Re:Zero.mkv" went into a hidden stream of "Re"; elsewhere names never changed.
func (e *Engine) migrateLegacyNames(t *bt.Torrent) {
	info := t.Info()
	if info == nil {
		return
	}
	top := e.names.top(info.BestName(), t.InfoHash())
	for _, f := range t.Files() {
		now := e.names.filePath(t, f)
		fi := f.FileInfo()
		old := legacyPath(e.opts.CacheDir, top, info.BestName(), info.IsDir(), fi.BestPath())
		if old == now || exists(now) || !exists(old) {
			continue
		}
		if err := moveFile(old, now); err != nil {
			e.log.Warn("move a download to its safe name", "from", old, "to", now, "err", err)
			continue
		}
		e.log.Info("moved a download to its safe name", "from", old, "to", now)
	}
}

// legacyPath is where a file went before names were made safe: the torrent's own names, unchanged. A top
// tagged on a clash was saved in the session and kept, so only the safe form of the torrent's name was new.
func legacyPath(cache, top, best string, dir bool, parts []string) string {
	if top == safeName(best) {
		top = best
	}
	if !dir {
		return filepath.Join(cache, top)
	}
	return filepath.Join(append([]string{cache, top}, parts...)...)
}

// moveFile renames, or copies then removes: a file kept in another's hidden stream can't be renamed out of it.
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
		return err
	}
	in.Close()
	// Only the stream itself: the file hosting it may hold other episodes' streams yet to move.
	return os.Remove(old)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
