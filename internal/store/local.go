package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

type LocalFile struct {
	ID         int     `json:"id"`
	Path       string  `json:"path"`
	Size       int64   `json:"size"`
	Modified   int64   `json:"modified"`
	AnimeID    int     `json:"animeId,omitempty"`
	Episode    int     `json:"episode,omitempty"`
	Season     int     `json:"season,omitempty"`
	Title      string  `json:"parsedTitle,omitempty"`
	Resolution string  `json:"resolution,omitempty"`
	Group      string  `json:"releaseGroup,omitempty"`
	Confidence float64 `json:"confidence"`
	Missing    bool    `json:"missing,omitempty"`
	ShowTitle  string  `json:"showTitle,omitempty"`
}

func (s *Store) SaveLocalFiles(ctx context.Context, stamp int64, files []LocalFile) (int, error) {
	if len(files) == 0 {
		return 0, nil
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO local_file (path, size, modified, anime_id, episode, season,
		                        parsed_title, resolution, release_group,
		                        confidence, scanned_at, missing)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(path) DO UPDATE SET
		    size = excluded.size,
		    modified = excluded.modified,
		    -- A hand assignment (confidence 1) outlives every rescan's guess.
		    anime_id = CASE WHEN local_file.confidence >= 1 THEN local_file.anime_id ELSE excluded.anime_id END,
		    episode = CASE WHEN local_file.confidence >= 1 THEN local_file.episode ELSE excluded.episode END,
		    season = CASE WHEN local_file.confidence >= 1 THEN local_file.season ELSE excluded.season END,
		    confidence = CASE WHEN local_file.confidence >= 1 THEN local_file.confidence ELSE excluded.confidence END,
		    parsed_title = excluded.parsed_title,
		    resolution = excluded.resolution,
		    release_group = excluded.release_group,
		    scanned_at = excluded.scanned_at,
		    missing = 0`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, f := range files {
		_, err := stmt.ExecContext(ctx, f.Path, f.Size, f.Modified,
			zeroToNull(f.AnimeID), zeroToNull(f.Episode), zeroToNull(f.Season),
			nullable(f.Title), nullable(f.Resolution), nullable(f.Group),
			f.Confidence, stamp)
		if err != nil {
			return 0, err
		}
	}
	return len(files), tx.Commit()
}

func (s *Store) NextScanStamp(ctx context.Context) (int64, error) {
	var last sql.NullInt64
	if err := s.r.QueryRowContext(ctx,
		`SELECT max(scanned_at) FROM local_file`).Scan(&last); err != nil {
		return 0, err
	}

	stamp := time.Now().UnixNano()
	if last.Valid && stamp <= last.Int64 {
		stamp = last.Int64 + 1
	}
	return stamp, nil
}

func likePrefix(root string) string {
	root = strings.TrimRight(root, `\/`) + string(filepath.Separator)
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(root)
}

func (s *Store) SweepLocal(ctx context.Context, stamp int64, roots []string) (int, error) {
	if len(roots) == 0 {
		return 0, nil
	}

	var query strings.Builder
	query.WriteString(`UPDATE local_file SET missing = 1 WHERE scanned_at < ? AND missing = 0 AND (`)
	args := []any{stamp}
	for i, root := range roots {
		if i > 0 {
			query.WriteString(" OR ")
		}
		query.WriteString("path LIKE ? ESCAPE '\\'")
		args = append(args, likePrefix(root)+"%")
	}
	query.WriteString(")")

	res, err := s.w.ExecContext(ctx, query.String(), args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) SweepOutside(ctx context.Context, roots []string) (int, error) {
	var query strings.Builder
	query.WriteString(`UPDATE local_file SET missing = 1 WHERE missing = 0`)
	var args []any
	for _, root := range roots {
		query.WriteString(" AND path NOT LIKE ? ESCAPE '\\'")
		args = append(args, likePrefix(root)+"%")
	}
	res, err := s.w.ExecContext(ctx, query.String(), args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) MarkLocalMissing(ctx context.Context, id int) error {
	_, err := s.w.ExecContext(ctx, `UPDATE local_file SET missing = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) ForgetMissing(ctx context.Context) (int, error) {
	res, err := s.w.ExecContext(ctx, `DELETE FROM local_file WHERE missing = 1`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) LocalEpisode(ctx context.Context, animeID, episode int) (LocalFile, error) {
	var f LocalFile
	var season, ep sql.NullInt64
	var title, res, group sql.NullString

	err := s.r.QueryRowContext(ctx, `
		SELECT id, path, size, modified, episode, season,
		       parsed_title, resolution, release_group, confidence
		FROM local_file
		WHERE missing = 0 AND anime_id = ? AND episode = ?
		ORDER BY confidence >= 1 DESC, CAST(rtrim(coalesce(resolution, ''), 'pi') AS INTEGER) DESC, size DESC
		LIMIT 1`, animeID, episode).
		Scan(&f.ID, &f.Path, &f.Size, &f.Modified, &ep, &season,
			&title, &res, &group, &f.Confidence)
	if errors.Is(err, sql.ErrNoRows) {
		return LocalFile{}, nil
	}
	if err != nil {
		return LocalFile{}, err
	}

	f.AnimeID = animeID
	f.Episode = int(ep.Int64)
	f.Season = int(season.Int64)
	f.Title, f.Resolution, f.Group = title.String, res.String, group.String
	return f, nil
}

func (s *Store) LocalFile(ctx context.Context, id int) (LocalFile, error) {
	var f LocalFile
	var animeID, season, ep sql.NullInt64
	var title, res, group sql.NullString
	var missing int

	err := s.r.QueryRowContext(ctx, `
		SELECT id, path, size, modified, anime_id, episode, season,
		       parsed_title, resolution, release_group, confidence, missing
		FROM local_file WHERE id = ?`, id).
		Scan(&f.ID, &f.Path, &f.Size, &f.Modified, &animeID, &ep, &season,
			&title, &res, &group, &f.Confidence, &missing)
	if errors.Is(err, sql.ErrNoRows) {
		return LocalFile{}, nil
	}
	if err != nil {
		return LocalFile{}, err
	}

	f.AnimeID, f.Episode, f.Season = int(animeID.Int64), int(ep.Int64), int(season.Int64)
	f.Title, f.Resolution, f.Group = title.String, res.String, group.String
	f.Missing = missing == 1
	return f, nil
}

func (s *Store) LocalFiles(ctx context.Context, animeID int, unmatchedOnly bool, p Paging) (Page[LocalFile], error) {
	return s.LocalFilesView(ctx, animeID, unmatchedOnly, false, p)
}

func (s *Store) LocalFilesView(ctx context.Context, animeID int, unmatchedOnly, missingOnly bool, p Paging) (Page[LocalFile], error) {
	where := `WHERE f.missing = ` + map[bool]string{false: "0", true: "1"}[missingOnly] + ` AND (?1 = 0 OR f.anime_id = ?1)`
	if unmatchedOnly {
		where += ` AND f.anime_id IS NULL`
	}

	var total int
	if err := s.r.QueryRowContext(ctx,
		`SELECT count(*) FROM local_file f `+where, animeID).Scan(&total); err != nil {
		return Page[LocalFile]{}, err
	}

	mode := s.TitleMode(ctx, 0)
	rows, err := s.r.QueryContext(ctx, `
		SELECT f.id, f.path, f.size, f.modified, f.anime_id, f.episode, f.season,
		       f.parsed_title, f.resolution, f.release_group, f.confidence, f.missing,
		       coalesce(a.title_romaji, ''), a.title_english
		FROM local_file f LEFT JOIN anime a ON a.id = f.anime_id `+where+`
		ORDER BY coalesce(f.parsed_title, f.path), f.episode
		LIMIT ?2 OFFSET ?3`, animeID, p.PerPage+1, p.Offset())
	if err != nil {
		return Page[LocalFile]{}, err
	}
	defer rows.Close()

	items := []LocalFile{}
	for rows.Next() {
		var f LocalFile
		var aid, season, ep sql.NullInt64
		var title, res, group sql.NullString
		var missing int
		var romaji string
		var english *string

		if err := rows.Scan(&f.ID, &f.Path, &f.Size, &f.Modified, &aid, &ep, &season,
			&title, &res, &group, &f.Confidence, &missing, &romaji, &english); err != nil {
			return Page[LocalFile]{}, err
		}
		f.AnimeID, f.Episode, f.Season = int(aid.Int64), int(ep.Int64), int(season.Int64)
		f.Title, f.Resolution, f.Group = title.String, res.String, group.String
		f.Missing = missing == 1
		if romaji != "" {
			f.ShowTitle = PickTitle(mode, romaji, english)
		}
		items = append(items, f)
	}
	if err := rows.Err(); err != nil {
		return Page[LocalFile]{}, err
	}
	return NewPage(items, p, total), nil
}

func (s *Store) AssignLocalFile(ctx context.Context, id, animeID, episode int) error {
	_, err := s.w.ExecContext(ctx, `
		UPDATE local_file SET anime_id = ?, episode = ?, confidence = 1
		WHERE id = ?`, zeroToNull(animeID), zeroToNull(episode), id)
	return err
}

type LocalStats struct {
	Files     int   `json:"files"`
	Matched   int   `json:"matched"`
	Unmatched int   `json:"unmatched"`
	Missing   int   `json:"missing"`
	Bytes     int64 `json:"bytes"`
	Anime     int   `json:"anime"`
}

func (s *Store) LocalStats(ctx context.Context) (LocalStats, error) {
	var out LocalStats
	err := s.r.QueryRowContext(ctx, `
		SELECT count(*),
		       coalesce(sum(anime_id IS NOT NULL), 0),
		       coalesce(sum(anime_id IS NULL), 0),
		       coalesce(sum(size), 0),
		       count(DISTINCT anime_id)
		FROM local_file WHERE missing = 0`).
		Scan(&out.Files, &out.Matched, &out.Unmatched, &out.Bytes, &out.Anime)
	if err != nil {
		return out, err
	}
	err = s.r.QueryRowContext(ctx,
		`SELECT count(*) FROM local_file WHERE missing = 1`).Scan(&out.Missing)
	return out, err
}
