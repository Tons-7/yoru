package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

type QueuedDownload struct {
	AnimeID  int     `json:"animeId"`
	EpKey    string  `json:"epKey"`
	Episode  int     `json:"episode"`
	Season   int     `json:"season"`
	State    string  `json:"state"`
	Error    *string `json:"error,omitempty"`
	Title    string  `json:"title,omitempty"`
	Cover    *string `json:"cover,omitempty"`
	InfoHash string  `json:"infoHash,omitempty"`
	Release  string  `json:"release,omitempty"`
	Attempts int     `json:"attempts,omitempty"`
	RetryAt  int64   `json:"retryAt,omitempty"`
}

func (s *Store) SetQueuedRelease(ctx context.Context, animeID int, epKey, infoHash, name string) (bool, error) {
	res, err := s.w.ExecContext(ctx, `
		UPDATE download_queue SET info_hash = ?, release_name = ?, error = NULL, attempts = 0, retry_at = NULL,
		    state = CASE WHEN state = 'failed' THEN 'pending' ELSE state END
		WHERE anime_id = ? AND ep_key = ?`, nullable(infoHash), nullable(name), animeID, epKey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) EnqueueRelease(ctx context.Context, animeID, season, episode int, infoHash, name string) error {
	if err := s.EnsureAnime(ctx, animeID); err != nil {
		return err
	}
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO download_queue (anime_id, ep_key, episode, season, state, queued_at, info_hash, release_name)
		VALUES (?,?,?,?,'pending',?,?,?)
		ON CONFLICT(anime_id, ep_key) DO UPDATE SET
		    state = CASE WHEN download_queue.state IN ('failed','done') THEN 'pending' ELSE download_queue.state END,
		    info_hash = excluded.info_hash, release_name = excluded.release_name,
		    error = NULL, attempts = 0, retry_at = NULL`,
		animeID, strconv.Itoa(episode), episode, season, time.Now().Unix(), nullable(infoHash), nullable(name))
	return err
}

func (s *Store) Enqueue(ctx context.Context, animeID, season int, episodes []int) (int, error) {
	if animeID == 0 || len(episodes) == 0 {
		return 0, nil
	}
	if err := s.EnsureAnime(ctx, animeID); err != nil {
		return 0, err
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO download_queue (anime_id, ep_key, episode, season, state, queued_at)
		VALUES (?,?,?,?,'pending',?)
		ON CONFLICT(anime_id, ep_key) DO UPDATE SET
		    state = CASE WHEN download_queue.state IN ('failed','done')
		                 THEN 'pending' ELSE download_queue.state END,
		    error = NULL, attempts = 0, retry_at = NULL`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	now := time.Now().Unix()
	var added int
	for _, episode := range episodes {
		if episode <= 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, animeID, strconv.Itoa(episode), episode, season, now); err != nil {
			return 0, err
		}
		added++
	}
	return added, tx.Commit()
}

func (s *Store) NextQueued(ctx context.Context) (QueuedDownload, bool, error) {
	var q QueuedDownload
	var hash, name *string
	now := time.Now()
	err := s.w.QueryRowContext(ctx, `
		UPDATE download_queue SET state = 'active', started_at = ?
		WHERE state = 'pending' AND (anime_id, ep_key) = (
		    SELECT anime_id, ep_key FROM download_queue
		    WHERE state = 'pending' AND (retry_at IS NULL OR retry_at <= ?)
		    ORDER BY queued_at, episode
		    LIMIT 1)
		RETURNING anime_id, ep_key, episode, season, info_hash, release_name, attempts`,
		now.Unix(), now.UnixMilli()).Scan(&q.AnimeID, &q.EpKey, &q.Episode, &q.Season, &hash, &name, &q.Attempts)
	q.InfoHash, q.Release = derefString(hash), derefString(name)
	if errors.Is(err, sql.ErrNoRows) {
		return QueuedDownload{}, false, nil
	}
	if err != nil {
		return QueuedDownload{}, false, err
	}
	q.State = "active"
	return q, true, nil
}

func (s *Store) PeekQueued(ctx context.Context) (QueuedDownload, bool, error) {
	q := QueuedDownload{State: "pending"}
	err := s.r.QueryRowContext(ctx, `
		SELECT anime_id, ep_key, episode, season, coalesce(info_hash, '') FROM download_queue
		WHERE state = 'pending' AND (retry_at IS NULL OR retry_at <= ?)
		ORDER BY queued_at, episode
		LIMIT 1`, time.Now().UnixMilli()).Scan(&q.AnimeID, &q.EpKey, &q.Episode, &q.Season, &q.InfoHash)
	if errors.Is(err, sql.ErrNoRows) {
		return QueuedDownload{}, false, nil
	}
	return q, err == nil, err
}

