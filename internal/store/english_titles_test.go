package store

import (
	"context"
	"testing"
	"time"

	"yoru/internal/metadata"
)

func titleOf(t *testing.T, s *Store, animeID, number int) string {
	t.Helper()
	eps, err := s.Episodes(context.Background(), animeID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range eps {
		if e.Number == number && e.TitleEN != nil {
			return *e.TitleEN
		}
	}
	return ""
}

func markTitlesAgo(t *testing.T, s *Store, animeID int, ago time.Duration) {
	t.Helper()
	if err := s.MarkTitlesTried(context.Background(), animeID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`UPDATE corpus_source SET refreshed_at = ? WHERE name = ?`,
		time.Now().Add(-ago).Unix(), titlesKey(animeID)); err != nil {
		t.Fatal(err)
	}
}

func TestEnglishTitlesFillOnlyMatchingAiredGaps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 700, "RELEASING", 3)

	day := 24 * time.Hour
	first, second, later := time.Now().Add(-9*day).Unix(), time.Now().Add(-2*day).Unix(), time.Now().Add(5*day).Unix()
	saved := []metadata.Episode{
		{Number: 1, TitleJA: "家族の信用", AirDate: first},
		{Number: 2, AirDate: second},
		{Number: 3, AirDate: later},
	}
	if _, err := s.SaveEpisodes(ctx, 700, saved); err != nil {
		t.Fatal(err)
	}
	if !s.TitlesDue(ctx, 700) {
		t.Fatal("aired episodes without English titles should be due")
	}

	filled, err := s.FillEnglishTitles(ctx, 700, []metadata.Episode{
		{Number: 1, TitleEN: "Family's Trust", AirDate: first - int64(5*day/time.Second)},
		{Number: 2, TitleEN: "Part One's Second", AirDate: second - int64(180*day/time.Second)},
		{Number: 3, TitleEN: "Next Week", AirDate: later},
		{Number: 9, TitleEN: "Not On The List", AirDate: later},
	})
	if err != nil || filled != 2 {
		t.Fatalf("filled %d, %v; want episodes 1 and 3", filled, err)
	}
	if got := titleOf(t, s, 700, 1); got != "Family's Trust" {
		t.Errorf("episode 1 = %q", got)
	}
	if got := titleOf(t, s, 700, 2); got != "" {
		t.Errorf("episode 2 = %q; a listing months off is another cour", got)
	}

	saved[0].TitleEN = "Family Trust"
	if _, err := s.SaveEpisodes(ctx, 700, saved); err != nil {
		t.Fatal(err)
	}
	if got := titleOf(t, s, 700, 1); got != "Family Trust" {
		t.Errorf("episode 1 = %q; the episode source's own title should win", got)
	}
}

func TestEnglishTitlesRetryRecentGapsOftenAndOldOnesMonthly(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	day := 24 * time.Hour
	seedCatalogue(t, s, 701, "RELEASING", 1)
	seedCatalogue(t, s, 702, "FINISHED", 1)
	if _, err := s.SaveEpisodes(ctx, 701, []metadata.Episode{{Number: 1, AirDate: time.Now().Add(-3 * day).Unix()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveEpisodes(ctx, 702, []metadata.Episode{{Number: 1, AirDate: time.Now().Add(-400 * day).Unix()}}); err != nil {
		t.Fatal(err)
	}

	markTitlesAgo(t, s, 701, time.Hour)
	markTitlesAgo(t, s, 702, time.Hour)
	if s.TitlesDue(ctx, 701) || s.TitlesDue(ctx, 702) {
		t.Fatal("a show just tried should wait")
	}

	markTitlesAgo(t, s, 701, 13*time.Hour)
	markTitlesAgo(t, s, 702, 13*time.Hour)
	if !s.TitlesDue(ctx, 701) {
		t.Error("a recent gap should be asked again after 12 hours")
	}
	if s.TitlesDue(ctx, 702) {
		t.Error("an old gap should wait a month")
	}

	markTitlesAgo(t, s, 702, 31*day)
	if !s.TitlesDue(ctx, 702) {
		t.Error("an old gap should be asked again after a month")
	}
}
