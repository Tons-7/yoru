package store

import (
	"context"
	"encoding/json"
)

// Romaji is AniList's Latin transliteration (Sousou no Frieren); native titles are never chosen.
const (
	TitleRomaji  = "romaji"
	TitleEnglish = "english"
)

// PickTitle resolves which title to show. Many anime have no English title, so
// english falls back to romaji rather than leaving a card blank.
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

// ShownTitle is the show's name as the app shows it, or "" for one never imported.
func (s *Store) ShownTitle(ctx context.Context, animeID int) string {
	var romaji string
	var english *string
	if s.r.QueryRowContext(ctx, `SELECT title_romaji, title_english FROM anime WHERE id = ?`, animeID).
		Scan(&romaji, &english) != nil {
		return ""
	}
	return PickTitle(s.TitleMode(ctx, animeID), romaji, english)
}

// AniListForMal is a MyAnimeList id's AniList one, from a show seen or the offline catalogue; 0 if neither.
func (s *Store) AniListForMal(ctx context.Context, malID int) int {
	var id int
	s.r.QueryRowContext(ctx, `SELECT id FROM anime WHERE mal_id = ?1
		UNION ALL SELECT anime_id FROM corpus_anime WHERE mal_id = ?1 LIMIT 1`, malID).Scan(&id)
	return id
}

// MalShow is a MyAnimeList show as Yoru knows it: its AniList id (0 if unknown) and list status, if listed.
type MalShow struct {
	ID     int
	Status string
	Year   int
	// Title as the viewer reads titles, for a show Yoru has; "" for one only in the catalogue.
	Title string
}

// ShowsForMal maps MyAnimeList ids at once, from shows seen and the catalogue, as AniListForMal does one.
func (s *Store) ShowsForMal(ctx context.Context, malIDs []int) (map[int]MalShow, error) {
	out := make(map[int]MalShow, len(malIDs))
	if len(malIDs) == 0 {
		return out, nil
	}
	ids, err := json.Marshal(malIDs)
	if err != nil {
		return nil, err
	}
	// Before the query: a read while its cursor holds a connection can deadlock the read pool.
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
		// A show seen twice (two catalogue rows): the listed one wins.
		if was, ok := out[mal]; !ok || was.Status == "" && show.Status != "" {
			out[mal] = show
		}
	}
	return out, rows.Err()
}

// BannerOf is the show's wide picture, else its cover, for an alert; "" when there's neither.
func (s *Store) BannerOf(ctx context.Context, animeID int) string {
	var url string
	s.r.QueryRowContext(ctx, `SELECT coalesce(banner_url, cover_url, '') FROM anime WHERE id = ?`, animeID).Scan(&url)
	return url
}

// TitleMode reads the display preference, per anime where one is set.
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
