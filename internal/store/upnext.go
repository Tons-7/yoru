package store

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
	"time"
)

type UpNextItem struct {
	AnimeID      int     `json:"animeId"`
	Episode      int     `json:"episode"`
	Title        string  `json:"title"`
	Cover        *string `json:"cover,omitempty"`
	EpisodeTitle string  `json:"episodeTitle,omitempty"`
	Minutes      int     `json:"minutes,omitempty"`
	Position     float64 `json:"position,omitempty"`
	Duration     float64 `json:"duration,omitempty"`
	Spoiler      bool    `json:"spoiler,omitempty"`
	Downloaded   bool    `json:"downloaded,omitempty"`
}

type Pick struct {
	AnimeID int `json:"animeId"`
	Episode int `json:"episode"`
}

func (s *Store) UpNext(ctx context.Context) ([]UpNextItem, error) {
	mode := s.TitleMode(ctx, 0)
	rows, err := s.r.QueryContext(ctx, `
		SELECT u.anime_id, u.episode, a.title_romaji, a.title_english, a.cover_url,
		       coalesce(ep.title_en, ''), coalesce(ep.runtime, a.duration, 0),
		       coalesce(pb.position_s, 0), coalesce(pb.duration_s, 0),
		       EXISTS (SELECT 1 FROM torrent_file tf
		               JOIN cache_entry c ON c.info_hash = tf.info_hash AND c.file_index = tf.file_index
		               WHERE tf.anime_id = u.anime_id AND tf.ep_key = cast(u.episode AS TEXT) AND c.complete = 1)
		       OR EXISTS (SELECT 1 FROM local_file lf
		                  WHERE lf.anime_id = u.anime_id AND lf.episode = u.episode AND lf.missing = 0)
		FROM up_next u
		JOIN anime a ON a.id = u.anime_id
		LEFT JOIN episode ep ON ep.anime_id = u.anime_id AND ep.ep_key = cast(u.episode AS TEXT)
		LEFT JOIN playback pb ON pb.anime_id = u.anime_id AND pb.ep_key = cast(u.episode AS TEXT) AND pb.watched = 0
		ORDER BY u.show_pos, u.episode`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []UpNextItem{}
	for rows.Next() {
		var it UpNextItem
		var romaji string
		var english *string
		if err := rows.Scan(&it.AnimeID, &it.Episode, &romaji, &english, &it.Cover,
			&it.EpisodeTitle, &it.Minutes, &it.Position, &it.Duration, &it.Downloaded); err != nil {
			return nil, err
		}
		it.Title = PickTitle(mode, romaji, english)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	type rule struct {
		mode string
		next int
	}
	rules := map[int]rule{}
	for i := range out {
		it := &out[i]
		r, ok := rules[it.AnimeID]
		if !ok {
			entry, _ := s.ListEntry(ctx, it.AnimeID)
			r = rule{s.SpoilerMode(ctx, it.AnimeID), entry.Progress + 1}
			rules[it.AnimeID] = r
		}
		it.Spoiler = it.EpisodeTitle != "" && HidesSpoilers(r.mode, it.Episode, r.next, false)
	}
	return out, nil
}

func (s *Store) AddUpNext(ctx context.Context, animeID, episode int) (bool, error) {
	if animeID == 0 || episode <= 0 {
		return false, nil
	}
	if err := s.EnsureAnime(ctx, animeID); err != nil {
		return false, err
	}
	res, err := s.w.ExecContext(ctx, `
		INSERT INTO up_next (anime_id, episode, show_pos, added_at)
		VALUES (?1, ?2, coalesce((SELECT show_pos FROM up_next WHERE anime_id = ?1 LIMIT 1),
		                         (SELECT max(show_pos) FROM up_next) + 1, 1), ?3)
		ON CONFLICT(anime_id, episode) DO NOTHING`, animeID, episode, time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *Store) RemoveUpNext(ctx context.Context, animeID, episode int) error {
	query, args := `DELETE FROM up_next WHERE anime_id = ?`, []any{animeID}
	if episode > 0 {
		query += ` AND episode = ?`
		args = append(args, episode)
	}
	_, err := s.w.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) ClearUpNext(ctx context.Context) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM up_next`)
	return err
}

func (s *Store) MoveUpNext(ctx context.Context, animeID, to int) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT anime_id FROM up_next GROUP BY anime_id ORDER BY min(show_pos), anime_id`)
	if err != nil {
		return err
	}
	var shows []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		shows = append(shows, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	at := slices.Index(shows, animeID)
	if at < 0 {
		return nil
	}
	shows = slices.Delete(shows, at, at+1)
	to = min(max(to, 0), len(shows))
	shows = slices.Insert(shows, to, animeID)
	for i, id := range shows {
		if _, err := tx.ExecContext(ctx, `UPDATE up_next SET show_pos = ? WHERE anime_id = ?`, i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) FillUpNext(ctx context.Context) (int, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT e.anime_id, e.progress FROM list_entry e
		WHERE e.status IN ('CURRENT','REPEATING')
		  AND e.anime_id NOT IN (SELECT anime_id FROM up_next)
		ORDER BY coalesce((SELECT max(last_played_at) FROM playback p WHERE p.anime_id = e.anime_id),
		                  e.local_updated_at) DESC, e.anime_id`)
	if err != nil {
		return 0, err
	}
	var watching []Pick
	for rows.Next() {
		var p Pick
		if err := rows.Scan(&p.AnimeID, &p.Episode); err != nil {
			rows.Close()
			return 0, err
		}
		watching = append(watching, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	added := 0
	for _, w := range watching {
		next, err := s.NextEpisode(ctx, w.AnimeID, w.Episode)
		if err != nil {
			return added, err
		}
		if next == 0 {
			continue
		}
		ok, err := s.AddUpNext(ctx, w.AnimeID, next)
		if err != nil {
			return added, err
		}
		if ok {
			added++
		}
	}
	return added, nil
}

func (s *Store) UpNextAfter(ctx context.Context, animeID, episode int) ([]UpNextItem, error) {
	items, err := s.UpNext(ctx)
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for _, it := range items {
		if it.AnimeID == animeID && it.Episode == episode {
			continue
		}
		if aired, err := s.onceAired(ctx, it.AnimeID, it.Episode); err != nil {
			return nil, err
		} else if aired == 0 {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func (s *Store) PlayOrder(ctx context.Context, animeID, episode, n int) (picks []Pick, fromList int, err error) {
	items, err := s.UpNextAfter(ctx, animeID, episode)
	if err != nil {
		return nil, 0, err
	}
	for _, it := range items[:min(n, len(items))] {
		picks = append(picks, Pick{AnimeID: it.AnimeID, Episode: it.Episode})
	}
	fromList = len(picks)
	last := Pick{AnimeID: animeID, Episode: episode}
	for len(picks) < n {
		if len(picks) > 0 {
			last = picks[len(picks)-1]
		}
		next, err := s.NextEpisode(ctx, last.AnimeID, last.Episode)
		if err != nil {
			return nil, 0, err
		}
		if next == 0 {
			sequel := s.NextSeason(ctx, last.AnimeID)
			if sequel == 0 || !s.isFinale(ctx, last) {
				break
			}
			picks = append(picks, Pick{AnimeID: sequel, Episode: 1})
			continue
		}
		picks = append(picks, Pick{AnimeID: last.AnimeID, Episode: next})
	}
	return picks, fromList, nil
}

func (s *Store) isFinale(ctx context.Context, p Pick) bool {
	total, _ := s.EpisodeCount(ctx, p.AnimeID)
	return total > 0 && p.Episode >= total
}

func leaveUpNext(ctx context.Context, tx *sql.Tx, animeID int, epKey string) error {
	episode, err := strconv.Atoi(epKey)
	if err != nil {
		return nil
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM up_next WHERE anime_id = ? AND episode = ?`, animeID, episode)
	return err
}
