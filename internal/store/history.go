package store

import (
	"context"
	"strconv"
	"time"
)

type HistoryEntry struct {
	AnimeID    int      `json:"animeId"`
	Title      string   `json:"title"`
	Romaji     string   `json:"romaji"`
	English    *string  `json:"english,omitempty"`
	Cover      *string  `json:"cover,omitempty"`
	Thumb      *string  `json:"thumb,omitempty"`
	EpKey      string   `json:"epKey"`
	Episode    int      `json:"episode"`
	Position   float64  `json:"position"`
	Duration   *float64 `json:"duration,omitempty"`
	Percent    float64  `json:"percent"`
	Watched    bool     `json:"watched"`
	Dismissed  bool     `json:"dismissed"`
	PlayCount  int      `json:"playCount"`
	LastPlayed int64    `json:"lastPlayed"`
	Finished   bool     `json:"finished"`

	Progress     int     `json:"progress"`
	ListStatus   *string `json:"listStatus,omitempty"`
	Episodes     *int    `json:"episodes,omitempty"`
	Airing       *string `json:"airing,omitempty"`
	NextEpisode  *int    `json:"nextEpisode,omitempty"`
	NextAiringAt *int    `json:"nextAiringAt,omitempty"`
}

const historyWhere = `
WHERE p.last_played_at > 0 AND (p.position_s > 0 OR p.watched = 1 OR p.played_s > 0 OR p.play_count > 0)`

const historyQuery = `
SELECT a.id, a.title_romaji, a.title_english, a.cover_url, a.cover_medium,
       p.ep_key, ep.number, p.position_s, p.duration_s, coalesce(p.done_s, 0),
       p.watched, p.dismissed, p.play_count, p.last_played_at,
       coalesce(e.progress, 0), e.status, a.episode_count, a.status, a.next_episode, a.next_airing_at
FROM playback p
JOIN anime a ON a.id = p.anime_id
LEFT JOIN episode ep ON ep.anime_id = p.anime_id AND ep.ep_key = p.ep_key
LEFT JOIN list_entry e ON e.anime_id = p.anime_id` + historyWhere + `
ORDER BY p.last_played_at DESC, p.rowid DESC
LIMIT ? OFFSET ?`

func (s *Store) History(ctx context.Context, p Paging) (Page[HistoryEntry], error) {
	mode := s.TitleMode(ctx, 0)

	rows, err := s.r.QueryContext(ctx, historyQuery, p.PerPage+1, p.Offset())
	if err != nil {
		return Page[HistoryEntry]{}, err
	}
	defer rows.Close()

	out := []HistoryEntry{}
	done := []float64{}
	for rows.Next() {
		var (
			e                  HistoryEntry
			number             *int
			watched, dismissed int
			duration           *float64
			doneAt             float64
		)
		err := rows.Scan(&e.AnimeID, &e.Romaji, &e.English, &e.Cover, &e.Thumb,
			&e.EpKey, &number, &e.Position, &duration, &doneAt,
			&watched, &dismissed, &e.PlayCount, &e.LastPlayed,
			&e.Progress, &e.ListStatus, &e.Episodes, &e.Airing, &e.NextEpisode, &e.NextAiringAt)
		if err != nil {
			return Page[HistoryEntry]{}, err
		}

		e.Duration = duration
		e.Watched, e.Dismissed = watched == 1, dismissed == 1
		e.Title = PickTitle(mode, e.Romaji, e.English)
		if number != nil {
			e.Episode = *number
		} else {
			e.Episode = episodeFromKey(e.EpKey)
		}
		if duration != nil && *duration > 0 {
			e.Percent = e.Position / *duration * 100
		}
		out = append(out, e)
		done = append(done, doneAt)
	}
	if err := rows.Err(); err != nil {
		return Page[HistoryEntry]{}, err
	}
	rows.Close()

	threshold := s.thresholds(ctx)
	for i := range out {
		e := &out[i]
		var duration float64
		if e.Duration != nil {
			duration = *e.Duration
		}
		e.Finished = playedThrough(played{e.AnimeID, e.Position, duration, e.Watched, done[i]}, threshold)
	}

	total, err := s.countRows(ctx, `SELECT count(*) FROM playback p`+historyWhere)
	if err != nil {
		return Page[HistoryEntry]{}, err
	}
	return NewPage(out, p, total), nil
}

