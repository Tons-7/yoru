package store

import (
	"context"
	"strings"
)

type DownloadPart struct {
	AnimeID   int     `json:"animeId,omitempty"`
	Title     string  `json:"title,omitempty"`
	Cover     *string `json:"cover,omitempty"`
	EpKey     string  `json:"epKey"`
	FileIndex int     `json:"fileIndex"`
	Size      int64   `json:"size"`
	Done      int64   `json:"done"`
	Complete  bool    `json:"complete"`
	Kept      bool    `json:"kept"`
	Pinned    bool    `json:"pinned"`
	Paused    bool    `json:"paused"`
}

func (s *Store) DownloadParts(ctx context.Context) (map[string][]DownloadPart, error) {
	mode := s.TitleMode(ctx, 0)
	rows, err := s.r.QueryContext(ctx, `
		SELECT f.info_hash, coalesce(f.anime_id, 0), a.title_romaji, a.title_english, a.cover_url,
		       f.ep_key, f.file_index, f.size_bytes,
		       coalesce(max(c.complete), 0), coalesce(max(c.kept), 0), coalesce(max(c.pinned), 0)
		FROM torrent_file f
		LEFT JOIN anime a ON a.id = f.anime_id
		LEFT JOIN cache_entry c ON c.info_hash = f.info_hash AND (c.file_index = f.file_index OR c.file_index = ?)
		WHERE f.selected = 1 AND f.ep_key IS NOT NULL
		GROUP BY f.info_hash, f.file_index
		ORDER BY f.info_hash, f.anime_id, CAST(f.ep_key AS INTEGER), f.file_index`, WholeTorrent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]DownloadPart{}
	for rows.Next() {
		var hash string
		var p DownloadPart
		var romaji, english *string
		var complete, kept, pinned int
		if err := rows.Scan(&hash, &p.AnimeID, &romaji, &english, &p.Cover, &p.EpKey, &p.FileIndex, &p.Size,
			&complete, &kept, &pinned); err != nil {
			return nil, err
		}
		if romaji != nil {
			p.Title = PickTitle(mode, *romaji, english)
		}
		p.Complete, p.Kept, p.Pinned = complete == 1, kept == 1, pinned == 1
		hash = strings.ToLower(hash)
		out[hash] = append(out[hash], p)
	}
	return out, rows.Err()
}
