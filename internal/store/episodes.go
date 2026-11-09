package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"yoru/internal/metadata"
)

type EpisodeRow struct {
	AnimeID  int     `json:"animeId"`
	EpKey    string  `json:"epKey"`
	Number   int     `json:"number"`
	Absolute *int    `json:"absolute,omitempty"`
	TitleEN  *string `json:"titleEn,omitempty"`
	TitleJA  *string `json:"titleJa,omitempty"`
	Overview *string `json:"overview,omitempty"`
	Still    *string `json:"still,omitempty"`
	AirDate  *int64  `json:"airDate,omitempty"`
	Runtime  *int    `json:"runtime,omitempty"`

	Display int `json:"display"`

	Planned bool `json:"planned,omitempty"`

	Filler    string   `json:"filler,omitempty"`
	Recap     bool     `json:"recap"`
	Watched   bool     `json:"watched"`
	Position  float64  `json:"position,omitempty"`
	Duration  float64  `json:"duration,omitempty"`
	Resumable bool     `json:"resumable,omitempty"`
	OnDisk    bool     `json:"onDisk"`
	Local     bool     `json:"local,omitempty"`
	Spoiler   bool     `json:"spoiler,omitempty"`
	Score     *float64 `json:"score,omitempty"`
	Forum     *string  `json:"forumUrl,omitempty"`
}

func (s *Store) SaveEpisodes(ctx context.Context, animeID int, eps []metadata.Episode) (int, error) {
	if len(eps) == 0 {
		return 0, nil
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO episode (anime_id, ep_key, number, tvdb_number, absolute, season_number, is_special,
		                     title_en, title_ja, overview, still_url, air_date, runtime, anidb_eid)
		VALUES (?,?,?,?,?,?,0,?,?,?,?,?,?,?)
		ON CONFLICT(anime_id, ep_key) DO UPDATE SET
		    tvdb_number=excluded.tvdb_number,
		    season_number=excluded.season_number,
		    absolute=excluded.absolute,
		    title_en=coalesce(excluded.title_en, episode.title_en),
		    title_ja=coalesce(excluded.title_ja, episode.title_ja),
		    overview=coalesce(excluded.overview, episode.overview),
		    still_url=coalesce(excluded.still_url, episode.still_url),
		    air_date=coalesce(excluded.air_date, episode.air_date),
		    runtime=coalesce(excluded.runtime, episode.runtime)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	keys := make([]any, 0, len(eps)+1)
	keys = append(keys, animeID)
	for _, e := range eps {
		if _, err := stmt.ExecContext(ctx, animeID, fmt.Sprint(e.Number), e.Number,
			nullableInt(e.TvdbNumber), nullableInt(e.Absolute), nullableInt(e.Season),
			nullable(e.TitleEN), nullable(e.TitleJA), nullable(e.Overview),
			nullable(e.Image), nullableInt64(e.AirDate), nullableInt(e.Runtime),
			nullableInt(e.AniDBID)); err != nil {
			return 0, err
		}
		keys = append(keys, fmt.Sprint(e.Number))
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM episode WHERE anime_id = ? AND is_special = 0
		  AND ep_key NOT IN (`+placeholders(len(eps))+`)`, keys...); err != nil {
		return 0, err
	}
	return len(eps), tx.Commit()
}

func (s *Store) SaveFillers(ctx context.Context, rows []metadata.Filler) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO filler (mal_id, number, kind, source) VALUES (?,?,?,'anifiller')
		ON CONFLICT(mal_id, number) DO UPDATE SET kind=excluded.kind, source=excluded.source`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, f := range rows {
		if _, err := stmt.ExecContext(ctx, f.MalID, f.Episode, f.Kind); err != nil {
			return 0, err
		}
	}
	return len(rows), tx.Commit()
}

func (s *Store) UnclassifiedEpisodes(ctx context.Context, animeID int) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `
		SELECT count(*)
		FROM episode e
		JOIN anime a ON a.id = e.anime_id
		LEFT JOIN filler f ON f.mal_id = a.mal_id AND f.number = e.number
		WHERE e.anime_id = ? AND a.mal_id IS NOT NULL
		  AND (f.kind IS NULL OR f.kind IN ('', 'unknown'))`, animeID).Scan(&n)
	return n, err
}