func (s *Store) Defer(ctx context.Context, animeID int, epKey string, cause error, at time.Time) error {
	_, err := s.w.ExecContext(ctx, `
		UPDATE download_queue
		SET state = 'pending', started_at = NULL, attempts = attempts + 1, retry_at = ?, error = ?, queued_at = ?
		WHERE state = 'active' AND anime_id = ? AND ep_key = ?`,
		at.UnixMilli(), cause.Error(), time.Now().Unix(), animeID, epKey)
	return err
}

func (s *Store) NextRetry(ctx context.Context) (time.Time, bool, error) {
	var at *int64
	err := s.r.QueryRowContext(ctx, `
		SELECT min(retry_at) FROM download_queue WHERE state = 'pending' AND retry_at > ?`,
		time.Now().UnixMilli()).Scan(&at)
	if err != nil || at == nil {
		return time.Time{}, false, err
	}
	return time.UnixMilli(*at), true, nil
}

func (s *Store) FinishQueued(ctx context.Context, animeID int, epKey string, cause error) error {
	if cause == nil {
		_, err := s.w.ExecContext(ctx,
			`DELETE FROM download_queue WHERE anime_id = ? AND ep_key = ?`, animeID, epKey)
		return err
	}
	_, err := s.w.ExecContext(ctx, `
		UPDATE download_queue SET state = 'failed', error = ?
		WHERE anime_id = ? AND ep_key = ?`, cause.Error(), animeID, epKey)
	return err
}

func (s *Store) RequeueActive(ctx context.Context, animeID int, epKey string) (bool, error) {
	res, err := s.w.ExecContext(ctx, `
		UPDATE download_queue SET state = 'pending', started_at = NULL
		WHERE state = 'active' AND anime_id = ? AND ep_key = ?`, animeID, epKey)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) ResetActive(ctx context.Context) error {
	_, err := s.w.ExecContext(ctx, `
		UPDATE download_queue
		SET state = 'pending', started_at = NULL,
		    queued_at = coalesce((SELECT min(queued_at) FROM download_queue), queued_at) - 1
		WHERE state = 'active'`)
	return err
}

func (s *Store) Prioritise(ctx context.Context, animeID int, epKey string) (bool, error) {
	return s.promote(ctx, animeID, epKey, "pending")
}

func (s *Store) Promote(ctx context.Context, animeID int, epKey string) (bool, error) {
	return s.promote(ctx, animeID, epKey, "pending", "active")
}

func (s *Store) promote(ctx context.Context, animeID int, epKey string, states ...string) (bool, error) {
	args := []any{animeID, epKey}
	for _, st := range states {
		args = append(args, st)
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(states)), ",")
	res, err := s.w.ExecContext(ctx, `
		UPDATE download_queue
		SET queued_at = coalesce((SELECT min(queued_at) FROM download_queue), queued_at) - 1, retry_at = NULL
		WHERE anime_id = ? AND ep_key = ? AND state IN (`+marks+`)`, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) QueuedDownloads(ctx context.Context) ([]QueuedDownload, error) {
	mode := s.TitleMode(ctx, 0)

	rows, err := s.r.QueryContext(ctx, `
		SELECT q.anime_id, q.ep_key, q.episode, q.season, q.state, q.error,
		       a.title_romaji, a.title_english, a.cover_url, q.info_hash, q.release_name, q.attempts, q.retry_at
		FROM download_queue q
		LEFT JOIN anime a ON a.id = q.anime_id
		ORDER BY q.state = 'active' DESC, q.queued_at, q.episode
		LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []QueuedDownload{}
	for rows.Next() {
		var q QueuedDownload
		var romaji, english, hash, name *string
		var retryAt *int64
		if err := rows.Scan(&q.AnimeID, &q.EpKey, &q.Episode, &q.Season, &q.State, &q.Error,
			&romaji, &english, &q.Cover, &hash, &name, &q.Attempts, &retryAt); err != nil {
			return nil, err
		}
		q.InfoHash, q.Release = derefString(hash), derefString(name)
		if retryAt != nil {
			q.RetryAt = *retryAt
		}
		if romaji != nil {
			q.Title = PickTitle(mode, *romaji, english)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (s *Store) Dequeue(ctx context.Context, animeID int, epKey string) (int, error) {
	query := `DELETE FROM download_queue WHERE anime_id = ?`
	args := []any{animeID}
	if epKey != "" {
		query += ` AND ep_key = ?`
		args = append(args, epKey)
	}

	res, err := s.w.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) ClearQueue(ctx context.Context) (int, error) {
	res, err := s.w.ExecContext(ctx, `DELETE FROM download_queue WHERE state != 'active'`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
