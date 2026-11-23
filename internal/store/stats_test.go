package store

import (
	"context"
	"testing"
	"time"
)

func TestStatsGroupTimeByGenreStudioAndSeason(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	play(t, s, 1, "1", 600, 1440)
	play(t, s, 2, "1", 300, 1440)
	for _, q := range []string{
		`UPDATE anime SET genres = '["Action","Drama"]', studios = '["Bones"]', season = 'FALL', season_year = 2025 WHERE id = 1`,
		`UPDATE anime SET genres = '["Action"]', studios = '["Madhouse"]', season = 'SPRING', season_year = 2024 WHERE id = 2`,
	} {
		if _, err := s.w.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}

	st, err := s.Stats(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if st.Shows != 2 {
		t.Errorf("shows = %d, want 2", st.Shows)
	}
	if len(st.Genres) != 2 || st.Genres[0].Name != "Action" || st.Genres[0].Shows != 2 {
		t.Fatalf("genres = %+v, want Action first across both shows", st.Genres)
	}
	if st.Genres[0].Seconds <= st.Genres[1].Seconds {
		t.Errorf("Action %v should outrank Drama %v", st.Genres[0].Seconds, st.Genres[1].Seconds)
	}
	if len(st.Studios) != 2 || st.Studios[0].Name != "Bones" {
		t.Errorf("studios = %+v, want Bones first (more time)", st.Studios)
	}
	if len(st.Seasons) != 2 || st.Seasons[0].Name != "FALL 2025" {
		t.Errorf("seasons = %+v, want FALL 2025 first", st.Seasons)
	}
	if len(st.Months) != 12 || st.Months[11].Month != time.Now().Format("2006-01") {
		t.Errorf("months = %+v, want 12 ending this month", st.Months)
	}
	if st.TotalSeconds <= 0 || st.Months[11].Seconds <= 0 {
		t.Errorf("watch time not counted: total %v, this month %v", st.TotalSeconds, st.Months[11].Seconds)
	}
}
