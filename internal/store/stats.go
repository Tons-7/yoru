package store

import (
	"context"
	"time"
)

type Taste struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
	Shows   int     `json:"shows"`
}

type MonthSeconds struct {
	Month   string  `json:"month"`
	Seconds float64 `json:"seconds"`
}

type Stats struct {
	TotalSeconds float64        `json:"totalSeconds"`
	Episodes     int            `json:"episodes"`
	Completed    int            `json:"completed"`
	Shows        int            `json:"shows"`
	Genres       []Taste        `json:"genres"`
	Studios      []Taste        `json:"studios"`
	Seasons      []Taste        `json:"seasons"`
	Months       []MonthSeconds `json:"months"`
}

func (s *Store) Stats(ctx context.Context, now time.Time) (Stats, error) {
	var out Stats
	err := s.r.QueryRowContext(ctx, `
		SELECT coalesce((SELECT sum(seconds) FROM watch_time), 0),
		       (SELECT count(*) FROM list_entry WHERE status = 'COMPLETED'),
		       (SELECT count(DISTINCT anime_id) FROM playback WHERE played_s > 0)`).
		Scan(&out.TotalSeconds, &out.Completed, &out.Shows)
	if err != nil {
		return out, err
	}
	if out.Episodes, err = s.finishedEpisodes(ctx); err != nil {
		return out, err
	}
	if out.Genres, err = s.taste(ctx, `
		SELECT g.value, sum(p.played_s), count(DISTINCT p.anime_id)
		FROM playback p JOIN anime a ON a.id = p.anime_id, json_each(a.genres) g
		WHERE p.played_s > 0 GROUP BY g.value ORDER BY 2 DESC LIMIT 8`); err != nil {
		return out, err
	}
	if out.Studios, err = s.taste(ctx, `
		SELECT st.value, sum(p.played_s), count(DISTINCT p.anime_id)
		FROM playback p JOIN anime a ON a.id = p.anime_id, json_each(a.studios) st
		WHERE p.played_s > 0 GROUP BY st.value ORDER BY 2 DESC LIMIT 6`); err != nil {
		return out, err
	}
	if out.Seasons, err = s.taste(ctx, `
		SELECT a.season || ' ' || a.season_year, sum(p.played_s), count(DISTINCT p.anime_id)
		FROM playback p JOIN anime a ON a.id = p.anime_id
		WHERE p.played_s > 0 AND a.season IS NOT NULL AND a.season_year IS NOT NULL
		GROUP BY a.season, a.season_year ORDER BY 2 DESC LIMIT 6`); err != nil {
		return out, err
	}

	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -11, 0)
	rows, err := s.r.QueryContext(ctx, `
		SELECT substr(day, 1, 7), sum(seconds) FROM watch_time WHERE day >= ? GROUP BY 1`, start.Format("2006-01-02"))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	by := map[string]float64{}
	for rows.Next() {
		var month string
		var secs float64
		if err := rows.Scan(&month, &secs); err != nil {
			return out, err
		}
		by[month] = secs
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for i := range 12 {
		m := start.AddDate(0, i, 0).Format("2006-01")
		out.Months = append(out.Months, MonthSeconds{Month: m, Seconds: by[m]})
	}
	return out, nil
}

func (s *Store) taste(ctx context.Context, query string) ([]Taste, error) {
	rows, err := s.r.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Taste{}
	for rows.Next() {
		var t Taste
		if err := rows.Scan(&t.Name, &t.Seconds, &t.Shows); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
