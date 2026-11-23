package library

import (
	"context"
	"errors"
	"testing"

	"yoru/internal/score"
)

func TestFindWithoutSitesSaysSo(t *testing.T) {
	st := bleachStore(t)
	f := NewFinder(st, nil, discard())
	_, err := f.Find(context.Background(), Request{
		AnimeID: tybwCalamity, Episode: 2, Prefs: score.DefaultPreferences(),
	})
	if !errors.Is(err, ErrNoIndexers) {
		t.Fatalf("err = %v, want ErrNoIndexers", err)
	}
}
