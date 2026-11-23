package store

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"
)

type Relation struct {
	AnimeID   int
	RelatedID int
	Kind      string
}

type Season struct {
	ID         int     `json:"id"`
	Ordinal    int     `json:"ordinal"`
	Romaji     string  `json:"romaji"`
	English    *string `json:"english"`
	Cover      *string `json:"cover"`
	Episodes   *int    `json:"episodes"`
	Year       *int    `json:"year"`
	Format     *string `json:"format"`
	Status     *string `json:"status"`
	ListStatus *string `json:"listStatus,omitempty"`
	Progress   int     `json:"progress"`
	OnList     bool    `json:"onList"`
}

type Franchise struct {
	RootID  int      `json:"rootId"`
	Seasons []Season `json:"seasons"`
}

type RelatedEntry struct {
	Season
	Kind string `json:"kind"`
}

const relatedQuery = `
WITH members AS (
    SELECT anime_id FROM franchise
    WHERE root_id = (SELECT root_id FROM franchise WHERE anime_id = ?)
    UNION SELECT ?
),
-- Both ways round, as edges are stored from the walked entry, or a film alone lists nothing.
linked AS (
    SELECT r.related_id AS id, r.kind AS kind
    FROM relation r JOIN members m ON m.anime_id = r.anime_id
    UNION ALL
    SELECT r.anime_id AS id,
           CASE r.kind WHEN 'SIDE_STORY' THEN 'PARENT'
                       WHEN 'SPIN_OFF'   THEN 'PARENT'
                       WHEN 'PARENT'     THEN 'SIDE_STORY'
                       WHEN 'SUMMARY'    THEN 'PARENT'
                       WHEN 'COMPILATION' THEN 'CONTAINS'
                       WHEN 'CONTAINS'   THEN 'COMPILATION'
                       WHEN 'ADAPTATION' THEN 'SOURCE'
                       WHEN 'SOURCE'     THEN 'ADAPTATION'
                       ELSE r.kind END AS kind
    FROM relation r JOIN members m ON m.anime_id = r.related_id
),
edges AS (
    SELECT id, min(kind) AS kind
    FROM linked
    WHERE kind NOT IN ('PREQUEL','SEQUEL')
      AND id NOT IN (SELECT anime_id FROM members)
      AND id NOT IN (SELECT anime_id FROM dead_anime)
      AND id <> ?
    GROUP BY id
)
SELECT e.id, e.kind,
       coalesce(a.title_romaji, ''), a.title_english, a.cover_url,
       coalesce(a.episode_count, c.episodes),
       -- OVAs and films often carry a start date but no season.
       coalesce(a.season_year, c.year, cast(substr(a.start_date, 1, 4) AS INTEGER)),
       a.format, a.status, l.status, coalesce(l.progress, 0), l.id IS NOT NULL
FROM edges e
LEFT JOIN anime a        ON a.id = e.id
LEFT JOIN corpus_anime c ON c.anime_id = e.id
LEFT JOIN list_entry l   ON l.anime_id = e.id
ORDER BY coalesce(a.season_year, c.year, cast(substr(a.start_date, 1, 4) AS INTEGER), 9999), a.title_romaji
LIMIT 200`

var SeasonKinds = map[string]bool{"PREQUEL": true, "SEQUEL": true}

func (s *Store) MarkRelationsFetched(ctx context.Context, ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO relation_fetch (anime_id, fetched_at) VALUES (?, unixepoch())
		ON CONFLICT(anime_id) DO UPDATE SET fetched_at = excluded.fetched_at`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RelationsStale(ctx context.Context, animeID int, maxAge time.Duration) bool {
	var age int64
	err := s.r.QueryRowContext(ctx,
		`SELECT unixepoch() - fetched_at FROM relation_fetch WHERE anime_id = ?`, animeID).Scan(&age)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return true
	case err != nil:
		return false
	}
	return age >= int64(maxAge.Seconds())
}

func (s *Store) Related(ctx context.Context, animeID int) ([]RelatedEntry, error) {
	rows, err := s.r.QueryContext(ctx, relatedQuery, animeID, animeID, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []RelatedEntry{}
	for rows.Next() {
		var e RelatedEntry
		if err := rows.Scan(&e.ID, &e.Kind, &e.Romaji, &e.English, &e.Cover,
			&e.Episodes, &e.Year, &e.Format, &e.Status, &e.ListStatus,
			&e.Progress, &e.OnList); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) DeadAnime(ctx context.Context, ids []int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, id := range ids {
		var n int
		if err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM dead_anime WHERE anime_id = ?`, id).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			out[id] = true
		}
	}
	return out, nil
}

