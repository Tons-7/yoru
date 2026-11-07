package store

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
)

const handPausedSetting = "downloads.hand_paused"

var handPausedMu sync.Mutex

func (s *Store) KnownTorrents(ctx context.Context) (map[string]bool, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT lower(info_hash) FROM torrent`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out[h] = true
	}
	return out, rows.Err()
}

func (s *Store) HandPaused(ctx context.Context) ([]string, error) {
	v, err := s.Setting(ctx, handPausedSetting)
	if err != nil || v == "" {
		return nil, err
	}
	var hashes []string
	if err := json.Unmarshal([]byte(v), &hashes); err != nil {
		return nil, nil
	}
	return hashes, nil
}

func (s *Store) SetHandPaused(ctx context.Context, hash string, paused bool) error {
	handPausedMu.Lock()
	defer handPausedMu.Unlock()
	hashes, err := s.HandPaused(ctx)
	if err != nil {
		return err
	}
	hash = strings.ToLower(hash)
	hashes = slices.DeleteFunc(hashes, func(h string) bool { return h == hash })
	if paused {
		hashes = append(hashes, hash)
	}
	b, err := json.Marshal(hashes)
	if err != nil {
		return err
	}
	return s.SetSetting(ctx, handPausedSetting, string(b))
}

func (s *Store) DropTorrentFile(ctx context.Context, infoHash string, fileIndex int) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM torrent_file WHERE lower(info_hash) = lower(?) AND file_index = ?`, infoHash, fileIndex)
	return err
}

func (s *Store) FileEpisode(ctx context.Context, infoHash string, fileIndex int) (int, string, bool) {
	var animeID int
	var ep string
	err := s.r.QueryRowContext(ctx, `
		SELECT anime_id, ep_key FROM torrent_file
		WHERE lower(info_hash) = lower(?) AND file_index = ? AND anime_id IS NOT NULL AND ep_key IS NOT NULL`,
		infoHash, fileIndex).Scan(&animeID, &ep)
	return animeID, ep, err == nil
}

func (s *Store) TorrentEpisodeCount(ctx context.Context, infoHash string) int {
	var n int
	err := s.r.QueryRowContext(ctx, `
		SELECT count(DISTINCT anime_id || ':' || ep_key) FROM torrent_file
		WHERE lower(info_hash) = lower(?) AND anime_id IS NOT NULL AND ep_key IS NOT NULL`, infoHash).Scan(&n)
	if err != nil {
		return 2
	}
	return n
}

func (s *Store) ShowTorrents(ctx context.Context, animeID int) (map[string]bool, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT DISTINCT lower(f.info_hash) FROM torrent_file f
		JOIN cache_entry c ON c.info_hash = f.info_hash
		WHERE f.anime_id = ? AND f.selected = 1`, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		out[hash] = true
	}
	return out, rows.Err()
}
