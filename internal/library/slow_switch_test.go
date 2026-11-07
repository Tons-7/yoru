package library

import (
	"context"
	"slices"
	"strings"
	"testing"

	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

const thirdHash = "5555555555555555555555555555555555555555"

func wasAdded(e *fakeEngine, hash string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.ContainsFunc(e.added, func(h string) bool { return strings.EqualFold(h, hash) })
}

func TestAQueuedEpisodeSkipsAReleaseRuledOutAsSlow(t *testing.T) {
	d, st, engine := workerQueueWith(t,
		release(otherHash, "[Crawl] Show - 02 [1080p].mkv", 500),
		release(thirdHash, "[Quick] Show - 02 [1080p].mkv", 80),
	)
	engine.finished[otherHash] = true
	engine.finished[thirdHash] = true
	d.prefetch.ruleOut(1, 2, otherHash)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{2}); err != nil {
		t.Fatal(err)
	}

	runUntil(t, d, func() bool { return len(queued(t, st)) == 0 })

	rec, ok, _ := st.TorrentForEpisode(ctx, 1, "2")
	if !ok || !strings.EqualFold(rec.InfoHash, thirdHash) {
		t.Fatalf("episode 2 is %+v, want the release that was not ruled out", rec)
	}
}

func TestARuledOutReleaseIsStillUsedWhenItIsTheOnlyOne(t *testing.T) {
	d, st, engine := workerQueueWith(t, release(otherHash, "[Crawl] Show - 02 [1080p].mkv", 500))
	engine.finished[otherHash] = true
	d.prefetch.ruleOut(1, 2, otherHash)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{2}); err != nil {
		t.Fatal(err)
	}

	runUntil(t, d, func() bool { return len(queued(t, st)) == 0 })

	rec, ok, _ := st.TorrentForEpisode(ctx, 1, "2")
	if !ok || !strings.EqualFold(rec.InfoHash, otherHash) {
		t.Fatalf("episode 2 is %+v, want the only release, slow or not", rec)
	}
}

func TestASlowDownloadIsKeptWhenNothingCouldReplaceIt(t *testing.T) {
	ctx := context.Background()
	prefs := score.DefaultPreferences()
	alone, _, _ := workerQueueWith(t, release(otherHash, "[Crawl] Show - 02 [1080p].mkv", 500))
	if alone.prefetch.alternative(ctx, 1, 2, 1, prefs, otherHash) {
		t.Error("the slow release counted as its own replacement; it would be deleted and fetched again from zero")
	}

	two, _, _ := workerQueueWith(t,
		release(otherHash, "[Crawl] Show - 02 [1080p].mkv", 500),
		release(thirdHash, "[Quick] Show - 02 [1080p].mkv", 80),
	)
	if !two.prefetch.alternative(ctx, 1, 2, 1, prefs, otherHash) {
		t.Error("another listed release was not seen as a replacement")
	}
	two.prefetch.ruleOut(1, 1, thirdHash)
	if two.prefetch.alternative(ctx, 1, 2, 1, prefs, otherHash) {
		t.Error("a release already found slow counted as a replacement")
	}
}

func TestAPausedReleaseIsNotResumedByTheNextEpisode(t *testing.T) {
	d, st, engine := workerQueueWith(t,
		release(otherHash, "[Pack] Show (01-12) [1080p][Batch]", 500),
		release(thirdHash, "[Quick] Show - 02 [1080p].mkv", 4),
	)
	engine.finished[otherHash] = true
	engine.finished[thirdHash] = true
	d.SetUserPaused(otherHash, true)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{2}); err != nil {
		t.Fatal(err)
	}

	runUntil(t, d, func() bool { return len(queued(t, st)) == 0 })

	if rec, ok, _ := st.TorrentForEpisode(ctx, 1, "2"); !ok || !strings.EqualFold(rec.InfoHash, thirdHash) {
		t.Fatalf("episode 2 is %+v; the pack paused by hand was taken again", rec)
	}
	if wasAdded(engine, otherHash) {
		t.Error("the paused pack was started again")
	}
}

func TestAnEpisodeOnlyAPausedReleaseHoldsStaysPausedInLine(t *testing.T) {
	d, st, engine := workerQueueWith(t, release(otherHash, "[Pack] Show (01-12) [1080p][Batch]", 500))
	engine.finished[otherHash] = true
	d.SetUserPaused(otherHash, true)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{2}); err != nil {
		t.Fatal(err)
	}

	d.step(ctx)

	rows := queued(t, st)
	if len(rows) != 1 || rows[0].State != "paused" {
		t.Fatalf("queue %+v; want episode 2 kept in line, paused", rows)
	}
	if wasAdded(engine, otherHash) {
		t.Error("the paused pack was started again")
	}
	if _, ok, _ := st.NextQueued(ctx); ok {
		t.Error("the queue took a paused episode")
	}

	if changed, err := d.PauseEpisode(ctx, 1, "2", false); err != nil || !changed {
		t.Fatalf("resume: %v, %v", changed, err)
	}
	if rows := queued(t, st); len(rows) != 1 || rows[0].State != "pending" {
		t.Errorf("queue %+v; resuming puts it back in line", rows)
	}
}

