package store

import (
	"context"
	"testing"
)

func coverOf(t *testing.T, s *Store, id int) string {
	t.Helper()
	var cover string
	if err := s.r.QueryRow(`SELECT coalesce(cover_url, '') FROM anime WHERE id = ?`, id).Scan(&cover); err != nil {
		t.Fatal(err)
	}
	return cover
}

func TestMALOnlyShowsTakeTheCorpusPicture(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const picture = "https://cdn.myanimelist.net/images/anime/1000/62422.jpg"

	if err := s.EnsureAnime(ctx, -62422); err != nil {
		t.Fatal(err)
	}
	if got := coverOf(t, s, -62422); got != "" {
		t.Fatalf("cover before the corpus knew one = %q", got)
	}

	entry := corpusEntry(-62422, "Silent Witch: Chinmoku no Majo no Kakushigoto - Sennyuu Kiroku")
	entry.Picture = picture
	if _, err := s.SaveCorpus(ctx, []CorpusEntry{entry}); err != nil {
		t.Fatal(err)
	}
	if got := coverOf(t, s, -62422); got != picture {
		t.Errorf("a coverless MAL-only show kept no cover after the corpus refresh: %q", got)
	}

	if err := s.EnsureAnime(ctx, -99); err != nil {
		t.Fatal(err)
	}
	fresh := corpusEntry(-77, "Another MAL-only show")
	fresh.Picture = picture
	if _, err := s.SaveCorpus(ctx, []CorpusEntry{fresh}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureAnime(ctx, -77); err != nil {
		t.Fatal(err)
	}
	if got := coverOf(t, s, -77); got != picture {
		t.Errorf("a MAL-only show made from the corpus has cover %q", got)
	}
}

func TestAniListShowsKeepTheirOwnArt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry := corpusEntry(500, "An AniList show")
	entry.Picture = "https://cdn.myanimelist.net/images/anime/1/500.jpg"
	if _, err := s.SaveCorpus(ctx, []CorpusEntry{entry}); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureAnime(ctx, 500); err != nil {
		t.Fatal(err)
	}
	if got := coverOf(t, s, 500); got != "" {
		t.Errorf("an AniList show took the corpus picture %q; its own art comes from AniList", got)
	}
}
