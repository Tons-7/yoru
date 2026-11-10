package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *Store) AnimeRecord(ctx context.Context, id int) (Anime, time.Time, bool, error) {
	var (
		a      Anime
		synced int64
	)
	err := s.r.QueryRowContext(ctx, `
		SELECT id, mal_id, title_romaji, title_english, title_native, coalesce(synonyms, '[]'),
		       format, status, episode_count, duration, season, season_year,
		       country, coalesce(is_adult, 0), description, cover_url, cover_color, cover_medium, banner_url,
		       average_score, mean_score, popularity, favourites,
		       coalesce(genres, '[]'), coalesce(studios, '[]'), coalesce(tags, '[]'), coalesce(links, '[]'),
		       source, coalesce(start_date, ''), coalesce(end_date, ''),
		       trailer_id, trailer_site, next_episode, next_airing_at, synced_at
		FROM anime WHERE id = ? AND synced_at > 0`, id).Scan(
		&a.ID, &a.MalID, &a.Romaji, &a.English, &a.Native, &a.Synonyms,
		&a.Format, &a.Status, &a.Episodes, &a.Duration, &a.Season, &a.SeasonYear,
		&a.Country, &a.IsAdult, &a.Description, &a.Cover, &a.CoverColor, &a.CoverMedium, &a.Banner,
		&a.AverageScore, &a.MeanScore, &a.Popularity, &a.Favourites,
		&a.Genres, &a.Studios, &a.Tags, &a.Links,
		&a.Source, &a.StartDate, &a.EndDate,
		&a.TrailerID, &a.TrailerSite, &a.NextEpisode, &a.NextAiringAt, &synced)
	if errors.Is(err, sql.ErrNoRows) {
		return Anime{}, time.Time{}, false, nil
	}
	if err != nil {
		return Anime{}, time.Time{}, false, err
	}
	return a, time.Unix(synced, 0), true, nil
}

type KeptMedia struct {
	Media        string
	Status       string
	SyncedAt     time.Time
	NextAiringAt int64
}

func (s *Store) KeptMedia(ctx context.Context, id int) (KeptMedia, bool) {
	var (
		k      KeptMedia
		synced int64
	)
	err := s.r.QueryRowContext(ctx, `
		SELECT media, coalesce(status, ''), synced_at, coalesce(next_airing_at, 0) FROM anime
		WHERE id = ? AND media IS NOT NULL AND synced_at > 0`, id).Scan(&k.Media, &k.Status, &synced, &k.NextAiringAt)
	k.SyncedAt = time.Unix(synced, 0)
	return k, err == nil
}

const (
	airingMaxAge   = 6 * time.Hour
	upcomingMaxAge = 24 * time.Hour
)

func (s *Store) StaleAiring(ctx context.Context, now time.Time, limit int) ([]int, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT id FROM anime
		WHERE id > 0 AND synced_at > 0
		  AND status IN ('RELEASING', 'NOT_YET_RELEASED')
		  AND id NOT IN (SELECT anime_id FROM dead_anime)
		  AND ((next_airing_at IS NOT NULL AND next_airing_at <= ?1 AND synced_at < next_airing_at)
		       OR synced_at < ?1 - CASE status WHEN 'RELEASING' THEN ?2 ELSE ?3 END)
		ORDER BY (next_airing_at IS NOT NULL AND next_airing_at <= ?1) DESC, synced_at
		LIMIT ?4`,
		now.Unix(), int64(airingMaxAge.Seconds()), int64(upcomingMaxAge.Seconds()), limit)
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

func (s *Store) MarkSynced(ctx context.Context, ids []int, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	args = append(args, at.Unix())
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.w.ExecContext(ctx, `
		UPDATE anime SET synced_at = ?1
		WHERE synced_at < ?1
		  AND NOT (next_airing_at IS NOT NULL AND next_airing_at <= ?1 AND synced_at < next_airing_at)
		  AND id IN (`+marks+`)`, args...)
	return err
}