func TestAPauseByHandOutlastsARestart(t *testing.T) {
	d, st, _ := workerQueueWith(t)
	recordPack(t, st, otherHash, "2", "3")
	d.SetUserPaused(otherHash, true)
	d.SetUserPaused(thirdHash, true)

	again := NewDownloader(st, NewPrefetcher(st, nil, nil, discard()), nil, discard())
	again.restoreUserPaused(context.Background())
	if !again.prefetch.pausedByUser(otherHash) || !again.userPausedHash(otherHash) {
		t.Fatal("the pause was forgotten on restart; the download would start again by itself")
	}
	if again.prefetch.pausedByUser(thirdHash) {
		t.Error("a pause of a download that no longer exists came back on restart")
	}
	again.SetUserPaused(otherHash, false)
	if hashes, _ := st.HandPaused(context.Background()); len(hashes) != 0 {
		t.Errorf("still remembered after resuming: %v", hashes)
	}
}

func TestASlowPackSharedWithOtherEpisodesSwitchesOnlyThisOne(t *testing.T) {
	d, st, engine := workerQueueWith(t,
		release(otherHash, "[Pack] Show (01-12) [1080p][Batch]", 500),
		release(thirdHash, "[Quick] Show - 02 [1080p].mkv", 12),
	)
	engine.finished[thirdHash] = true
	ctx := context.Background()
	for i, ep := range []string{"2", "3"} {
		if err := st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: otherHash, EngineID: 50, Name: "[Pack] Show (01-12) [1080p][Batch]",
			AnimeID: 1, EpKey: ep, FilePath: "Show - 0" + ep + ".mkv", FileIndex: i + 2, TotalSize: 100}); err != nil {
			t.Fatal(err)
		}
	}
	engine.ids[50] = otherHash

	if !d.prefetch.switchAway(ctx, 1, 2, 1, score.DefaultPreferences(), &torrent.SlowError{ID: 50, Percent: 10}) {
		t.Fatal("a slow pack shared with episode 3 was kept for episode 2")
	}
	if engine.wasDeleted(otherHash) {
		t.Fatal("the shared pack was deleted with episode 3's data")
	}
	if _, err := st.Enqueue(ctx, 1, 1, []int{2}); err != nil {
		t.Fatal(err)
	}
	runUntil(t, d, func() bool { return len(queued(t, st)) == 0 })
	if rec, ok, _ := st.TorrentForEpisode(ctx, 1, "2"); !ok || !strings.EqualFold(rec.InfoHash, thirdHash) {
		t.Fatalf("episode 2 is %+v; want it fetched from the release that is not slow", rec)
	}
}

func TestTheOnlySeededReleaseIsNeverSwappedForDeadOnes(t *testing.T) {
	ctx := context.Background()
	prefs := score.DefaultPreferences()
	d, _, _ := workerQueueWith(t,
		release(otherHash, "[Lone] Show - 02 [1080p].mkv", 1),
		release(thirdHash, "[Dead] Show - 02 [1080p].mkv", 0),
		release("6666666666666666666666666666666666666666", "[Gone] Show - 02 [1080p].mkv", 0),
	)
	if d.prefetch.alternative(ctx, 1, 2, 1, prefs, otherHash) {
		t.Error("0-seeder releases counted as a replacement for the only one with a seeder; it would be deleted for nothing")
	}

	better, _, _ := workerQueueWith(t,
		release(otherHash, "[Lone] Show - 02 [1080p].mkv", 1),
		release(thirdHash, "[Fine] Show - 02 [1080p].mkv", 6),
	)
	if !better.prefetch.alternative(ctx, 1, 2, 1, prefs, otherHash) {
		t.Error("a release with more seeders should still be a replacement for a crawling one")
	}
}

func TestSwitchesStopAtTheLimit(t *testing.T) {
	p := NewPrefetcher(nil, nil, nil, discard())
	for i := range slowSwitches {
		if n := p.ruleOut(1, 2, otherHash); n != i+1 {
			t.Fatalf("switch %d counted %d; the same release ruled out again must count, or the queue loops", i+1, n)
		}
	}
	if p.switches(1, 2) < slowSwitches {
		t.Fatalf("switches = %d, want the limit reached", p.switches(1, 2))
	}
	if p.switches(1, 3) != 0 {
		t.Error("another episode inherited the switches")
	}
	if !p.ruledOut(strings.ToUpper(otherHash)) || p.ruledOut(thirdHash) {
		t.Error("a slow release is ruled out by its hash, for every episode")
	}
}

func TestTheNextEpisodeSkipsAPackFoundSlowForTheLastOne(t *testing.T) {
	d, st, engine := workerQueueWith(t,
		release(otherHash, "[Crawl] Show (01-12) [1080p][Batch]", 500),
		release(thirdHash, "[Quick] Show - 03 [1080p].mkv", 4),
	)
	engine.finished[otherHash] = true
	engine.finished[thirdHash] = true
	if found, err := d.prefetch.finder.Find(context.Background(), Request{AnimeID: 1, Episode: 3, Season: 1, Prefs: score.DefaultPreferences()}); err != nil ||
		found.Results[0].Torrent.InfoHash != otherHash {
		t.Fatalf("the pack should be the pick before it is ruled out: %v", err)
	}
	d.prefetch.ruleOut(1, 2, otherHash)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{3}); err != nil {
		t.Fatal(err)
	}

	runUntil(t, d, func() bool { return len(queued(t, st)) == 0 })

	rec, ok, _ := st.TorrentForEpisode(ctx, 1, "3")
	if !ok || !strings.EqualFold(rec.InfoHash, thirdHash) {
		t.Fatalf("episode 3 is %+v; the pack crawled for episode 2 and should not be tried again", rec)
	}
}
