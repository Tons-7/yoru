package store

import (
	"context"
	"database/sql"
)

func (s *Store) SequelsMissed(ctx context.Context, limit int) ([]int, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT r.related_id
		FROM list_entry l
		JOIN relation r      ON r.anime_id = l.anime_id AND r.kind = 'SEQUEL'
		LEFT JOIN anime a    ON a.id = r.related_id
		LEFT JOIN list_entry n ON n.anime_id = r.related_id
		WHERE l.status IN ('COMPLETED', 'REPEATING')
		  AND n.anime_id IS NULL
		  AND NOT EXISTS (SELECT 1 FROM user_anime u
		                  WHERE u.anime_id = r.related_id AND (u.favourite = 1 OR u.hidden = 1))
		  AND coalesce(a.status, '') IN ('', 'FINISHED', 'RELEASING')
		  AND coalesce(a.format, '') <> 'MUSIC'
		GROUP BY r.related_id
		ORDER BY max(coalesce(a.start_date, '')) DESC
		LIMIT ?`, limit)
	return s.ids(rows, err)
}

func (s *Store) TopScored(ctx context.Context, limit int) ([]int, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT anime_id FROM list_entry
		WHERE status IN ('COMPLETED', 'REPEATING', 'CURRENT', 'PAUSED')
		ORDER BY score DESC, max(remote_updated_at, local_updated_at) DESC
		LIMIT ?`, limit)
	return s.ids(rows, err)
}

func (s *Store) Known(ctx context.Context) (map[int]bool, error) {
	ids, err := s.ids(s.r.QueryContext(ctx, `
		SELECT anime_id FROM list_entry
		UNION SELECT anime_id FROM user_anime WHERE favourite = 1 OR hidden = 1`))
	listed := make(map[int]bool, len(ids))
	for _, id := range ids {
		listed[id] = true
	}
	return listed, err
}

func (s *Store) ids(rows *sql.Rows, err error) ([]int, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) HiddenIDs(ctx context.Context) (map[int]bool, error) {
	ids, err := s.ids(s.r.QueryContext(ctx, `SELECT anime_id FROM user_anime WHERE hidden = 1`))
	out := make(map[int]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

type HiddenShow struct {
	AnimeID int    `json:"animeId"`
	Title   string `json:"title"`
	Cover   string `json:"cover,omitempty"`
}

func (s *Store) HiddenShows(ctx context.Context) ([]HiddenShow, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT u.anime_id, coalesce(nullif(a.title_english, ''), a.title_romaji, ''), coalesce(a.cover_url, '')
		FROM user_anime u LEFT JOIN anime a ON a.id = u.anime_id
		WHERE u.hidden = 1 ORDER BY u.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HiddenShow{}
	for rows.Next() {
		var h HiddenShow
		if err := rows.Scan(&h.AnimeID, &h.Title, &h.Cover); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