func (s *Store) SaveFillerGaps(ctx context.Context, malID int, rows []metadata.Filler) (int, error) {
	if malID == 0 || len(rows) == 0 {
		return 0, nil
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO filler (mal_id, number, kind, source)
		VALUES (?,?,?,'animefillerlist')
		ON CONFLICT(mal_id, number) DO UPDATE SET
		    kind = CASE WHEN filler.kind IN ('', 'unknown') THEN excluded.kind ELSE filler.kind END,
		    source = CASE WHEN filler.kind IN ('', 'unknown') THEN excluded.source ELSE filler.source END`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var filled int
	for _, r := range rows {
		if r.Episode == 0 || r.Kind == "" || r.Kind == "unknown" {
			continue
		}
		res, err := stmt.ExecContext(ctx, malID, r.Episode, r.Kind)
		if err != nil {
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			filled++
		}
	}
	return filled, tx.Commit()
}

func (s *Store) SaveFlags(ctx context.Context, malID int, flags []metadata.EpisodeFlags) (int, error) {
	if malID == 0 || len(flags) == 0 {
		return 0, nil
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var recaps int
	for _, f := range flags {
		kind := "unknown"
		if f.Filler {
			kind = "filler"
		}
		if f.Recap {
			recaps++
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO filler (mal_id, number, kind, source, recap, recap_source)
			VALUES (?,?,?,'mal',?, 'mal')
			ON CONFLICT(mal_id, number) DO UPDATE SET
			    recap=excluded.recap,
			    recap_source=excluded.recap_source,
			    kind=CASE WHEN filler.kind IN ('', 'unknown') THEN excluded.kind ELSE filler.kind END`,
			malID, f.Episode, kind, boolInt(f.Recap)); err != nil {
			return 0, err
		}
	}
	return recaps, tx.Commit()
}

