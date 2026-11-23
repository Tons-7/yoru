package store

import (
	"context"
	"encoding/json"
)

const (
	TitleRomaji  = "romaji"
	TitleEnglish = "english"
)

func PickTitle(mode, romaji string, english *string) string {
	if mode == TitleEnglish && english != nil && *english != "" {
		return *english
	}
	if romaji != "" {
		return romaji
	}
	if english != nil {
		return *english
	}
	return ""
}

func (s *Store) ShownTitle(ctx context.Context, animeID int) string {
	var romaji string
	var english *string
	if s.r.QueryRowContext(ctx, `SELECT title_romaji, title_english FROM anime WHERE id = ?`, animeID).
		Scan(&romaji, &english) != nil {
		return ""
	}
	return PickTitle(s.TitleMode(ctx, animeID), romaji, english)
}

func (s *Store) AniListForMal(ctx context.Context, malID int) int {
	var id int
	s.r.QueryRowContext(ctx, malPick, malID).Scan(&id)
	return id
}

type MalShow struct {
	ID     int
	Status string
	Year   int
	Title  string
}

func (s *Store) ShowsForMal(ctx context.Context, malIDs []int) (map[int]MalShow, error) {
	out := make(map[int]MalShow, len(malIDs))
	if len(malIDs) == 0 {
		return out, nil
	}
	ids, err := json.Marshal(malIDs)
	if err != nil {
		return nil, err
	}
	mode := s.TitleMode(ctx, 0)
	rows, err := s.r.QueryContext(ctx, `
		SELECT j.value, coalesce(a.id, c.anime_id, 0), coalesce(l.status, ''),
		       coalesce(a.season_year, cast(substr(a.start_date, 1, 4) AS INTEGER), c.year, 0),
		       coalesce(a.title_romaji, ''), a.title_english
		FROM json_each(?) j
		LEFT JOIN anime a         ON a.mal_id = j.value
		LEFT JOIN corpus_anime c  ON a.id IS NULL AND c.mal_id = j.value
		LEFT JOIN list_entry l    ON l.anime_id = coalesce(a.id, c.anime_id)`, string(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mal int
		var show MalShow
		var romaji string
		var english *string
		if err := rows.Scan(&mal, &show.ID, &show.Status, &show.Year, &romaji, &english); err != nil {
			return nil, err
		}
		if romaji != "" {
			show.Title = PickTitle(mode, romaji, english)
		}
		if was, ok := out[mal]; !ok || was.Status == "" && show.Status != "" {
			out[mal] = show
		}
	}
	return out, rows.Err()
}

func (s *Store) BannerOf(ctx context.Context, animeID int) string {
	var url string
	s.r.QueryRowContext(ctx, `SELECT coalesce(banner_url, cover_url, '') FROM anime WHERE id = ?`, animeID).Scan(&url)
	return url
}

func (s *Store) TitleMode(ctx context.Context, animeID int) string {
	prefs, err := s.Prefs(ctx, animeID)
	if err != nil {
		return TitleEnglish
	}
	if mode := prefs.String("display.titles"); mode == TitleRomaji {
		return TitleRomaji
	}
	return TitleEnglish
}
