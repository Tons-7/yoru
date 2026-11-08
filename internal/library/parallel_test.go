package library

import (
	"context"
	"testing"
	"time"
)

func (d *Downloader) track(f *inFlight) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.running == nil {
		d.running = map[string]*inFlight{}
	}
	d.running[f.session()] = f
}

func (d *Downloader) step(parent context.Context) bool {
	next, ok, err := d.store.NextQueued(parent)
	if err != nil || !ok {
		return false
	}
	d.launch(parent, next, inTurn)
	d.workers.Wait()
	return true
}

func runningFrom(t *testing.T, d *Downloader, limit int) (int, startedAs) {
	t.Helper()
	next, how, ok := d.claim(context.Background(), limit)
	if !ok {
		return 0, how
	}
	d.track(&inFlight{animeID: next.AnimeID, epKey: next.EpKey, stop: func() {}, forced: how == onPurpose, along: how == alongside})
	return next.Episode, how
}

func TestOneAtATimeStartsOneAndWaits(t *testing.T) {
	d, st := pausableQueue(t)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if d.Parallel(ctx) != 1 {
		t.Fatalf("parallel = %d, want one at a time by default", d.Parallel(ctx))
	}
	if ep, _ := runningFrom(t, d, 1); ep != 1 {
		t.Fatalf("started episode %d, want 1", ep)
	}
	if ep, _ := runningFrom(t, d, 1); ep != 0 {
		t.Errorf("episode %d started while one was already downloading", ep)
	}
}

func TestTheSettingLetsMoreRunAtOnce(t *testing.T) {
	d, st := pausableQueue(t)
	ctx := context.Background()
	if err := st.SetSetting(ctx, parallelSetting, "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Enqueue(ctx, 1, 1, []int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	limit := d.Parallel(ctx)
	if limit != 2 {
		t.Fatalf("parallel = %d, want 2", limit)
	}
	first, _ := runningFrom(t, d, limit)
	second, _ := runningFrom(t, d, limit)
	third, _ := runningFrom(t, d, limit)
	if first != 1 || second != 2 || third != 0 {
		t.Errorf("started %d, %d, %d; want 1 and 2 together, then a wait", first, second, third)
	}
}

func TestASlowDownloadStopsHoldingTheLine(t *testing.T) {
	d, st := pausableQueue(t)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	runningFrom(t, d, 1)
	for len(d.wake) > 0 {
		<-d.wake
	}
	d.markSlow(1, 1)
	select {
	case <-d.wake:
	default:
		t.Error("marking a download slow did not wake the queue")
	}
	if ep, _ := runningFrom(t, d, 1); ep != 2 {
		t.Errorf("started %d beside the slow one, want episode 2", ep)
	}
}

func TestASlowDownloadThatRecoversCountsAgain(t *testing.T) {
	d, st := pausableQueue(t)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	runningFrom(t, d, 1)
	d.markSlow(1, 1)
	d.mu.Lock()
	d.running[lineKey(1, "1")].slowUntil = time.Now().Add(-time.Second)
	d.mu.Unlock()
	if ep, _ := runningFrom(t, d, 1); ep != 0 {
		t.Errorf("episode %d started beside a download no longer reported slow", ep)
	}
}

func TestAnEpisodeOfATorrentAlreadyRunningRidesAlong(t *testing.T) {
	d, st := pausableQueue(t)
	ctx := context.Background()
	recordPack(t, st, otherHash, "2", "3")
	if _, err := st.Enqueue(ctx, 1, 1, []int{2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	runningFrom(t, d, 1)
	ep, how := runningFrom(t, d, 1)
	if ep != 3 || how != alongside {
		t.Errorf("started %d as %v; want episode 3 from the same pack, alongside", ep, how)
	}
	if ep, _ := runningFrom(t, d, 1); ep != 0 {
		t.Errorf("episode %d from another release started past the limit", ep)
	}
}

func TestClaimsOnOneTorrentAreCounted(t *testing.T) {
	_, st := pausableQueue(t)
	p := NewPrefetcher(st, nil, nil, discard())
	first := p.claimWhile(otherHash)
	second := p.claimWhile(otherHash)
	if p.claims(otherHash) != 2 {
		t.Fatalf("claims = %d, want 2", p.claims(otherHash))
	}
	first()
	first()
	if !p.Claimed(otherHash) {
		t.Error("one download letting go freed a torrent the other still uses")
	}
	second()
	if p.Claimed(otherHash) {
		t.Error("the torrent stayed claimed with nothing using it")
	}
}

func TestPickingAReleaseForTheRunningEpisodeKeepsOneAtATime(t *testing.T) {
	d, st := pausableQueue(t)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	runningFrom(t, d, 1)
	if ok, err := d.Restart(ctx, 1, "1"); err != nil || !ok {
		t.Fatalf("restart = %v, %v", ok, err)
	}
	if _, err := st.RequeueActive(ctx, 1, "1"); err != nil {
		t.Fatal(err)
	}
	d.mu.Lock()
	delete(d.running, lineKey(1, "1"))
	d.mu.Unlock()

	if ep, how := runningFrom(t, d, 1); ep != 1 || how != inTurn {
		t.Fatalf("started %d as %v; want episode 1 again, in its turn", ep, how)
	}
	if ep, _ := runningFrom(t, d, 1); ep != 0 {
		t.Errorf("episode %d started beside it at one at a time", ep)
	}
}

func TestAnEpisodeIsNotClaimedAgainBeforeItsLastWorkerLeaves(t *testing.T) {
	d, st := pausableQueue(t)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{1}); err != nil {
		t.Fatal(err)
	}
	runningFrom(t, d, 1)
	d.force(1, "1")
	if _, err := st.RequeueActive(ctx, 1, "1"); err != nil {
		t.Fatal(err)
	}
	if ep, how := runningFrom(t, d, 1); how == onPurpose {
		t.Fatalf("episode %d claimed on purpose while its old worker was still winding down", ep)
	}
	d.mu.Lock()
	delete(d.running, lineKey(1, "1"))
	d.mu.Unlock()
	if ep, how := runningFrom(t, d, 1); ep != 1 || how != onPurpose {
		t.Errorf("started %d as %v once the old worker left; want episode 1 on purpose", ep, how)
	}
}

func TestAnotherDownloadStartsOnlyWithRoomForIt(t *testing.T) {
	d, _ := pausableQueue(t)
	d.dir = t.TempDir()
	old := freeSpace
	t.Cleanup(func() { freeSpace = old })
	freeSpace = func(string) (int64, string, error) { return MinFree + MinFree/2, "D:", nil }

	if !d.roomForAnother() {
		t.Fatal("the first download was held back with room for it")
	}
	d.track(&inFlight{animeID: 1, epKey: "1", stop: func() {}})
	if d.roomForAnother() {
		t.Error("a second download started with room for only one")
	}
}
