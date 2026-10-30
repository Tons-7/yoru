package store

import (
	"context"
	"encoding/json"
	"strconv"
)

type AlertShow struct {
	AnimeID      int     `json:"animeId"`
	Title        string  `json:"title"`
	Cover        *string `json:"cover,omitempty"`
	Status       string  `json:"status,omitempty"`
	NextEpisode  int     `json:"nextEpisode,omitempty"`
	NextAiringAt int64   `json:"nextAiringAt,omitempty"`
	Because      string  `json:"because"`
}

func (s *Store) AlertShows(ctx context.Context) ([]AlertShow, error) {
	mode := s.TitleMode(ctx, 0)
	rows, err := s.r.QueryContext(ctx, `
		SELECT c.id, coalesce(a.title_romaji, ''), a.title_english, a.cover_url, coalesce(a.status, ''),
		       coalesce(a.next_episode, 0), coalesce(a.next_airing_at, 0), `+followColumns("c.id")+`
		FROM (
			SELECT anime_id AS id FROM follow
			UNION SELECT anime_id FROM list_entry WHERE status IN ('CURRENT', 'REPEATING')
			UNION SELECT anime_id FROM anime_pref
			      WHERE key = 'autodownload.enabled' AND value = 'true' OR key = 'autodownload.want' AND value <> ''
		) c
		LEFT JOIN anime a ON a.id = c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertShow{}
	for rows.Next() {
		var (
			show                     AlertShow
			romaji                   string
			english                  *string
			followed                 int
			status, mute, auto, want string
		)
		if err := rows.Scan(&show.AnimeID, &romaji, &english, &show.Cover, &show.Status, &show.NextEpisode,
			&show.NextAiringAt, &followed, &status, &mute, &auto, &want); err != nil {
			return nil, err
		}
		state := followState(followed == 1, status, mute, auto, want)
		if !state.On {
			continue
		}
		show.Title, show.Because = PickTitle(mode, romaji, english), state.Because
		out = append(out, show)
	}
	return out, rows.Err()
}

type ShowCard struct {
	Title string
	Cover *string
}

func (s *Store) ShowCards(ctx context.Context, ids []int) (map[int]ShowCard, error) {
	out := make(map[int]ShowCard, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	mode := s.TitleMode(ctx, 0)
	rows, err := s.r.QueryContext(ctx, `
		SELECT a.id, a.title_romaji, a.title_english, a.cover_url
		FROM anime a WHERE a.id IN (SELECT value FROM json_each(?))`, string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id      int
			romaji  string
			english *string
			card    ShowCard
		)
		if err := rows.Scan(&id, &romaji, &english, &card.Cover); err != nil {
			return nil, err
		}
		card.Title = PickTitle(mode, romaji, english)
		out[id] = card
	}
	return out, rows.Err()
}

type WatchRow struct {
	Follow
	Notify, Auto, Want string
	First              int
}

const watchQuery = `WITH f(anime_id, episodes, progress, next_episode, next_airing_at, end_date) AS (` + followQuery + `)
	SELECT f.anime_id, f.episodes, f.progress, f.next_episode, f.next_airing_at, f.end_date,
	       coalesce((SELECT value FROM anime_pref WHERE anime_id = f.anime_id AND key = 'notify.enabled'), ''),
	       coalesce((SELECT value FROM anime_pref WHERE anime_id = f.anime_id AND key = 'autodownload.enabled'), ''),
	       coalesce((SELECT value FROM anime_pref WHERE anime_id = f.anime_id AND key = 'autodownload.want'), ''),
	       coalesce((SELECT min(episode) FROM notified_release WHERE anime_id = f.anime_id AND episode > 0), 0)
	FROM f`

func (s *Store) WatchRows(ctx context.Context) ([]WatchRow, error) {
	rows, err := s.r.QueryContext(ctx, watchQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchRow
	for rows.Next() {
		var r WatchRow
		if err := rows.Scan(&r.AnimeID, &r.Episodes, &r.Progress, &r.NextEpisode, &r.NextAiringAt, &r.EndDate,
			&r.Notify, &r.Auto, &r.Want, &r.First); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ReadyEpisodes(ctx context.Context, ids []int) (map[[2]int]bool, error) {
	out := map[[2]int]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `
		WITH ids(id) AS (SELECT value FROM json_each(?1))
		SELECT anime_id, episode FROM notified_release WHERE anime_id IN (SELECT id FROM ids) AND episode > 0
		UNION
		SELECT anime_id, CAST(ep_key AS INTEGER) FROM torrent_file
		WHERE anime_id IN (SELECT id FROM ids) AND selected = 1 AND ep_key <> '' AND ep_key NOT GLOB '*[^0-9]*'
		UNION
		SELECT anime_id, episode FROM local_file
		WHERE anime_id IN (SELECT id FROM ids) AND missing = 0 AND episode > 0`, string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, episode int
		if err := rows.Scan(&id, &episode); err != nil {
			return nil, err
		}
		out[[2]int{id, episode}] = true
	}
	return out, rows.Err()
}

func (s *Store) ResumePoints(ctx context.Context, ids []int) (map[[2]int][2]float64, error) {
	out := map[[2]int][2]float64{}
	if len(ids) == 0 {
		return out, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.r.QueryContext(ctx, `
		SELECT anime_id, ep_key, position_s, coalesce(duration_s, 0), watched, coalesce(done_s, 0) FROM playback
		WHERE anime_id IN (SELECT value FROM json_each(?)) AND position_s > 0
		  AND ep_key <> '' AND ep_key NOT GLOB '*[^0-9]*'`, string(list))
	if err != nil {
		return nil, err
	}
	type point struct {
		id                       int
		key                      string
		position, duration, done float64
		watched                  bool
	}
	var points []point
	for rows.Next() {
		var p point
		if err := rows.Scan(&p.id, &p.key, &p.position, &p.duration, &p.watched, &p.done); err != nil {
			rows.Close()
			return nil, err
		}
		points = append(points, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	threshold := s.thresholds(ctx)
	for _, p := range points {
		episode, err := strconv.Atoi(p.key)
		if err != nil || p.duration <= 0 || !resumable(p.position, p.duration, p.watched, threshold(p.id), p.done) {
			continue
		}
		out[[2]int{p.id, episode}] = [2]float64{p.position, p.duration}
	}
	return out, nil
}