func (s *Store) IsSaved(ctx context.Context, id int) bool {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM anime WHERE id = ? AND synced_at > 0`, id).Scan(&n)
	return err == nil && n > 0
}

func (s *Store) HasRelations(ctx context.Context, animeID int) (bool, error) {
	var count int
	err := s.r.QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM relation WHERE anime_id = ? OR related_id = ?)
		     + (SELECT count(*) FROM franchise WHERE anime_id = ?)`,
		animeID, animeID, animeID).Scan(&count)
	return count > 0, err
}

func (s *Store) MarkNoRelations(ctx context.Context, animeID int) error {
	if animeID == 0 {
		return nil
	}
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO relation (anime_id, related_id, kind) VALUES (?, ?, 'NONE')
		ON CONFLICT(anime_id, related_id, kind) DO NOTHING`, animeID, animeID)
	return err
}

func (s *Store) SaveRelations(ctx context.Context, fetched []int, rels []Relation) error {
	if len(rels) == 0 && len(fetched) == 0 {
		return nil
	}
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range fetched {
		if _, err := tx.ExecContext(ctx, `DELETE FROM relation WHERE (anime_id = ? OR related_id = ?) AND kind <> 'NONE'`, id, id); err != nil {
			return err
		}
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO relation (anime_id, related_id, kind) VALUES (?,?,?)
		 ON CONFLICT DO NOTHING`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, r := range rels {
		if r.AnimeID == 0 || r.RelatedID == 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, r.AnimeID, r.RelatedID, r.Kind); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RebuildFranchisesFor(ctx context.Context, ids []int, known map[int]string) (int, error) {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT anime_id, related_id FROM relation
		WHERE kind IN ('PREQUEL','SEQUEL')
		  AND anime_id NOT IN (SELECT anime_id FROM dead_anime)
		  AND related_id NOT IN (SELECT anime_id FROM dead_anime)`)
	if err != nil {
		return 0, err
	}

	adj := map[int][]int{}
	for rows.Next() {
		var a, b int
		if err := rows.Scan(&a, &b); err != nil {
			rows.Close()
			return 0, err
		}
		adj[a] = append(adj[a], b)
		adj[b] = append(adj[b], a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	seen := map[int]bool{}
	var groups [][]int
	starts := slices.Collect(maps.Keys(adj))
	if ids != nil {
		starts = slices.Clone(ids)
		for _, id := range ids {
			members, err := tx.QueryContext(ctx, `
				SELECT anime_id FROM franchise WHERE root_id IN (SELECT root_id FROM franchise WHERE anime_id = ?)`, id)
			if err != nil {
				return 0, err
			}
			for members.Next() {
				var m int
				if err := members.Scan(&m); err != nil {
					members.Close()
					return 0, err
				}
				starts = append(starts, m)
			}
			members.Close()
			if err := members.Err(); err != nil {
				return 0, err
			}
		}
	}
	slices.Sort(starts)
	for _, id := range starts {
		if seen[id] || adj[id] == nil {
			continue
		}
		groups = append(groups, component(id, adj, seen))
	}

	dates, err := startDates(ctx, tx, groups, known)
	if err != nil {
		return 0, err
	}

	if ids == nil {
		if _, err := tx.ExecContext(ctx, `DELETE FROM franchise`); err != nil {
			return 0, err
		}
	} else {
		stale := slices.Clone(ids)
		for _, members := range groups {
			stale = append(stale, members...)
		}
		for _, m := range stale {
			if _, err := tx.ExecContext(ctx, `DELETE FROM franchise WHERE anime_id = ? OR root_id = ?`, m, m); err != nil {
				return 0, err
			}
		}
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO franchise (anime_id, root_id, ordinal) VALUES (?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, members := range groups {
		sort.Slice(members, func(i, j int) bool {
			di, dj := dateOf(dates, members[i]), dateOf(dates, members[j])
			if di != dj {
				return di < dj
			}
			return members[i] < members[j]
		})

		root := members[0]
		for ordinal, member := range members {
			if _, err := stmt.ExecContext(ctx, member, root, ordinal+1); err != nil {
				return 0, err
			}
		}
	}

	return len(groups), tx.Commit()
}

func component(start int, adj map[int][]int, seen map[int]bool) []int {
	var out []int
	stack := []int{start}
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		stack = append(stack, adj[id]...)
	}
	return out
}

func dateOf(dates map[int]int, id int) int {
	if d, ok := dates[id]; ok {
		return d
	}
	return 99999999
}

func startDates(ctx context.Context, tx *sql.Tx, groups [][]int, known map[int]string) (map[int]int, error) {
	out := map[int]int{}
	for _, members := range groups {
		for _, id := range members {
			var date string
			err := tx.QueryRowContext(ctx, `
				SELECT coalesce(nullif(replace(coalesce(a.start_date,''), '-', ''), ''),
				                CASE WHEN c.year IS NOT NULL THEN printf('%04d0000', c.year) END, '')
				FROM (SELECT ? AS id) x
				LEFT JOIN anime a ON a.id = x.id
				LEFT JOIN corpus_anime c ON c.anime_id = x.id`, id).Scan(&date)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if k := strings.ReplaceAll(known[id], "-", ""); date == "" || (len(date) == 8 && len(k) == 8 && precision(k) > precision(date)) {
				date = k
			}
			n := 99999999
			if v, err := atoiSafe(date); err == nil && v > 0 {
				n = latestIn(v)
			}
			out[id] = n
		}
	}
	return out, nil
}

func precision(yyyymmdd string) int {
	switch {
	case !strings.HasSuffix(yyyymmdd, "00"):
		return 3
	case !strings.HasSuffix(yyyymmdd, "0000"):
		return 2
	}
	return 1
}

func latestIn(yyyymmdd int) int {
	switch {
	case yyyymmdd%10000 == 0:
		return yyyymmdd + 9999
	case yyyymmdd%100 == 0:
		return yyyymmdd + 99
	}
	return yyyymmdd
}

const franchiseQuery = `
SELECT f.anime_id, f.ordinal,
       coalesce(a.title_romaji, ''), a.title_english, a.cover_url,
       coalesce(a.episode_count, c.episodes),
       coalesce(a.season_year, c.year, cast(substr(a.start_date, 1, 4) AS INTEGER)),
       a.format, a.status, e.status,
       coalesce(e.progress, 0), e.id IS NOT NULL
FROM franchise f
LEFT JOIN anime a       ON a.id = f.anime_id
LEFT JOIN corpus_anime c ON c.anime_id = f.anime_id
LEFT JOIN list_entry e   ON e.anime_id = f.anime_id
WHERE f.root_id = (SELECT root_id FROM franchise WHERE anime_id = ?)
  AND f.anime_id NOT IN (SELECT anime_id FROM dead_anime)
ORDER BY f.ordinal`

func (s *Store) Franchise(ctx context.Context, animeID int) (Franchise, error) {
	rows, err := s.r.QueryContext(ctx, franchiseQuery, animeID)
	if err != nil {
		return Franchise{}, err
	}
	defer rows.Close()

	out := Franchise{Seasons: []Season{}}
	for rows.Next() {
		var s Season
		if err := rows.Scan(&s.ID, &s.Ordinal, &s.Romaji, &s.English, &s.Cover,
			&s.Episodes, &s.Year, &s.Format, &s.Status, &s.ListStatus,
			&s.Progress, &s.OnList); err != nil {
			return Franchise{}, err
		}
		out.Seasons = append(out.Seasons, s)
	}
	if err := rows.Err(); err != nil {
		return Franchise{}, err
	}

	if len(out.Seasons) == 0 {
		return s.soloFranchise(ctx, animeID)
	}
	out.RootID = out.Seasons[0].ID
	return out, nil
}

func (s *Store) NextSeason(ctx context.Context, animeID int) int {
	var id int
	s.r.QueryRowContext(ctx, `
		SELECT n.anime_id
		FROM franchise f
		JOIN franchise n ON n.root_id = f.root_id
		 AND n.ordinal = (SELECT min(ordinal) FROM franchise WHERE root_id = f.root_id AND ordinal > f.ordinal)
		JOIN anime a ON a.id = n.anime_id AND a.status IN ('RELEASING', 'FINISHED')
		WHERE f.anime_id = ?
		LIMIT 1`, animeID).Scan(&id)
	return id
}

func (s *Store) soloFranchise(ctx context.Context, animeID int) (Franchise, error) {
	var season Season
	err := s.r.QueryRowContext(ctx, `
		SELECT a.id, coalesce(a.title_romaji, ''), a.title_english, a.cover_url,
		       a.episode_count, a.season_year, a.format, a.status, e.status,
		       coalesce(e.progress, 0), e.id IS NOT NULL
		FROM anime a
		LEFT JOIN list_entry e ON e.anime_id = a.id
		WHERE a.id = ?`, animeID).Scan(
		&season.ID, &season.Romaji, &season.English, &season.Cover,
		&season.Episodes, &season.Year, &season.Format, &season.Status,
		&season.ListStatus, &season.Progress, &season.OnList)
	if err != nil {
		return Franchise{RootID: animeID, Seasons: []Season{}}, nil
	}
	season.Ordinal = 1
	return Franchise{RootID: animeID, Seasons: []Season{season}}, nil
}

func (s *Store) EnglishTitle(ctx context.Context, animeID int) (string, error) {
	var title *string
	err := s.r.QueryRowContext(ctx,
		`SELECT title_english FROM anime WHERE id = ?`, animeID).Scan(&title)
	if err != nil || title == nil {
		return "", nil
	}
	return *title, nil
}

func (s *Store) SearchTitles(ctx context.Context, animeID int) ([]string, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT text FROM (
		    SELECT title_romaji AS text, 0 AS rank FROM anime WHERE id = ?1 AND title_romaji <> ''
		    UNION
		    SELECT title_english, 1 FROM anime WHERE id = ?1 AND title_english IS NOT NULL
		    UNION
		    SELECT t.text, CASE t.kind WHEN 'primary' THEN 0 WHEN 'official' THEN 1 ELSE 2 END
		    FROM title t WHERE t.anime_id = ?1
		)
		WHERE text IS NOT NULL AND text <> ''
		ORDER BY rank, length(text)
		LIMIT 12`, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) EpisodeRuntime(ctx context.Context, animeID int) int {
	var minutes *int
	err := s.r.QueryRowContext(ctx,
		`SELECT duration FROM anime WHERE id = ?`, animeID).Scan(&minutes)
	if err != nil || minutes == nil {
		return 0
	}
	return *minutes
}

func (s *Store) EpisodeCount(ctx context.Context, animeID int) (int, error) {
	var count *int
	err := s.r.QueryRowContext(ctx, `
		SELECT coalesce(a.episode_count, c.episodes)
		FROM corpus_anime c
		LEFT JOIN anime a ON a.id = c.anime_id
		WHERE c.anime_id = ?
		UNION ALL
		SELECT episode_count FROM anime WHERE id = ?
		LIMIT 1`, animeID, animeID).Scan(&count)
	if err != nil || count == nil {
		return 0, nil
	}
	return *count, nil
}

func atoiSafe(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errNotNumber
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}

var errNotNumber = errorString("not a number")

type errorString string

func (e errorString) Error() string { return string(e) }
