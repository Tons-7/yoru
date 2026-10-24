package store

import (
	"context"
	"fmt"
	"testing"
)

// A season's finale plays on into the next season once it has aired; mid-season, or before it airs, it doesn't.
func TestTheFinalePlaysIntoTheNextSeason(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 12)
	seedCatalogue(t, s, 2, "NOT_YET_RELEASED", 12)
	seedCatalogue(t, s, 3, "FINISHED", 1)
	for _, f := range [][3]int{{1, 1, 1}, {2, 1, 2}, {3, 1, 3}} {
		if _, err := s.w.ExecContext(ctx, `INSERT INTO franchise (anime_id, root_id, ordinal) VALUES (?,?,?)`, f[0], f[1], f[2]); err != nil {
			t.Fatal(err)
		}
	}
	order := func(episode int) string {
		picks, _, err := s.PlayOrder(ctx, 1, episode, 1)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(picks)
	}

	if got := order(12); got != "[]" {
		t.Errorf("season 2 hasn't aired, yet after the finale %s; season 3 must not jump the queue", got)
	}
	s.w.ExecContext(ctx, `UPDATE anime SET status = 'RELEASING' WHERE id = 2`)
	if got := order(12); got != "[{2 1}]" {
		t.Errorf("after the finale %s, want season 2's first episode", got)
	}
	if got := order(5); got != "[{1 6}]" {
		t.Errorf("mid-season %s, want episode 6", got)
	}
}
