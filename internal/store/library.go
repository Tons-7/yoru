package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Anime struct {
	ID           int
	MalID        *int
	Romaji       string
	English      *string
	Native       *string
	Synonyms     string
	Format       *string
	Status       *string
	Episodes     *int
	Duration     *int
	Season       *string
	SeasonYear   *int
	Country      *string
	IsAdult      bool
	Description  *string
	Cover        *string
	CoverColor   *string
	Banner       *string
	AverageScore *int
	MeanScore    *int
	Popularity   *int
	Favourites   *int
	Genres       string
	Studios      string
	Tags         string
	Links        string
	Source       *string
	StartDate    string
	EndDate      string
	CoverMedium  *string
	TrailerID    *string
	TrailerSite  *string
	NextEpisode  *int
	NextAiringAt *int

	Media *string
}

type Entry struct {
	ID          int
	AnimeID     int
	Status      *string
	Progress    int
	Score       int
	Repeat      int
	Notes       *string
	Private     bool
	Hidden      bool
	StartedAt   string
	CompletedAt string
	UpdatedAt   int
}

type ImportResult struct {
	Anime   int `json:"anime"`
	Entries int `json:"entries"`
	Removed int `json:"removed"`
}

type LibraryItem struct {
	ID int `json:"id"`

	Title   string  `json:"title"`
	Romaji  string  `json:"romaji"`
	English *string `json:"english"`

	Cover        *string `json:"cover"`
	Color        *string `json:"color"`
	Episodes     *int    `json:"episodes"`
	Status       *string `json:"status"`
	Progress     int     `json:"progress"`
	Score        int     `json:"score"`
	NextEpisode  *int    `json:"nextEpisode"`
	NextAiringAt *int    `json:"nextAiringAt"`
	Format       *string `json:"format,omitempty"`
	SeasonYear   *int    `json:"seasonYear,omitempty"`
	Airing       *string `json:"airing,omitempty"`
	Favourite    bool    `json:"favourite"`

	Resume *Resume `json:"resume,omitempty"`
}

type Resume struct {
	EpKey      string   `json:"epKey"`
	Episode    *int     `json:"episode"`
	Position   float64  `json:"position"`
	Duration   *float64 `json:"duration"`
	Percent    float64  `json:"percent"`
	Watched    bool     `json:"watched"`
	LastPlayed int      `json:"lastPlayed"`
}

func (r *Resume) compute() {
	if r.Duration != nil && *r.Duration > 0 {
		r.Percent = r.Position / *r.Duration * 100
	}
}

var animeContent = []string{
	"mal_id", "title_romaji", "title_english", "title_native", "synonyms", "status", "episode_count",
	"duration", "format", "season", "season_year", "country", "is_adult", "description", "cover_url",
	"cover_color", "cover_medium", "banner_url", "average_score", "mean_score", "popularity", "favourites",
	"genres", "studios", "tags", "links", "source", "start_date", "end_date", "trailer_id", "trailer_site",
	"next_episode", "next_airing_at", "media",
}

const resyncAfter = time.Hour

func animeChanged() string {
	mine, theirs := make([]string, len(animeContent)), make([]string, len(animeContent))
	for i, c := range animeContent {
		mine[i], theirs[i] = "anime."+c, "excluded."+c
	}
	return fmt.Sprintf("(%s) IS NOT (%s) OR anime.synced_at < excluded.synced_at - %d",
		strings.Join(mine, ", "), strings.Join(theirs, ", "), int64(resyncAfter.Seconds()))
}

