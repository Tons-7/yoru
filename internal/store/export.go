package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type ExportEntry struct {
	AnimeID     int    `json:"animeId"`
	MalID       *int   `json:"malId,omitempty"`
	Title       string `json:"title"`
	English     string `json:"english,omitempty"`
	Episodes    *int   `json:"episodes,omitempty"`
	Status      string `json:"status,omitempty"`
	Progress    int    `json:"progress"`
	Score       int    `json:"score,omitempty"`
	Repeat      int    `json:"repeat,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
	Favourite   bool   `json:"favourite,omitempty"`
	Note        string `json:"note,omitempty"`
}

const exportQuery = `
SELECT a.id, a.mal_id, a.title_romaji, coalesce(a.title_english, ''),
       a.episode_count,
       coalesce(e.status, ''), coalesce(e.progress, 0), coalesce(e.score, 0),
       coalesce(e.repeat_count, 0),
       coalesce(e.started_at, ''), coalesce(e.completed_at, ''),
       coalesce(u.favourite, 0), coalesce(u.note, '')
FROM anime a
LEFT JOIN list_entry e ON e.anime_id = a.id
LEFT JOIN user_anime u ON u.anime_id = a.id
WHERE e.anime_id IS NOT NULL OR u.favourite = 1
ORDER BY a.title_romaji`

func (s *Store) ExportLibrary(ctx context.Context) ([]ExportEntry, error) {
	rows, err := s.r.QueryContext(ctx, exportQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ExportEntry{}
	for rows.Next() {
		var e ExportEntry
		var favourite int
		if err := rows.Scan(&e.AnimeID, &e.MalID, &e.Title, &e.English, &e.Episodes,
			&e.Status, &e.Progress, &e.Score, &e.Repeat,
			&e.StartedAt, &e.CompletedAt, &favourite, &e.Note); err != nil {
			return nil, err
		}
		e.Favourite = favourite == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AnimeIDByMAL(ctx context.Context, malID int) (int, bool, error) {
	var id int
	err := s.r.QueryRowContext(ctx, malPick, malID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

const malPick = `
	SELECT id FROM (
	    SELECT a.id, 0 AS src,
	           EXISTS (SELECT 1 FROM list_entry e WHERE e.anime_id = a.id) AS listed,
	           coalesce((SELECT local_updated_at FROM list_entry e WHERE e.anime_id = a.id), 0) AS touched
	    FROM anime a WHERE a.mal_id = ?1
	    UNION ALL
	    SELECT anime_id, 1, 0, 0 FROM corpus_anime WHERE mal_id = ?1
	) ORDER BY listed DESC, touched DESC, id > 0 DESC, src, id
	LIMIT 1`

const importedStatus = `CASE
	    WHEN list_entry.status = 'COMPLETED' AND coalesce(excluded.status, '') <> 'REPEATING' THEN list_entry.status
	    WHEN excluded.status = 'PLANNING' AND list_entry.status IS NOT NULL THEN list_entry.status
	    ELSE coalesce(excluded.status, list_entry.status) END`

type ImportReport struct {
	Entries    int `json:"entries"`
	Favourites int `json:"favourites"`
	Skipped    int `json:"skipped"`
}

func (s *Store) ImportEntries(ctx context.Context, entries []ExportEntry) (ImportReport, error) {
	var rep ImportReport
	now := time.Now().Unix()

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()

	for _, e := range entries {
		if e.AnimeID == 0 {
			rep.Skipped++
			continue
		}
		if err := ensureAnime(ctx, tx, e.AnimeID); err != nil {
			return rep, err
		}
		if !ValidStatus(e.Status) {
			e.Status = ""
		}

		listed := e.Status != "" || e.Progress > 0 || e.Score > 0 || e.Repeat > 0
		if listed {
			var previous sql.NullString
			if err := tx.QueryRowContext(ctx, `SELECT status FROM list_entry WHERE anime_id = ?`, e.AnimeID).
				Scan(&previous); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return rep, err
			}
			rewatch := e.Status == StatusRepeating && previous.Valid && previous.String != StatusRepeating
			if _, err := tx.ExecContext(ctx, `
			INSERT INTO list_entry (id, anime_id, status, progress, score, repeat_count,
			                        started_at, completed_at,
			                        remote_updated_at, local_updated_at, dirty)
			VALUES ((SELECT min(coalesce(min(id), 0), 0) - 1 FROM list_entry),
			        ?1, ?2, ?3, ?4, ?5, ?6, ?7, 0, ?8, 1)
			ON CONFLICT(anime_id) DO UPDATE SET
			    progress = CASE WHEN ?9 THEN excluded.progress ELSE max(list_entry.progress, excluded.progress) END,
			    status = `+importedStatus+`,
			    score = CASE WHEN excluded.score > 0 THEN excluded.score ELSE list_entry.score END,
			    repeat_count = max(list_entry.repeat_count, excluded.repeat_count),
			    started_at = coalesce(list_entry.started_at, excluded.started_at),
			    completed_at = coalesce(list_entry.completed_at, excluded.completed_at),
			    local_updated_at = max(excluded.local_updated_at, list_entry.local_updated_at + 1),
			    dirty = 1
			WHERE ?9
			   OR excluded.progress > list_entry.progress
			   OR (`+importedStatus+`) IS NOT list_entry.status
			   OR (excluded.score > 0 AND excluded.score <> list_entry.score)
			   OR excluded.repeat_count > list_entry.repeat_count
			   OR (list_entry.started_at IS NULL AND excluded.started_at IS NOT NULL)
			   OR (list_entry.completed_at IS NULL AND excluded.completed_at IS NOT NULL)`,
				e.AnimeID, nullable(e.Status), e.Progress, e.Score, e.Repeat,
				nullable(e.StartedAt), nullable(e.CompletedAt), now, rewatch); err != nil {
				return rep, err
			}
			if rewatch {
				if _, err := tx.ExecContext(ctx,
					`UPDATE playback SET watched = 0, position_s = 0, played_s = 0, dismissed = 0 WHERE anime_id = ?`,
					e.AnimeID); err != nil {
					return rep, err
				}
			}
			rep.Entries++
		}

		if !e.Favourite && e.Note == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_anime (anime_id, favourite, note, updated_at,
			                        favourite_synced, favourite_dirty)
			VALUES (?, ?, ?, ?, 0, ?)
			ON CONFLICT(anime_id) DO UPDATE SET
			    favourite = max(user_anime.favourite, excluded.favourite),
			    note = coalesce(excluded.note, user_anime.note),
			    updated_at = excluded.updated_at,
			    favourite_dirty = CASE
			        WHEN max(user_anime.favourite, excluded.favourite) <> user_anime.favourite_synced
			        THEN 1 ELSE user_anime.favourite_dirty END`,
			e.AnimeID, boolInt(e.Favourite), nullable(e.Note), now, boolInt(e.Favourite)); err != nil {
			return rep, err
		}
		if e.Favourite {
			rep.Favourites++
		}
	}
	if err := tx.Commit(); err != nil {
		return rep, err
	}
	return rep, nil
}
