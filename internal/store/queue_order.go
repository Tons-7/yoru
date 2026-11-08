package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

type lineSpot struct {
	animeID int
	epKey   string
	at      int64
}

func (s *Store) MoveQueued(ctx context.Context, animeID int, epKey, to string) (bool, error) {
	return s.moveInLine(ctx, animeID, epKey, func(rest []lineSpot, i int) (int, bool) {
		switch to {
		case "up":
			return max(i-1, 0), true
		case "down":
			return min(i+1, len(rest)), true
		case "top":
			return 0, true
		case "bottom":
			return len(rest), true
		}
		return 0, false
	})
}

func (s *Store) MoveQueuedNear(ctx context.Context, animeID int, epKey string, targetAnime int, targetEp string, after bool) (bool, error) {
	return s.moveInLine(ctx, animeID, epKey, func(rest []lineSpot, _ int) (int, bool) {
		for k, l := range rest {
			if l.animeID == targetAnime && l.epKey == targetEp {
				if after {
					return k + 1, true
				}
				return k, true
			}
		}
		return 0, false
	})
}

func (s *Store) moveInLine(ctx context.Context, animeID int, epKey string, place func(rest []lineSpot, i int) (int, bool)) (bool, error) {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT anime_id, ep_key, queued_at FROM download_queue
		WHERE state IN ('pending', 'paused') ORDER BY queued_at, episode`)
	if err != nil {
		return false, err
	}
	var line []lineSpot
	for rows.Next() {
		var l lineSpot
		if err := rows.Scan(&l.animeID, &l.epKey, &l.at); err != nil {
			rows.Close()
			return false, err
		}
		line = append(line, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}

	i := -1
	for k, l := range line {
		if l.animeID == animeID && l.epKey == epKey {
			i = k
		}
	}
	if i < 0 {
		return false, nil
	}
	moving := line[i]
	rest := append(append([]lineSpot{}, line[:i]...), line[i+1:]...)
	at, ok := place(rest, i)
	if !ok || at == i {
		return false, nil
	}
	order := append(append(append([]lineSpot{}, rest[:at]...), moving), rest[at:]...)
	base := line[0].at
	for k, l := range order {
		if _, err := tx.ExecContext(ctx, `UPDATE download_queue SET queued_at = ? WHERE anime_id = ? AND ep_key = ?`,
			base+int64(k), l.animeID, l.epKey); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

func (s *Store) ClaimQueued(ctx context.Context, animeID int, epKey string) (QueuedDownload, bool, error) {
	var q QueuedDownload
	var hash, name *string
	err := s.w.QueryRowContext(ctx, `
		UPDATE download_queue SET state = 'active', started_at = ?
		WHERE anime_id = ? AND ep_key = ? AND state = 'pending'
		RETURNING anime_id, ep_key, episode, season, info_hash, release_name, attempts`,
		time.Now().Unix(), animeID, epKey).Scan(&q.AnimeID, &q.EpKey, &q.Episode, &q.Season, &hash, &name, &q.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return QueuedDownload{}, false, nil
	}
	if err != nil {
		return QueuedDownload{}, false, err
	}
	q.InfoHash, q.Release, q.State = derefString(hash), derefString(name), "active"
	return q, true, nil
}

func (s *Store) NextFromTorrents(ctx context.Context, hashes []string) (QueuedDownload, bool, error) {
	if len(hashes) == 0 {
		return QueuedDownload{}, false, nil
	}
	args := []any{time.Now().UnixMilli()}
	for _, h := range hashes {
		args = append(args, strings.ToLower(h))
	}
	var animeID int
	var epKey string
	err := s.r.QueryRowContext(ctx, `
		SELECT q.anime_id, q.ep_key FROM download_queue q
		JOIN torrent_file f ON f.anime_id = q.anime_id AND f.ep_key = q.ep_key AND f.selected = 1
		WHERE q.state = 'pending' AND (q.retry_at IS NULL OR q.retry_at <= ?)
		  AND lower(f.info_hash) IN (`+strings.TrimSuffix(strings.Repeat("?,", len(hashes)), ",")+`)
		  AND (q.info_hash IS NULL OR lower(q.info_hash) = lower(f.info_hash))
		ORDER BY q.queued_at, q.episode
		LIMIT 1`, args...).Scan(&animeID, &epKey)
	if errors.Is(err, sql.ErrNoRows) {
		return QueuedDownload{}, false, nil
	}
	if err != nil {
		return QueuedDownload{}, false, err
	}
	return s.ClaimQueued(ctx, animeID, epKey)
}

func (s *Store) PrioritiseWaiting(ctx context.Context, animeID int, epKey string) (bool, error) {
	res, err := s.w.ExecContext(ctx, `
		UPDATE download_queue
		SET queued_at = coalesce((SELECT min(queued_at) FROM download_queue), queued_at) - 1,
		    retry_at = NULL, state = 'pending', error = CASE WHEN state = 'paused' THEN NULL ELSE error END
		WHERE anime_id = ? AND ep_key = ? AND state IN ('pending', 'paused')`, animeID, epKey)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) SetQueuedPaused(ctx context.Context, animeID int, epKey string, paused bool) (bool, error) {
	query := `UPDATE download_queue SET state = 'pending', retry_at = NULL, error = NULL
		WHERE anime_id = ? AND ep_key = ? AND state = 'paused'`
	if paused {
		query = `UPDATE download_queue SET state = 'paused', started_at = NULL, retry_at = NULL, error = NULL
			WHERE anime_id = ? AND ep_key = ? AND state IN ('pending', 'active')`
	}
	res, err := s.w.ExecContext(ctx, query, animeID, epKey)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

type releaseEpisode struct {
	animeID  int
	epKey    string
	complete bool
	kept     bool
	name     string
}

func (s *Store) ReleaseEpisodes(ctx context.Context, infoHash string, fileIndex int) ([]EpisodeRef, error) {
	eps, err := s.releaseEpisodes(ctx, infoHash, fileIndex)
	out := make([]EpisodeRef, 0, len(eps))
	for _, e := range eps {
		out = append(out, EpisodeRef{AnimeID: e.animeID, EpKey: e.epKey})
	}
	return out, err
}

func (s *Store) releaseEpisodes(ctx context.Context, infoHash string, fileIndex int) ([]releaseEpisode, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT f.anime_id, f.ep_key, coalesce(max(c.complete), 0), coalesce(max(c.kept), 0), coalesce(t.name, '')
		FROM torrent_file f
		JOIN torrent t ON t.info_hash = f.info_hash
		LEFT JOIN cache_entry c ON c.info_hash = f.info_hash AND (c.file_index = f.file_index OR c.file_index = ?)
		WHERE lower(f.info_hash) = lower(?) AND f.selected = 1 AND f.anime_id IS NOT NULL AND f.ep_key IS NOT NULL
		  AND (? < 0 OR f.file_index = ?)
		GROUP BY f.anime_id, f.ep_key
		ORDER BY f.anime_id, CAST(f.ep_key AS INTEGER), f.ep_key`, WholeTorrent, infoHash, fileIndex, fileIndex)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []releaseEpisode
	for rows.Next() {
		var e releaseEpisode
		var complete, kept int
		if err := rows.Scan(&e.animeID, &e.epKey, &complete, &kept, &e.name); err != nil {
			return nil, err
		}
		e.complete, e.kept = complete == 1, kept == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SetReleaseQueuedPaused(ctx context.Context, infoHash string, fileIndex int, paused bool) (int, error) {
	episodes, err := s.releaseEpisodes(ctx, infoHash, fileIndex)
	if err != nil {
		return 0, err
	}
	state := "pending"
	if paused {
		state = "paused"
	}
	n := 0
	for _, e := range episodes {
		if e.complete {
			continue
		}
		changed, err := s.SetQueuedPaused(ctx, e.animeID, e.epKey, paused)
		if err != nil {
			return n, err
		}
		if changed {
			n++
			continue
		}
		if !e.kept {
			continue
		}
		var exists int
		if err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM download_queue WHERE anime_id = ? AND ep_key = ?`,
			e.animeID, e.epKey).Scan(&exists); err != nil {
			return n, err
		}
		if exists > 0 {
			continue
		}
		episode, _ := strconv.Atoi(e.epKey)
		if _, err := s.w.ExecContext(ctx, `
			INSERT INTO download_queue (anime_id, ep_key, episode, season, state, queued_at, info_hash, release_name)
			VALUES (?, ?, ?, 0, ?, coalesce((SELECT max(queued_at) FROM download_queue), ?) + 1, ?, ?)`,
			e.animeID, e.epKey, episode, state, time.Now().Unix(), strings.ToLower(infoHash), e.name); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