func (s *Store) ForgetHistory(ctx context.Context, animeID int, epKey string) (int, error) {
	var (
		res interface{ RowsAffected() (int64, error) }
		err error
	)
	switch {
	case animeID != 0 && epKey != "":
		res, err = s.w.ExecContext(ctx,
			`DELETE FROM playback WHERE anime_id = ? AND ep_key = ?`, animeID, epKey)
	case animeID != 0:
		res, err = s.w.ExecContext(ctx, `DELETE FROM playback WHERE anime_id = ?`, animeID)
	default:
		res, err = s.w.ExecContext(ctx, `DELETE FROM playback`)
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

type WatchStats struct {
	WeekSeconds  float64 `json:"weekSeconds"`
	MonthSeconds float64 `json:"monthSeconds"`
	TotalSeconds float64 `json:"totalSeconds"`
	Episodes     int     `json:"episodes"`
	Completed    int     `json:"completed"`

	Days []DaySeconds `json:"days"`
}

type DaySeconds struct {
	Day     string  `json:"day"`
	Seconds float64 `json:"seconds"`
}

type played struct {
	animeID            int
	position, duration float64
	watched            bool
	done               float64
}

func playedThrough(p played, threshold func(animeID int) float64) bool {
	return finished(p.position, p.duration, p.watched, threshold(p.animeID), p.done)
}

func (s *Store) thresholds(ctx context.Context) func(animeID int) float64 {
	global := s.thresholdFor(ctx, 0)
	own := map[int]string{}
	if rows, err := s.r.QueryContext(ctx, `SELECT anime_id, value FROM anime_pref WHERE key = 'sync.progress_at'`); err == nil {
		for rows.Next() {
			var id int
			var v string
			if rows.Scan(&id, &v) == nil {
				own[id] = v
			}
		}
		if rows.Err() != nil {
			clear(own)
		}
		rows.Close()
	}
	return func(animeID int) float64 {
		v, ok := own[animeID]
		if !ok {
			return global
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			return f
		}
		return defaultThreshold()
	}
}

func (s *Store) finishedEpisodes(ctx context.Context) (int, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT p.anime_id, p.position_s, coalesce(p.duration_s, 0), p.watched, coalesce(p.done_s, 0)
		FROM playback p`+historyWhere)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var all []played
	for rows.Next() {
		var p played
		if err := rows.Scan(&p.animeID, &p.position, &p.duration, &p.watched, &p.done); err != nil {
			return 0, err
		}
		all = append(all, p)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()

	threshold := s.thresholds(ctx)
	n := 0
	for _, p := range all {
		if p.watched || playedThrough(p, threshold) {
			n++
		}
	}
	return n, nil
}

func (s *Store) WatchStats(ctx context.Context, now time.Time) (WatchStats, error) {
	var out WatchStats
	week := now.AddDate(0, 0, -6).Format("2006-01-02")
	month := now.AddDate(0, 0, -29).Format("2006-01-02")
	err := s.r.QueryRowContext(ctx, `
		SELECT coalesce(sum(CASE WHEN day >= ?1 THEN seconds END), 0),
		       coalesce(sum(CASE WHEN day >= ?2 THEN seconds END), 0),
		       coalesce(sum(seconds), 0),
		       (SELECT count(*) FROM list_entry WHERE status = 'COMPLETED')
		FROM watch_time`, week, month).
		Scan(&out.WeekSeconds, &out.MonthSeconds, &out.TotalSeconds, &out.Completed)
	if err != nil {
		return out, err
	}
	if out.Episodes, err = s.finishedEpisodes(ctx); err != nil {
		return out, err
	}

	rows, err := s.r.QueryContext(ctx, `SELECT day, seconds FROM watch_time WHERE day >= ?`, month)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	by := map[string]float64{}
	for rows.Next() {
		var day string
		var secs float64
		if err := rows.Scan(&day, &secs); err != nil {
			return out, err
		}
		by[day] = secs
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.Days = make([]DaySeconds, 0, 30)
	for i := 29; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		out.Days = append(out.Days, DaySeconds{Day: day, Seconds: by[day]})
	}
	return out, nil
}

func episodeFromKey(key string) int {
	n, err := strconv.Atoi(key)
	if err != nil {
		return 0
	}
	return n
}
