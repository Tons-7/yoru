package library

import (
	"context"
	"testing"

	"yoru/internal/store"
)

func pausableQueue(t *testing.T) (*Downloader, *store.Store) {
	t.Helper()
	st := prefetchStore(t)
	episodes := 12
	if _, err := st.ImportList(context.Background(),
		[]store.Anime{{ID: 1, Romaji: "Show", Synonyms: "[]", Genres: "[]", Episodes: &episodes}},
		nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	return NewDownloader(st, NewPrefetcher(st, nil, nil, discard()), nil, discard()), st
}

func TestPausedQueueStartsNothingUntilResumed(t *testing.T) {
	d, _ := pausableQueue(t)
	ctx := context.Background()
	if d.held() || d.Paused() {
		t.Fatal("a new queue starts out paused")
	}

	if err := d.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	if !d.held() || !d.Paused() {
		t.Error("a paused queue would still start the next download")
	}

	if err := d.SetPaused(ctx, false); err != nil {
		t.Fatal(err)
	}
	if d.held() {
		t.Error("a resumed queue is still held")
	}
	select {
	case <-d.wake:
	default:
		t.Error("resuming did not wake the worker")
	}
}

func TestQueueWaitsForRoomOnTheDrive(t *testing.T) {
	d, _ := pausableQueue(t)
	d.WithRoom(t.TempDir())
	free := int64(MinFree - 1)
	old := freeSpace
	freeSpace = func(string) (int64, string, error) { return free, "D:", nil }
	t.Cleanup(func() { freeSpace = old })

	if d.roomy() {
		t.Fatal("a download would start with the drive nearly full")
	}
	if got, drive := d.Short(); got != free || drive != "D:" {
		t.Errorf("short = %d on %q, want %d on D:", got, drive, free)
	}
	free = MinFree
	if !d.roomy() {
		t.Fatal("room again, yet the queue still waits")
	}
	if got, _ := d.Short(); got != 0 {
		t.Errorf("still says short: %d", got)
	}
}

func TestPausingTheQueueStopsEveryDownloadInFlight(t *testing.T) {
	d, _ := pausableQueue(t)
	stopped := 0
	d.track(&inFlight{animeID: 1, epKey: "3", stop: func() { stopped++ }})
	d.track(&inFlight{animeID: 1, epKey: "4", stop: func() { stopped++ }})

	if err := d.SetPaused(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if stopped != 2 {
		t.Errorf("stopped %d of 2 downloads under a paused queue", stopped)
	}
}

func TestPausedQueueStaysPausedAcrossARestart(t *testing.T) {
	d, st := pausableQueue(t)
	ctx := context.Background()
	if err := d.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}

	again := NewDownloader(st, NewPrefetcher(st, nil, nil, discard()), nil, discard())
	again.restorePaused(ctx)
	if !again.Paused() {
		t.Error("the pause was forgotten")
	}

	if err := again.SetPaused(ctx, false); err != nil {
		t.Fatal(err)
	}
	third := NewDownloader(st, NewPrefetcher(st, nil, nil, discard()), nil, discard())
	third.restorePaused(ctx)
	if third.Paused() {
		t.Error("a resumed queue came back paused")
	}
}

func TestStartNowRunsAlongsideAndResumes(t *testing.T) {
	d, st := pausableQueue(t)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	active, _, err := st.NextQueued(ctx)
	if err != nil || active.Episode != 1 {
		t.Fatalf("active %+v, err %v", active, err)
	}
	stopped := false
	d.track(&inFlight{animeID: 1, epKey: "1", stop: func() { stopped = true }})
	if err := d.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	stopped = false

	moved, err := d.Now(ctx, 1, "3")
	if err != nil || !moved {
		t.Fatalf("moved %v, err %v", moved, err)
	}
	if stopped || d.Paused() {
		t.Errorf("in flight stopped: %v, queue still paused: %v", stopped, d.Paused())
	}

	next, how, ok := d.claim(ctx, 1)
	if !ok || next.Episode != 3 || how != onPurpose {
		t.Errorf("claimed %+v as %v, %v; want episode 3 started on purpose past the limit", next, how, ok)
	}
}

func TestDownloadNowIgnoresWhatIsNotWaiting(t *testing.T) {
	d, _ := pausableQueue(t)
	stopped := false
	d.track(&inFlight{animeID: 1, epKey: "1", stop: func() { stopped = true }})

	moved, err := d.Now(context.Background(), 1, "9")
	if err != nil || moved || stopped {
		t.Errorf("moved %v, stopped %v, err %v", moved, stopped, err)
	}
}