var upsertAnime = `
INSERT INTO anime (
    id, mal_id, title_romaji, title_english, title_native, synonyms,
    format, status, episode_count, duration, season, season_year,
    country, is_adult, description, cover_url, cover_color, cover_medium, banner_url,
    average_score, mean_score, popularity, favourites,
    genres, studios, tags, links, source, start_date, end_date,
    trailer_id, trailer_site, next_episode, next_airing_at, media, synced_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
    mal_id=excluded.mal_id, title_romaji=excluded.title_romaji,
    title_english=excluded.title_english, title_native=excluded.title_native,
    synonyms=excluded.synonyms, status=excluded.status,
    episode_count=excluded.episode_count, duration=excluded.duration,
    format=excluded.format, season=excluded.season, season_year=excluded.season_year,
    country=excluded.country, is_adult=excluded.is_adult,
    description=excluded.description, cover_url=excluded.cover_url,
    cover_color=excluded.cover_color, cover_medium=excluded.cover_medium,
    banner_url=excluded.banner_url, average_score=excluded.average_score,
    mean_score=excluded.mean_score, popularity=excluded.popularity,
    favourites=excluded.favourites, genres=excluded.genres,
    studios=excluded.studios, tags=excluded.tags, links=excluded.links,
    source=excluded.source, start_date=excluded.start_date, end_date=excluded.end_date,
    trailer_id=excluded.trailer_id, trailer_site=excluded.trailer_site,
    next_episode=excluded.next_episode, next_airing_at=excluded.next_airing_at,
    media=excluded.media, synced_at=excluded.synced_at
WHERE ` + animeChanged()

const entryUpdateSet = `
    status=excluded.status, progress=excluded.progress, score=excluded.score,
    repeat_count=excluded.repeat_count, notes=excluded.notes,
    private=excluded.private, hidden=excluded.hidden,
    started_at=excluded.started_at, completed_at=excluded.completed_at,
    remote_updated_at=excluded.remote_updated_at, dirty=0`

func buildUpsertEntry(guard bool) string {
	where := ""
	if guard {
		where = " WHERE list_entry.dirty = 0 AND excluded.remote_updated_at > list_entry.remote_updated_at"
	}
	return `
INSERT INTO list_entry (
    id, anime_id, status, progress, score, repeat_count, notes,
    private, hidden, started_at, completed_at,
    remote_updated_at, local_updated_at, dirty
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,0)
ON CONFLICT(id) DO UPDATE SET` + entryUpdateSet + where + `
ON CONFLICT(anime_id) DO UPDATE SET id=excluded.id,` + entryUpdateSet + where
}

var (
	upsertEntryBase = buildUpsertEntry(false)
	upsertEntry     = buildUpsertEntry(true)
)

type ImportMode string

const (
	ImportMerge   ImportMode = "merge"
	ImportReplace ImportMode = "replace"
)

func (m ImportMode) valid() bool { return m == ImportMerge || m == ImportReplace }

func (s *Store) IsAdult(ctx context.Context, animeID int) (bool, error) {
	var adult int
	err := s.r.QueryRowContext(ctx,
		`SELECT coalesce(is_adult, 0) FROM anime WHERE id = ?`, animeID).Scan(&adult)
	if err != nil {
		return false, err
	}
	return adult == 1, nil
}

func (s *Store) Format(ctx context.Context, animeID int) *string {
	var format *string
	if err := s.r.QueryRowContext(ctx, `
		SELECT coalesce((SELECT format FROM anime WHERE id = ?1),
		                (SELECT nullif(upper(kind), 'UNKNOWN') FROM corpus_anime WHERE anime_id = ?1))`,
		animeID).Scan(&format); err != nil {
		return nil
	}
	return format
}

