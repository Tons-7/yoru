package store

import (
	"context"
	"testing"
)

// The root losing its sequel edge leaves its old seasons a franchise of their own, not two loose shows.
func TestDetachedRootKeepsItsSiblingsGrouped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i, id := range []int{70, 71, 72} {
		seedCatalogue(t, s, id, "FINISHED", 12)
		s.w.ExecContext(ctx, `UPDATE anime SET start_date = ? WHERE id = ?`, []string{"2018-01-01", "2020-01-01", "2022-01-01"}[i], id)
	}
	if err := s.SaveRelations(ctx, nil, []Relation{
		{AnimeID: 70, RelatedID: 71, Kind: "SEQUEL"},
		{AnimeID: 71, RelatedID: 72, Kind: "SEQUEL"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RebuildFranchises(ctx); err != nil {
		t.Fatal(err)
	}
	// 70 is fetched again and has no season edges any more.
	if err := s.SaveRelations(ctx, []int{70}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RebuildFranchisesFor(ctx, []int{70}, nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{71, 72} {
		var root int
		if err := s.r.QueryRowContext(ctx, `SELECT root_id FROM franchise WHERE anime_id = ?`, id).Scan(&root); err != nil || root != 71 {
			t.Errorf("%d: root %d, %v; want still grouped under 71", id, root, err)
		}
	}
}

// A sequel the walk found but no row describes yet must not become season one.
func TestUnknownMemberSortsLast(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 50, "FINISHED", 12)
	seedCatalogue(t, s, 60, "FINISHED", 12)
	s.w.ExecContext(ctx, `UPDATE anime SET start_date = '2020-01-01' WHERE id = 50`)
	s.w.ExecContext(ctx, `UPDATE anime SET start_date = '2022-01-01' WHERE id = 60`)

	if err := s.SaveRelations(ctx, nil, []Relation{
		{AnimeID: 50, RelatedID: 60, Kind: "SEQUEL"},
		{AnimeID: 60, RelatedID: 7, Kind: "SEQUEL"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RebuildFranchises(ctx); err != nil {
		t.Fatal(err)
	}

	want := map[int]int{50: 1, 60: 2, 7: 3}
	for id, ordinal := range want {
		var got, root int
		if err := s.r.QueryRowContext(ctx,
			`SELECT ordinal, root_id FROM franchise WHERE anime_id = ?`, id).Scan(&got, &root); err != nil {
			t.Fatalf("%d: %v", id, err)
		}
		if got != ordinal || root != 50 {
			t.Errorf("%d: ordinal %d root %d, want %d root 50", id, got, root, ordinal)
		}
	}
}
