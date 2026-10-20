package store

import (
	"context"
	"database/sql"
	"slices"
	"strconv"
	"time"
)

// UpNextItem is one episode on the Up next list, in play order.
type UpNextItem struct {
	AnimeID      int     `json:"animeId"`
	Episode      int     `json:"episode"`
	Title        string  `json:"title"`
	Cover        *string `json:"cover,omitempty"`
	EpisodeTitle string  `json:"episodeTitle,omitempty"`
	Minutes      int     `json:"minutes,omitempty"`
	// Where a started episode resumes, for the card's bar.
	Position float64 `json:"position,omitempty"`
	Duration float64 `json:"duration,omitempty"`
}

// Pick is an episode to play.
type Pick struct {
	AnimeID int `json:"animeId"`
	Episode int `json:"episode"`
}

// UpNext is the list top to bottom: shows in their places, each show's episodes in episode order.
func (s *Store) UpNext(ctx context.Context) ([]UpNextItem, error) {
	mode := s.TitleMode(ctx, 0)
	rows, err := s.r.QueryContext(ctx, `
		SELECT u.anime_id, u.episode, a.title_romaji, a.title_english, a.cover_url,
		       coalesce(ep.title_en, ''), coalesce(ep.runtime, a.duration, 0),
		       coalesce(pb.position_s, 0), coalesce(pb.duration_s, 0)
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
			&it.EpisodeTitle, &it.Minutes, &it.Position, &it.Duration); err != nil {
			return nil, err
		}
		it.Title = PickTitle(mode, romaji, english)
		out = append(out, it)
	}
	return out, rows.Err()
}

// AddUpNext puts an episode on the list: with its show's other episodes if any are there, else at the bottom.
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

// RemoveUpNext takes an episode off the list, or with episode 0 the whole show.
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

// MoveUpNext puts a show at place to among the list's shows (0 is the top); its episodes go with it.
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

// FillUpNext adds the next aired unwatched episode of every show being watched, the latest watched first.
// A show already on the list, or caught up, is skipped, so a second press adds nothing.
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

// UpNextAfter is the list as it plays once an episode ends: without that episode, or ones still to broadcast.
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

// PlayOrder is what plays after an episode, up to n: the Up next list top to bottom, then past its end the next
// episodes of the last show. fromList counts the picks that came from the list.
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
			break
		}
		picks = append(picks, Pick{AnimeID: last.AnimeID, Episode: next})
	}
	return picks, fromList, nil
}

// leaveUpNext takes a watched episode off the list, as a rewatch keeps progress where it was.
func leaveUpNext(ctx context.Context, tx *sql.Tx, animeID int, epKey string) error {
	episode, err := strconv.Atoi(epKey)
	if err != nil {
		return nil
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM up_next WHERE anime_id = ? AND episode = ?`, animeID, episode)
	return err
}