func (s *Store) ImportList(ctx context.Context, anime []Anime, entries []Entry, mode ImportMode) (ImportResult, error) {
	if !mode.valid() {
		mode = ImportMerge
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	for _, a := range anime {
		_, err := tx.ExecContext(ctx, upsertAnime,
			a.ID, a.MalID, a.Romaji, a.English, a.Native, orJSON(a.Synonyms),
			a.Format, a.Status, a.Episodes, a.Duration, a.Season, a.SeasonYear,
			a.Country, boolInt(a.IsAdult), a.Description,
			a.Cover, a.CoverColor, a.CoverMedium, a.Banner,
			a.AverageScore, a.MeanScore, a.Popularity, a.Favourites,
			orJSON(a.Genres), orJSON(a.Studios), orJSON(a.Tags), orJSON(a.Links),
			a.Source, nullable(a.StartDate), nullable(a.EndDate),
			a.TrailerID, a.TrailerSite, a.NextEpisode, a.NextAiringAt, a.Media, now)
		if err != nil {
			return ImportResult{}, err
		}
	}

	statement := upsertEntry
	if mode == ImportReplace {
		statement = upsertEntryBase
	}
	for _, e := range entries {
		_, err := tx.ExecContext(ctx, statement,
			e.ID, e.AnimeID, e.Status, e.Progress, e.Score, e.Repeat, e.Notes,
			boolInt(e.Private), boolInt(e.Hidden),
			nullable(e.StartedAt), nullable(e.CompletedAt), e.UpdatedAt, now)
		if err != nil {
			return ImportResult{}, err
		}
	}

	var removed int
	if mode == ImportReplace {
		if removed, err = dropMissingEntries(ctx, tx, entries); err != nil {
			return ImportResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Anime: len(anime), Entries: len(entries), Removed: removed}, nil
}

func dropMissingEntries(ctx context.Context, tx *sql.Tx, keep []Entry) (int, error) {
	wanted := make(map[int]struct{}, len(keep))
	for _, e := range keep {
		wanted[e.AnimeID] = struct{}{}
	}

	rows, err := tx.QueryContext(ctx, `SELECT anime_id FROM list_entry WHERE anime_id > 0`)
	if err != nil {
		return 0, err
	}

	var stale []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		if _, ok := wanted[id]; !ok {
			stale = append(stale, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, id := range stale {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM list_entry WHERE anime_id = ?`, id); err != nil {
			return 0, err
		}
	}
	return len(stale), nil
}

const latestPlayback = `
LEFT JOIN (
    SELECT anime_id, ep_key, position_s, duration_s, watched, last_played_at,
           ROW_NUMBER() OVER (PARTITION BY anime_id ORDER BY last_played_at DESC) AS rn
    FROM playback
    WHERE last_played_at > 0
) p ON p.anime_id = a.id AND p.rn = 1
LEFT JOIN episode ep ON ep.anime_id = a.id AND ep.ep_key = p.ep_key`

const libraryColumns = `
a.id, a.title_romaji, a.title_english, a.cover_url, a.cover_color, a.episode_count,
e.status, e.progress, e.score, a.next_episode, a.next_airing_at, a.status, a.format,
coalesce((SELECT ua.favourite FROM user_anime ua WHERE ua.anime_id = a.id AND ua.hidden = 0), 0),
p.ep_key, ep.number, p.position_s, p.duration_s, p.watched, p.last_played_at`

var LibrarySorts = []string{"updated", "title", "score", "airing", "progress", "watched", "new", "added", "status"}

var librarySortSQL = map[string]string{
	"updated":  "e.local_updated_at DESC, a.title_romaji",
	"title":    "{title}, a.title_romaji",
	"score":    "e.score DESC, e.local_updated_at DESC",
	"airing":   "a.next_airing_at IS NULL, a.next_airing_at, a.title_romaji",
	"progress": "e.progress DESC, e.local_updated_at DESC",
	"watched":  "coalesce(p.last_played_at, 0) DESC, e.remote_updated_at DESC, e.local_updated_at DESC, a.title_romaji",
	"new": `(a.status = 'RELEASING' AND coalesce(a.next_episode, 1) - 1 > coalesce(e.progress, 0)) DESC,
	        coalesce(p.last_played_at, 0) DESC, e.local_updated_at DESC, a.title_romaji`,
	"added": "e.id < 0 DESC, abs(e.id) DESC",
	"status": `CASE e.status WHEN 'CURRENT' THEN 0 WHEN 'PLANNING' THEN 1 WHEN 'REPEATING' THEN 2
	           WHEN 'PAUSED' THEN 3 WHEN 'COMPLETED' THEN 4 WHEN 'DROPPED' THEN 5 ELSE 6 END,
	           {title}`,
}

const libraryWhere = `
WHERE (?1 = '' OR e.status = ?1)
  AND (?2 = '' OR a.title_romaji LIKE ?2 ESCAPE '\' OR a.title_english LIKE ?2 ESCAPE '\'
       OR a.synonyms LIKE ?2 ESCAPE '\')`

const favouritesFrom = `
FROM user_anime u
JOIN anime a ON a.id = u.anime_id
LEFT JOIN list_entry e ON e.anime_id = a.id
`

const favouritesWhere = `u.favourite = 1 AND u.hidden = 0`

func libraryFrom(favourite bool) string {
	if favourite {
		return favouritesFrom
	}
	return `
FROM list_entry e
JOIN anime a ON a.id = e.anime_id
`
}

func libraryFilterWhere(favourite bool) string {
	if favourite {
		return libraryWhere + `
  AND ` + favouritesWhere
	}
	return libraryWhere
}

func libraryQuery(sort string, favourite bool, mode string) string {
	order, ok := librarySortSQL[sort]
	if !ok {
		order = librarySortSQL[LibrarySorts[0]]
	}
	title := "lower(coalesce(a.title_english, a.title_romaji))"
	if mode == TitleRomaji {
		title = "lower(a.title_romaji)"
	}
	order = strings.ReplaceAll(order, "{title}", title)
	return `
SELECT ` + libraryColumns + libraryFrom(favourite) + latestPlayback + libraryFilterWhere(favourite) + `
ORDER BY ` + order + `
LIMIT ?3 OFFSET ?4`
}

const inProgress = `dismissed = 0 AND position_s >= 15
      AND NOT (coalesce(duration_s, 0) > 0 AND position_s >= duration_s * coalesce(
          (SELECT CAST(value AS REAL) FROM anime_pref
           WHERE anime_id = playback.anime_id AND key = 'sync.progress_at'
             AND CAST(value AS REAL) > 0 AND CAST(value AS REAL) <= 1), ?1))
      AND NOT (coalesce(done_s, 0) > 0 AND position_s >= done_s)
      AND NOT (coalesce(duration_s, 0) <= 0 AND watched = 1)`

const latestFirst = `ORDER BY last_played_at DESC, watched, ep_key DESC`

const continuePicks = `
WITH resume AS (
    SELECT anime_id, ep_key, position_s, duration_s, watched, last_played_at, rn
    FROM (SELECT playback.*, ROW_NUMBER() OVER (PARTITION BY anime_id ` + latestFirst + `) AS rn
          FROM playback WHERE dismissed = 0) playback
    WHERE rn = 1 AND ` + inProgress + `
),
upnext AS (
    SELECT e.anime_id,
           cast(e.progress + 1 AS TEXT) AS ep_key,
           NULL AS position_s, NULL AS duration_s, 0 AS watched,
           coalesce((SELECT max(last_played_at) FROM playback p WHERE p.anime_id = e.anime_id),
                    e.local_updated_at) AS last_played_at
    FROM list_entry e
    JOIN anime a ON a.id = e.anime_id
    WHERE e.status IN ('CURRENT','REPEATING')
      AND (e.progress > 0 OR e.status = 'REPEATING')
      AND (a.episode_count IS NULL OR e.progress < a.episode_count)
      AND (a.next_episode IS NULL OR a.next_airing_at IS NULL
           OR e.progress + 1 < a.next_episode OR a.next_airing_at <= unixepoch())
      AND e.anime_id NOT IN (SELECT anime_id FROM resume WHERE rn = 1)
      AND NOT EXISTS (
          SELECT 1 FROM playback d
          WHERE d.anime_id = e.anime_id
            AND d.ep_key = cast(e.progress + 1 AS TEXT)
            AND d.dismissed = 1)
),
picks AS (
    SELECT anime_id, ep_key, position_s, duration_s, watched, last_played_at
    FROM resume WHERE rn = 1
    UNION ALL
    SELECT anime_id, ep_key, position_s, duration_s, watched, last_played_at FROM upnext
)`

const continueQuery = continuePicks + `
SELECT ` + libraryColumns + `
FROM picks p
JOIN anime a ON a.id = p.anime_id
LEFT JOIN list_entry e ON e.anime_id = a.id
LEFT JOIN episode ep ON ep.anime_id = a.id AND ep.ep_key = p.ep_key
ORDER BY p.last_played_at DESC
LIMIT ?2 OFFSET ?3`

const continueCountQuery = continuePicks + `SELECT count(*) FROM picks`

type LibraryFilter struct {
	Status    string
	Query     string
	Sort      string
	Favourite bool
}

func (s *Store) Library(ctx context.Context, f LibraryFilter, p Paging) (Page[LibraryItem], error) {
	like := ""
	if q := strings.TrimSpace(f.Query); q != "" {
		like = "%" + escapeLike(q) + "%"
	}
	items, err := s.libraryRows(ctx, libraryQuery(f.Sort, f.Favourite, s.TitleMode(ctx, 0)), f.Status, like, p.PerPage+1, p.Offset())
	if err != nil {
		return Page[LibraryItem]{}, err
	}

	total, err := s.countRows(ctx,
		`SELECT count(*)`+libraryFrom(f.Favourite)+libraryFilterWhere(f.Favourite), f.Status, like)
	if err != nil {
		return Page[LibraryItem]{}, err
	}
	return NewPage(items, p, total), nil
}

type LibraryCounts struct {
	Total             int            `json:"total"`
	Statuses          map[string]int `json:"statuses"`
	Favourites        int            `json:"favourites"`
	FavouriteStatuses map[string]int `json:"favouriteStatuses"`
}

func (s *Store) LibraryCounts(ctx context.Context) (LibraryCounts, error) {
	out := LibraryCounts{Statuses: map[string]int{}, FavouriteStatuses: map[string]int{}}
	total, err := s.statusCounts(ctx, `
		SELECT e.status, count(*) FROM list_entry e
		JOIN anime a ON a.id = e.anime_id
		GROUP BY e.status`, out.Statuses)
	if err != nil {
		return out, err
	}
	out.Total = total
	out.Favourites, err = s.statusCounts(ctx,
		`SELECT e.status, count(*)`+favouritesFrom+`WHERE `+favouritesWhere+` GROUP BY e.status`, out.FavouriteStatuses)
	return out, err
}

func (s *Store) statusCounts(ctx context.Context, query string, into map[string]int) (int, error) {
	rows, err := s.r.QueryContext(ctx, query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var status *string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return 0, err
		}
		if status != nil {
			into[*status] = n
		}
		total += n
	}
	return total, rows.Err()
}

func (s *Store) ContinueWatching(ctx context.Context, p Paging) (Page[LibraryItem], error) {
	threshold := s.watchedThreshold(ctx)
	items, err := s.libraryRows(ctx, continueQuery, threshold, p.PerPage+1, p.Offset())
	if err != nil {
		return Page[LibraryItem]{}, err
	}

	total, err := s.countRows(ctx, continueCountQuery, threshold)
	if err != nil {
		return Page[LibraryItem]{}, err
	}
	return NewPage(items, p, total), nil
}

func (s *Store) countRows(ctx context.Context, query string, args ...any) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

func (s *Store) libraryRows(ctx context.Context, query string, args ...any) ([]LibraryItem, error) {
	mode := s.TitleMode(ctx, 0)

	rows, err := s.r.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []LibraryItem{}
	for rows.Next() {
		var (
			it      LibraryItem
			status  *string
			prog    *int
			score   *int
			epKey   *string
			epNum   *int
			pos     *float64
			dur     *float64
			watched *int
			played  *int
			fav     int
		)
		if err := rows.Scan(&it.ID, &it.Romaji, &it.English, &it.Cover, &it.Color, &it.Episodes,
			&status, &prog, &score, &it.NextEpisode, &it.NextAiringAt, &it.Airing, &it.Format, &fav,
			&epKey, &epNum, &pos, &dur, &watched, &played); err != nil {
			return nil, err
		}

		it.Title = PickTitle(mode, it.Romaji, it.English)
		it.Status = status
		it.Favourite = fav == 1
		if prog != nil {
			it.Progress = *prog
		}
		if score != nil {
			it.Score = *score
		}
		if epKey != nil && pos != nil {
			r := &Resume{
				EpKey: *epKey, Episode: epNum, Position: *pos, Duration: dur,
				Watched: watched != nil && *watched == 1,
			}
			if played != nil {
				r.LastPlayed = *played
			}
			r.compute()
			it.Resume = r
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func clampLimit(v, def, max int) int {
	if v <= 0 || v > max {
		return def
	}
	return v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func orJSON(s string) string {
	if s == "" {
		return "[]"
	}
	return s
}
