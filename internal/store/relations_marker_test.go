package store

import (
	"context"
	"testing"
)

func TestNoRelationsMarkerSurvivesARebuild(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedShow(t, s, 1, 12)

	if known, _ := s.HasRelations(ctx, 1); known {
		t.Fatal("nothing recorded yet")
	}
	if err := s.MarkNoRelations(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if known, _ := s.HasRelations(ctx, 1); !known {
		t.Fatal("the marker was not recorded")
	}

	if _, err := s.RebuildFranchisesFor(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
	if known, _ := s.HasRelations(ctx, 1); !known {
		t.Fatal("the marker did not survive the rebuild")
	}

	seasons, err := s.Franchise(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(seasons.Seasons) > 1 {
		t.Fatalf("standalone show reports %d seasons", len(seasons.Seasons))
	}
}
