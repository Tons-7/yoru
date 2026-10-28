package store

import "context"

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