func (s *Store) SaveScores(ctx context.Context, malID int, scores []metadata.EpisodeScore) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, sc := range scores {
		var score any
		if sc.Score > 0 {
			score = sc.Score
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO episode_score (mal_id, number, score, forum_url) VALUES (?,?,?,?)
			ON CONFLICT(mal_id, number) DO UPDATE SET score=excluded.score, forum_url=excluded.forum_url`,
			malID, sc.Episode, score, nullable(sc.Forum)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.MarkSource(ctx, scoresKey(malID), len(scores))
}

const scoresRetry = 7 * 24 * time.Hour

func (s *Store) ScoresDue(ctx context.Context, animeID, malID int) bool {
	if malID == 0 {
		return false
	}
	if at, err := s.SourceRefreshedAt(ctx, scoresKey(malID)); err != nil || time.Since(at) < scoresRetry {
		return false
	}
	var watched bool
	s.r.QueryRowContext(ctx, `
		SELECT coalesce((SELECT progress FROM list_entry WHERE anime_id = ?), 0) > 0
		    OR EXISTS (SELECT 1 FROM playback WHERE anime_id = ? AND watched = 1)`,
		animeID, animeID).Scan(&watched)
	return watched
}

func scoresKey(malID int) string {
	return "scores:" + strconv.Itoa(malID)
}

func (s *Store) SaveSkips(ctx context.Context, malID, episode int, skips []metadata.SkipTime) error {
	if len(skips) == 0 {
		return nil
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, k := range skips {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO skip_time (mal_id, number, kind, start_s, end_s, skip_id)
			VALUES (?,?,?,?,?,?)
			ON CONFLICT(mal_id, number, kind) DO UPDATE SET
			    start_s=excluded.start_s, end_s=excluded.end_s, skip_id=excluded.skip_id`,
			malID, episode, k.Kind, k.Start, k.End, nullable(k.SkipID)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const episodeListQuery = `
SELECT e.anime_id, e.ep_key, e.number, e.absolute,
       e.title_en, e.title_ja, e.overview, e.still_url, e.air_date, e.runtime,
       coalesce(f.user_kind, f.kind, ''), coalesce(f.recap, 0),
       coalesce(p.watched, 0) OR e.number <= coalesce(l.progress, 0),
       coalesce(p.position_s, 0), coalesce(p.duration_s, 0),
       EXISTS (SELECT 1 FROM torrent_file tf
               WHERE tf.anime_id = e.anime_id AND tf.ep_key = e.ep_key AND tf.selected = 1),
       coalesce(p.done_s, 0), sc.score, sc.forum_url
FROM episode e
LEFT JOIN anime a          ON a.id = e.anime_id
LEFT JOIN list_entry l     ON l.anime_id = e.anime_id
LEFT JOIN filler f         ON f.mal_id = a.mal_id AND f.number = e.number
LEFT JOIN episode_score sc ON sc.mal_id = a.mal_id AND sc.number = e.number
LEFT JOIN playback p       ON p.anime_id = e.anime_id AND p.ep_key = e.ep_key
WHERE e.anime_id = ?
ORDER BY e.number`

func (s *Store) Episodes(ctx context.Context, animeID int) ([]EpisodeRow, error) {
	threshold := s.thresholdFor(ctx, animeID)

	rows, err := s.r.QueryContext(ctx, episodeListQuery, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []EpisodeRow{}
	for rows.Next() {
		var e EpisodeRow
		var watched, recap int
		var done float64
		if err := rows.Scan(&e.AnimeID, &e.EpKey, &e.Number, &e.Absolute,
			&e.TitleEN, &e.TitleJA, &e.Overview, &e.Still, &e.AirDate, &e.Runtime,
			&e.Filler, &recap, &watched, &e.Position, &e.Duration, &e.OnDisk, &done,
			&e.Score, &e.Forum); err != nil {
			return nil, err
		}
		e.Watched = watched == 1
		if !e.Watched {
			e.Score, e.Forum = nil, nil
		}
		e.Resumable = resumable(e.Position, e.Duration, e.Watched, threshold, done)
		e.Recap = recap == 1
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(out) == 0 {
		planned, err := s.plannedEpisodes(ctx, animeID)
		if err != nil {
			return nil, err
		}
		return s.asScheduled(ctx, animeID, s.asItself(ctx, animeID, s.markLocal(ctx, animeID, planned))), nil
	}

	out = s.padToCount(ctx, animeID, out)
	for i := range out {
		out[i].Display = out[i].Number
	}
	return s.markSpoilers(ctx, animeID, s.asScheduled(ctx, animeID, s.asItself(ctx, animeID, s.markLocal(ctx, animeID, out)))), nil
}

func (s *Store) markSpoilers(ctx context.Context, animeID int, out []EpisodeRow) []EpisodeRow {
	mode := s.SpoilerMode(ctx, animeID)
	if mode == SpoilersOff {
		return out
	}
	now := time.Now().Unix()
	next := 0
	for _, e := range out {
		if !e.Watched && (e.AirDate == nil || *e.AirDate <= now) {
			next = e.Number
			break
		}
	}
	if next == 0 && len(out) > 0 {
		next = out[len(out)-1].Number + 1
	}
	for i := range out {
		out[i].Spoiler = HidesSpoilers(mode, out[i].Number, next, out[i].Watched)
	}
	return out
}

func (s *Store) asScheduled(ctx context.Context, animeID int, out []EpisodeRow) []EpisodeRow {
	var next int
	var at int64
	if err := s.r.QueryRowContext(ctx,
		`SELECT coalesce(next_episode, 0), coalesce(next_airing_at, 0) FROM anime WHERE id = ?`,
		animeID).Scan(&next, &at); err != nil || next <= 0 || at <= 0 {
		return out
	}
	now := time.Now().Unix()
	for i := range out {
		e := &out[i]
		switch {
		case e.Number == next:
			e.AirDate = &at
		case e.Number < next && e.AirDate != nil && *e.AirDate > now:
			e.AirDate = &now
		case e.Number > next && e.AirDate != nil && *e.AirDate < at:
			e.AirDate = &at
		}
	}
	return out
}

func (s *Store) markLocal(ctx context.Context, animeID int, out []EpisodeRow) []EpisodeRow {
	rows, err := s.r.QueryContext(ctx,
		`SELECT DISTINCT episode FROM local_file WHERE anime_id = ? AND missing = 0 AND episode IS NOT NULL`, animeID)
	if err != nil {
		return out
	}
	defer rows.Close()
	have := map[int]bool{}
	for rows.Next() {
		var n int
		if rows.Scan(&n) == nil {
			have[n] = true
		}
	}
	if rows.Err() != nil {
		return out
	}
	for i := range out {
		out[i].Local = have[out[i].Number]
	}
	return out
}

var (
	lineBreak = regexp.MustCompile(`(?i)<br\s*/?>`)
	markup    = regexp.MustCompile(`<[^>]*>`)
)

func plain(description string) string {
	text := markup.ReplaceAllString(lineBreak.ReplaceAllString(description, " "), "")
	return strings.Join(strings.Fields(html.UnescapeString(text)), " ")
}

func (s *Store) asItself(ctx context.Context, animeID int, out []EpisodeRow) []EpisodeRow {
	if len(out) != 1 {
		return out
	}
	var format, description, art *string
	var count, minutes *int
	var status string
	if err := s.r.QueryRowContext(ctx, `
		SELECT format, episode_count, description, coalesce(banner_url, cover_url), duration, coalesce(status, '')
		FROM anime WHERE id = ?`, animeID).Scan(&format, &count, &description, &art, &minutes, &status); err != nil {
		return out
	}
	if count == nil || *count != 1 {
		return out
	}

	row := &out[0]
	if status != "NOT_YET_RELEASED" {
		row.Planned = false
	}
	if row.TitleEN == nil && row.TitleJA == nil && format != nil && *format == "MOVIE" {
		title := "Complete Movie"
		row.TitleEN = &title
	}
	if (row.Overview == nil || *row.Overview == "") && description != nil {
		if text := plain(*description); text != "" {
			row.Overview = &text
		}
	}
	if row.Still == nil || *row.Still == "" {
		row.Still = art
	}
	if row.Runtime == nil {
		row.Runtime = minutes
	}
	return out
}

func (s *Store) padToCount(ctx context.Context, animeID int, out []EpisodeRow) []EpisodeRow {
	for _, e := range out {
		if e.TitleEN != nil || e.AirDate != nil {
			return out
		}
	}

	total, _ := s.EpisodeLimit(ctx, animeID)
	if total <= 0 {
		return out
	}

	last := out[len(out)-1].Number
	first := out[0].Number
	for n := last + 1; n-first+1 <= total && n-last <= 5000; n++ {
		out = append(out, EpisodeRow{
			AnimeID: animeID,
			EpKey:   strconv.Itoa(n),
			Number:  n,
			Planned: true,
		})
	}
	return out
}

func (s *Store) EpisodeLimit(ctx context.Context, animeID int) (limit int, firm bool) {
	if total, _ := s.EpisodeCount(ctx, animeID); total > 0 {
		return total, true
	}
	return s.scheduledThrough(ctx, animeID), false
}

func Slack(firm bool) int {
	if firm {
		return 0
	}
	return 3
}

func (s *Store) plannedEpisodes(ctx context.Context, animeID int) ([]EpisodeRow, error) {
	total, _ := s.EpisodeLimit(ctx, animeID)
	if total <= 0 {
		return []EpisodeRow{}, nil
	}
	if total > 5000 {
		total = 5000
	}

	played, err := s.playbackByEpisode(ctx, animeID)
	if err != nil {
		return nil, err
	}
	cached, err := s.cachedEpisodes(ctx, animeID)
	if err != nil {
		return nil, err
	}
	entry, err := s.ListEntry(ctx, animeID)
	if err != nil {
		return nil, err
	}

	threshold := s.thresholdFor(ctx, animeID)
	out := make([]EpisodeRow, 0, total)
	for n := 1; n <= total; n++ {
		key := strconv.Itoa(n)
		row := EpisodeRow{AnimeID: animeID, EpKey: key, Number: n, Display: n, Planned: true}
		if p, ok := played[key]; ok {
			row.Watched, row.Position, row.Duration = p.watched, p.position, p.duration
			row.Resumable = resumable(p.position, p.duration, p.watched, threshold, p.done)
		}
		row.Watched = row.Watched || n <= entry.Progress
		_, row.OnDisk = cached[key]
		out = append(out, row)
	}
	return out, nil
}

func (s *Store) scheduledThrough(ctx context.Context, animeID int) int {
	var next int
	if err := s.r.QueryRowContext(ctx,
		`SELECT coalesce(next_episode, 0) FROM anime WHERE id = ?`, animeID).Scan(&next); err != nil {
		return 0
	}
	return next
}

type playedEpisode struct {
	watched  bool
	position float64
	duration float64
	done     float64
}

func (s *Store) playbackByEpisode(ctx context.Context, animeID int) (map[string]playedEpisode, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT ep_key, watched, position_s, coalesce(duration_s, 0), coalesce(done_s, 0) FROM playback WHERE anime_id = ?`,
		animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]playedEpisode{}
	for rows.Next() {
		var key string
		var watched int
		var p playedEpisode
		if err := rows.Scan(&key, &watched, &p.position, &p.duration, &p.done); err != nil {
			return nil, err
		}
		p.watched = watched == 1
		out[key] = p
	}
	return out, rows.Err()
}

func (s *Store) cachedEpisodes(ctx context.Context, animeID int) (map[string]struct{}, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT ep_key FROM torrent_file WHERE anime_id = ? AND selected = 1`, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]struct{}{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out[key] = struct{}{}
	}
	return out, rows.Err()
}

func (s *Store) NextEpisode(ctx context.Context, animeID, after int) (int, error) {
	prefs, err := s.Prefs(ctx, animeID)
	if err != nil {
		return 0, err
	}
	total, _ := s.EpisodeCount(ctx, animeID)

	if !prefs.Bool("playback.skip_filler") {
		if total > 0 && after+1 > total {
			return 0, nil
		}
		return s.onceAired(ctx, animeID, after+1)
	}

	var next *int
	err = s.r.QueryRowContext(ctx, `
		SELECT min(e.number) FROM episode e
		LEFT JOIN anime a  ON a.id = e.anime_id
		LEFT JOIN filler f ON f.mal_id = a.mal_id AND f.number = e.number
		WHERE e.anime_id = ? AND e.is_special = 0 AND e.number > ?
		  AND coalesce(f.user_kind, f.kind, '') <> 'filler' AND coalesce(f.recap, 0) = 0`,
		animeID, after).Scan(&next)
	if err != nil {
		return 0, err
	}
	if next == nil {
		var last int
		if err := s.r.QueryRowContext(ctx,
			`SELECT coalesce(max(number), 0) FROM episode WHERE anime_id = ? AND is_special = 0`, animeID).Scan(&last); err != nil {
			return 0, err
		}
		n := max(last, after) + 1
		next = &n
	}
	if total > 0 && *next > total {
		return 0, nil
	}
	return s.onceAired(ctx, animeID, *next)
}

func (s *Store) onceAired(ctx context.Context, animeID, episode int) (int, error) {
	var nextEpisode int
	var airingAt int64
	err := s.r.QueryRowContext(ctx,
		`SELECT coalesce(next_episode, 0), coalesce(next_airing_at, 0) FROM anime WHERE id = ?`,
		animeID).Scan(&nextEpisode, &airingAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if !aired(episode, nextEpisode, airingAt, time.Now().Unix()) {
		return 0, nil
	}
	return episode, nil
}

type EpisodeAlias struct {
	Tvdb                       int
	Absolute                   int
	TvdbSeason                 int
	StatedTvdb, StatedAbsolute bool
}

func (a EpisodeAlias) Numbers() []int {
	var out []int
	for _, n := range []int{a.Tvdb, a.Absolute} {
		if n > 0 && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func (s *Store) EpisodeAlias(ctx context.Context, animeID, episode int) (EpisodeAlias, error) {
	var tvdb, absolute, season *int
	err := s.r.QueryRowContext(ctx,
		`SELECT tvdb_number, absolute, season_number FROM episode WHERE anime_id = ? AND ep_key = ?`,
		animeID, strconv.Itoa(episode)).Scan(&tvdb, &absolute, &season)
	if err != nil {
		return EpisodeAlias{}, nil
	}

	if season == nil {
		tvdb = nil
	}
	out := EpisodeAlias{StatedTvdb: tvdb != nil, StatedAbsolute: absolute != nil}
	if tvdb != nil && *tvdb != episode {
		out.Tvdb, out.TvdbSeason = *tvdb, *season
	}
	if absolute != nil && *absolute != episode {
		out.Absolute = *absolute
	}
	return out, nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func (s *Store) MalID(ctx context.Context, animeID int) (int, error) {
	var mal *int
	err := s.r.QueryRowContext(ctx, `
		SELECT coalesce(a.mal_id, c.mal_id)
		FROM corpus_anime c
		LEFT JOIN anime a ON a.id = c.anime_id
		WHERE c.anime_id = ?
		UNION ALL SELECT mal_id FROM anime WHERE id = ?
		LIMIT 1`, animeID, animeID).Scan(&mal)
	if err != nil || mal == nil {
		return 0, nil
	}
	return *mal, nil
}

const episodeCoverageQuery = `
SELECT (SELECT count(*) FROM episode WHERE anime_id = ?),
       (SELECT count(*) FROM episode WHERE anime_id = ?
          AND (still_url IS NULL OR still_url = '' OR title_en IS NULL OR title_en = '')),
       coalesce((SELECT status FROM anime WHERE id = ?), ''),
       coalesce((SELECT episode_count FROM anime WHERE id = ?), 0)`

func (s *Store) EpisodesStale(ctx context.Context, animeID int, maxAge time.Duration) bool {
	var stored, incomplete, expected int
	var status string
	if err := s.r.QueryRowContext(ctx, episodeCoverageQuery,
		animeID, animeID, animeID, animeID).Scan(&stored, &incomplete, &status, &expected); err != nil {
		return true
	}
	if stored > 0 {
		airing := status == "RELEASING" || status == "NOT_YET_RELEASED"
		if incomplete == 0 && stored >= expected && !airing {
			return false
		}
	}
	if maxAge <= 0 {
		return true
	}

	at, err := s.SourceRefreshedAt(ctx, episodesKey(animeID))
	return err != nil || at.IsZero() || time.Since(at) >= maxAge
}

func (s *Store) StillGaps(ctx context.Context, animeID int) (map[int]bool, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT number FROM episode
		WHERE anime_id = ? AND number IS NOT NULL AND (still_url IS NULL OR still_url = '')`, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	missing := map[int]bool{}
	for rows.Next() {
		var number int
		if err := rows.Scan(&number); err != nil {
			return nil, err
		}
		missing[number] = true
	}
	return missing, rows.Err()
}

func (s *Store) TitlesDue(ctx context.Context, animeID int) bool {
	now := time.Now()
	var missing, recent int
	if err := s.r.QueryRowContext(ctx, `
		SELECT count(*), coalesce(sum(air_date >= ?), 0) FROM episode
		WHERE anime_id = ? AND is_special = 0 AND number IS NOT NULL
		  AND (title_en IS NULL OR title_en = '')
		  AND air_date IS NOT NULL AND air_date <= ?`,
		now.Add(-titlesRecent).Unix(), animeID, now.Unix()).Scan(&missing, &recent); err != nil || missing == 0 {
		return false
	}
	at, err := s.SourceRefreshedAt(ctx, titlesKey(animeID))
	if err != nil {
		return false
	}
	if recent > 0 {
		return now.Sub(at) >= titlesRetry
	}
	return now.Sub(at) >= titlesRetryOld
}

func (s *Store) MarkTitlesTried(ctx context.Context, animeID, count int) error {
	return s.MarkSource(ctx, titlesKey(animeID), count)
}

const (
	titlesRecent    = 30 * 24 * time.Hour
	titlesRetry     = 12 * time.Hour
	titlesRetryOld  = 30 * 24 * time.Hour
	titlesDateSlack = 10 * 24 * time.Hour
)

func titlesKey(animeID int) string {
	return "titles:" + strconv.Itoa(animeID)
}

func (s *Store) FillEnglishTitles(ctx context.Context, animeID int, eps []metadata.Episode) (int, error) {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE episode SET title_en = ?
		WHERE anime_id = ? AND ep_key = ? AND is_special = 0 AND (title_en IS NULL OR title_en = '')
		  AND air_date IS NOT NULL AND abs(air_date - ?) <= ?`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	slack := int64(titlesDateSlack / time.Second)
	var filled int
	for _, e := range eps {
		title := strings.TrimSpace(e.TitleEN)
		if e.Number <= 0 || title == "" || e.AirDate == 0 {
			continue
		}
		res, err := stmt.ExecContext(ctx, title, animeID, strconv.Itoa(e.Number), e.AirDate, slack)
		if err != nil {
			return 0, err
		}
		if n, err := res.RowsAffected(); err == nil {
			filled += int(n)
		}
	}
	return filled, tx.Commit()
}

func (s *Store) SetEpisodeStills(ctx context.Context, animeID int, stills map[int]string) (int, error) {
	if len(stills) == 0 {
		return 0, nil
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		UPDATE episode SET still_url = ?
		WHERE anime_id = ? AND number = ? AND (still_url IS NULL OR still_url = '')`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var n int
	for number, url := range stills {
		res, err := stmt.ExecContext(ctx, url, animeID, number)
		if err != nil {
			return 0, err
		}
		if rows, _ := res.RowsAffected(); rows > 0 {
			n++
		}
	}
	return n, tx.Commit()
}

const bareEpisode = `title_en IS NULL AND title_ja IS NULL AND title_romaji IS NULL AND overview IS NULL
	AND still_url IS NULL AND air_date IS NULL AND absolute IS NULL AND tvdb_id IS NULL AND anidb_eid IS NULL`

func (s *Store) EpisodesFromReleasesOnly(ctx context.Context, animeID int) bool {
	var described int
	err := s.r.QueryRowContext(ctx,
		`SELECT count(*) FROM episode WHERE anime_id = ? AND NOT (`+bareEpisode+`)`, animeID).Scan(&described)
	return err == nil && described == 0
}

func (s *Store) SaveDerivedEpisodes(ctx context.Context, animeID int, numbers []int) (int, error) {
	if err := s.MarkSource(ctx, derivedKey(animeID), len(numbers)); err != nil {
		return 0, err
	}
	if limit, firm := s.EpisodeLimit(ctx, animeID); limit > 0 {
		if _, err := s.w.ExecContext(ctx, `
			DELETE FROM episode
			WHERE anime_id = ? AND number > ? AND is_special = 0 AND `+bareEpisode+`
			  AND NOT EXISTS (SELECT 1 FROM torrent_file tf
			                  WHERE tf.anime_id = episode.anime_id AND tf.ep_key = episode.ep_key AND tf.selected = 1)
			  AND NOT EXISTS (SELECT 1 FROM local_file lf
			                  WHERE lf.anime_id = episode.anime_id AND lf.episode = episode.number AND lf.missing = 0)`,
			animeID, limit+Slack(firm)); err != nil {
			return 0, err
		}
	}
	if len(numbers) == 0 {
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
		INSERT INTO episode (anime_id, ep_key, number, is_special)
		VALUES (?,?,?,0)
		ON CONFLICT(anime_id, ep_key) DO NOTHING`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var saved int
	for _, n := range numbers {
		if n <= 0 || n > 5000 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, animeID, strconv.Itoa(n), n); err != nil {
			return 0, err
		}
		saved++
	}
	return saved, tx.Commit()
}

func (s *Store) FillEpisodes(ctx context.Context, animeID int, eps []metadata.Episode) (int, error) {
	if len(eps) == 0 {
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
		INSERT INTO episode (anime_id, ep_key, number, is_special, title_en, title_ja, overview, air_date, runtime)
		VALUES (?,?,?,0,?,?,?,?,?)
		ON CONFLICT(anime_id, ep_key) DO UPDATE SET
		    title_en=coalesce(episode.title_en, excluded.title_en),
		    title_ja=coalesce(episode.title_ja, excluded.title_ja),
		    overview=coalesce(episode.overview, excluded.overview),
		    air_date=coalesce(episode.air_date, excluded.air_date),
		    runtime=coalesce(episode.runtime, excluded.runtime)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var saved int
	for _, e := range eps {
		if e.Number <= 0 || e.Number > 5000 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, animeID, strconv.Itoa(e.Number), e.Number,
			nullable(e.TitleEN), nullable(e.TitleJA), nullable(e.Overview),
			nullableInt64(e.AirDate), nullableInt(e.Runtime)); err != nil {
			return 0, err
		}
		saved++
	}
	return saved, tx.Commit()
}

func (s *Store) EpisodesBare(ctx context.Context, animeID int) bool {
	var described int
	err := s.r.QueryRowContext(ctx, `
		SELECT count(*) FROM episode
		WHERE anime_id = ? AND (title_en IS NOT NULL OR title_ja IS NOT NULL OR air_date IS NOT NULL)`,
		animeID).Scan(&described)
	return err == nil && described == 0
}

func (s *Store) ListingTried(ctx context.Context, animeID int) bool {
	at, err := s.SourceRefreshedAt(ctx, listingKey(animeID))
	return err == nil && !at.IsZero() && time.Since(at) < 30*24*time.Hour
}

func (s *Store) MarkListingTried(ctx context.Context, animeID, count int) error {
	return s.MarkSource(ctx, listingKey(animeID), count)
}

func listingKey(animeID int) string {
	return "listing:" + strconv.Itoa(animeID)
}

func (s *Store) EpisodesDerived(ctx context.Context, animeID int) bool {
	at, err := s.SourceRefreshedAt(ctx, derivedKey(animeID))
	return err == nil && !at.IsZero() && time.Since(at) < 24*time.Hour
}

func derivedKey(animeID int) string {
	return "derived:" + strconv.Itoa(animeID)
}

func (s *Store) MarkEpisodesFetched(ctx context.Context, animeID, count int) error {
	return s.MarkSource(ctx, episodesKey(animeID), count)
}

func episodesKey(animeID int) string {
	return "episodes:" + strconv.Itoa(animeID)
}

const flagsRetry = 7 * 24 * time.Hour

func (s *Store) FlagsFetched(ctx context.Context, malID int) bool {
	if malID == 0 {
		return true
	}
	var at int64
	var final int
	if err := s.r.QueryRowContext(ctx,
		`SELECT refreshed_at, records FROM corpus_source WHERE name = ?`,
		flagsKey(malID)).Scan(&at, &final); err != nil {
		return false
	}
	return final > 0 || time.Since(time.Unix(at, 0)) < flagsRetry
}

func (s *Store) MarkFlagsFetched(ctx context.Context, malID int, final bool) error {
	records := 0
	if final {
		records = 1
	}
	return s.MarkSource(ctx, flagsKey(malID), records)
}

func (s *Store) Finished(ctx context.Context, animeID int) bool {
	var status string
	s.r.QueryRowContext(ctx, `SELECT coalesce(status, '') FROM anime WHERE id = ?`, animeID).Scan(&status)
	return status == "FINISHED"
}

func flagsKey(malID int) string {
	return "flags:" + strconv.Itoa(malID)
}
