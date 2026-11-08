package store

import (
	"context"
	"testing"
)

func TestAnInterruptedDownloadResumesFirstAfterARestart(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Enqueue(ctx, 2, 1, []int{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MoveQueued(ctx, 2, "1", "bottom"); err != nil {
		t.Fatal(err)
	}
	for {
		next, ok, err := st.NextQueued(ctx)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		if next.AnimeID == 2 {
			break
		}
		if err := st.FinishQueued(ctx, next.AnimeID, next.EpKey, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Enqueue(ctx, 3, 1, []int{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Prioritise(ctx, 3, "1"); err != nil {
		t.Fatal(err)
	}

	if err := st.ResetActive(ctx); err != nil {
		t.Fatal(err)
	}
	next, ok, err := st.NextQueued(ctx)
	if err != nil || !ok || next.AnimeID != 2 {
		t.Fatalf("took %+v, %v, %v; want the download that was running when the app closed", next, ok, err)
	}
}
