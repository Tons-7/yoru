package store

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
)

const handResumedKey = "downloads.hand_resumed"

var handResumedMu sync.Mutex

func (s *Store) HandResumed(ctx context.Context) map[string]bool {
	raw, _ := s.Setting(ctx, handResumedKey)
	var list []string
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &list)
	}
	out := make(map[string]bool, len(list))
	for _, h := range list {
		out[strings.ToLower(h)] = true
	}
	return out
}

func (s *Store) SetHandResumed(ctx context.Context, hash string, on bool) error {
	handResumedMu.Lock()
	defer handResumedMu.Unlock()
	hash = strings.ToLower(hash)
	raw, _ := s.Setting(ctx, handResumedKey)
	var list []string
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &list)
	}
	list = slices.DeleteFunc(list, func(h string) bool { return strings.EqualFold(h, hash) })
	if on {
		list = append(list, hash)
		if len(list) > 200 {
			list = list[len(list)-200:]
		}
	}
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return s.SetSetting(ctx, handResumedKey, string(b))
}

func (s *Store) TorrentFilePath(ctx context.Context, infoHash string, index int) (string, error) {
	var path string
	err := s.r.QueryRowContext(ctx,
		`SELECT path FROM torrent_file WHERE lower(info_hash) = lower(?) AND file_index = ?`, infoHash, index).Scan(&path)
	return path, err
}
