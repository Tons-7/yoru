package library

import (
	"context"
	"fmt"
	"strings"
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
	if others == 0 {
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
	}
	if err := c.store.DropTorrentFile(ctx, infoHash, index); err != nil {
		return 0, err
	}
	c.log.Info("one episode of a pack removed", "hash", infoHash, "file", index)
	return freed, nil
}
