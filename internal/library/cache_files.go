package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"yoru/internal/torrent"
)

func (c *Cache) PauseFile(ctx context.Context, infoHash string, index int, paused bool) error {
	if c.torrent == nil {
		return fmt.Errorf("torrent engine unavailable")
	}
	id, ok := c.liveID(ctx, infoHash)
	if !ok {
		return fmt.Errorf("not held by the engine")
	}
	return c.torrent.IncludeFile(ctx, id, index, !paused)
}

func (c *Cache) FilePlaying(ctx context.Context, infoHash string, index int) bool {
	entries, err := c.store.CacheEntries(ctx)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.EqualFold(e.InfoHash, infoHash) && e.FileIndex == index && e.Pinned {
			return true
		}
	}
	return false
}

func (c *Cache) RemoveFile(ctx context.Context, infoHash string, index int) (int64, error) {
	files, err := c.store.DownloadFiles(ctx, infoHash)
	if err != nil {
		return 0, err
	}
	found, others := false, 0
	for _, f := range files {
		switch {
		case f.FileIndex == index:
			found = true
		case f.FileIndex >= 0 && f.EpKey != "":
			others++
		}
	}
	if !found {
		return 0, fmt.Errorf("no such episode in the download")
	}
	if others == 0 && !c.holdsUnrecorded(ctx, infoHash, map[int]bool{index: true}) {
		return c.Remove(ctx, infoHash)
	}

	entries, err := c.store.CacheEntries(ctx)
	if err != nil {
		return 0, err
	}
	var freed int64
	for _, e := range entries {
		if !strings.EqualFold(e.InfoHash, infoHash) || e.FileIndex != index {
			continue
		}
		if e.Pinned {
			return 0, fmt.Errorf("that episode is playing right now")
		}
		freed = e.Bytes
	}
	if id, ok := c.liveID(ctx, infoHash); ok {
		if err := c.torrent.DeleteFile(ctx, id, index); err != nil {
			return 0, err
		}
	} else if !c.removeRecordedFile(ctx, infoHash, index) {
		freed = 0
	}
	if err := c.store.DropTorrentFile(ctx, infoHash, index); err != nil {
		return 0, err
	}
	c.log.Info("one episode of a pack removed", "hash", infoHash, "file", index)
	return freed, nil
}

func (c *Cache) removeRecordedFile(ctx context.Context, infoHash string, index int) bool {
	recorded, err := c.store.TorrentFilePath(ctx, infoHash, index)
	if err != nil || recorded == "" {
		return false
	}
	parts := strings.FieldsFunc(recorded, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return false
	}
	tops := torrent.DiskNames(parts[0], infoHash)
	if host, ok := torrent.StreamHost(parts[0]); ok {
		tops = append(tops, host)
	}
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return false
	}
	defer root.Close()
	for _, top := range tops {
		rel := filepath.Join(append([]string{top}, parts[1:]...)...)
		if !filepath.IsLocal(rel) || !torrent.Within(c.dir, filepath.Join(c.dir, rel)) {
			continue
		}
		info, err := root.Stat(rel)
		if err != nil || info.IsDir() {
			continue
		}
		if err := root.Remove(rel); err != nil {
			c.log.Warn("remove an episode file", "file", rel, "err", err)
			return false
		}
		return true
	}
	return false
}
