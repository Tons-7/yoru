package store

import (
	"context"
	"database/sql"
)

// Home's own picks: sequels missed, and what "For you" grows from.

// SequelsMissed follow shows finished on the list and weren't started: off it first, then planned; newest
// first. Aired only, as far as known here; a dropped one stays out.
func (s *Store) SequelsMissed(ctx context.Context, limit int) ([]int, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT r.related_id
		FROM list_entry l
		JOIN relation r      ON r.anime_id = l.anime_id AND r.kind = 'SEQUEL'
		LEFT JOIN anime a    ON a.id = r.related_id
		LEFT JOIN list_entry n ON n.anime_id = r.related_id
		WHERE l.status IN ('COMPLETED', 'REPEATING')
		  AND (n.anime_id IS NULL OR n.status = 'PLANNING')
		  AND coalesce(a.status, '') IN ('', 'FINISHED', 'RELEASING')
		  AND coalesce(a.format, '') <> 'MUSIC'
		GROUP BY r.related_id
		ORDER BY max(n.anime_id IS NOT NULL), max(coalesce(a.start_date, '')) DESC
		LIMIT ?`, limit)
	return s.ids(rows, err)
}

// TopScored are the best-liked watched shows by the viewer's score, scored or not; latest first among equals.
func (s *Store) TopScored(ctx context.Context, limit int) ([]int, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT anime_id FROM list_entry
		WHERE status IN ('COMPLETED', 'REPEATING', 'CURRENT', 'PAUSED')
		ORDER BY score DESC, max(remote_updated_at, local_updated_at) DESC
		LIMIT ?`, limit)
	return s.ids(rows, err)
}

// Listed is every show on the list, whatever its status.
func (s *Store) Listed(ctx context.Context) (map[int]bool, error) {
	ids, err := s.ids(s.r.QueryContext(ctx, `SELECT anime_id FROM list_entry`))
	listed := make(map[int]bool, len(ids))
	for _, id := range ids {
		listed[id] = true
	}
	return listed, err
}

// ids reads a one-column query of ids.
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
