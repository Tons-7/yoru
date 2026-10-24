package library

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"yoru/internal/indexer"
	"yoru/internal/store"
)

// A show on the list as watching, with its status synced from the tracker rather than set here.
// Episode 5 airs at `airs`; everything before has aired.
func watchedShow(t *testing.T, airs time.Time) *store.Store {
	t.Helper()
	st := prefetchStore(t)
	setAiring(t, st, 5, airs)
	return st
}

func setAiring(t *testing.T, st *store.Store, nextEpisode int, airs time.Time) {
	t.Helper()
	ctx := context.Background()
	episodes, current, planning := 12, "CURRENT", "PLANNING"
	at := int(airs.Unix())
	if _, err := st.ImportList(ctx,
		[]store.Anime{
			{ID: 1, Romaji: "Show", Synonyms: "[]", Genres: "[]", Episodes: &episodes, NextEpisode: &nextEpisode, NextAiringAt: &at},
			{ID: 2, Romaji: "Later", Synonyms: "[]", Genres: "[]", Episodes: &episodes},
		},
		[]store.Entry{{ID: 11, AnimeID: 1, Status: &current, Progress: 3}, {ID: 12, AnimeID: 2, Status: &planning}},
		store.ImportMerge); err != nil {
		t.Fatal(err)
	}
}

func notifications(t *testing.T, st *store.Store) []store.Notification {
	t.Helper()
	items, err := st.Notifications(context.Background(), false, 50)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func watcherWith(st *store.Store, releases ...indexer.Torrent) *Watcher {
	idx := fixedIndexer{results: releases}
	return NewWatcher(st, NewFinder(st, idx, discard()), idx, discard())
}

func episodeOf(n store.Notification) int {
	if n.Episode == nil {
		return 0
	}
	return *n.Episode
}

// Nothing past the viewer has aired, so there is no news; once an episode airs it is announced, once.
func TestWatcherAnnouncesAnEpisodeOnceItAirs(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 4, time.Now().Add(time.Hour))
	ctx := context.Background()
	w := watcherWith(st,
		release(goodHash, "[Best] Show - 04 [1080p].mkv", 500),
		release(deadHash, "[Best] Show - 05 [1080p].mkv", 500),
		release("3333333333333333333333333333333333333333", "[Best] Later - 01 [1080p].mkv", 500),
	)

	rep, err := w.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Checked != 1 || rep.Notified != 0 || len(notifications(t, st)) != 0 {
		t.Fatalf("announced before anything aired: %+v", rep)
	}

	// Episode 4 airs, and 5 is next.
	setAiring(t, st, 5, time.Now().Add(time.Hour))
	rep, _ = w.Poll(ctx)
	got := notifications(t, st)
	if rep.Notified != 1 || len(got) != 1 || episodeOf(got[0]) != 4 {
		t.Fatalf("report = %+v, notifications = %+v; want episode 4 announced", rep, got)
	}
	if rep, _ := w.Poll(ctx); rep.Notified != 0 || len(notifications(t, st)) != 1 {
		t.Fatalf("a later poll announced it again: %+v", rep)
	}
}

// The reported bug: on episode 3 of a show at 9, with part of the backlog recorded by an earlier build,
// every poll announced the rest of it. Only the latest aired episode is news, whatever the records say.
func TestWatcherAnnouncesOnlyTheLatestWhateverWasRecordedBefore(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 10, time.Now().Add(24*time.Hour))
	ctx := context.Background()
	for ep := 4; ep <= 6; ep++ {
		if err := st.RecordNotified(ctx, fmt.Sprintf("old:%d", ep), 1, ep); err != nil {
			t.Fatal(err)
		}
	}
	var releases []indexer.Torrent
	for ep := 4; ep <= 9; ep++ {
		releases = append(releases, release(fmt.Sprintf("%040d", ep), fmt.Sprintf("[Best] Show - %02d [1080p].mkv", ep), 500))
	}
	w := watcherWith(st, releases...)

	rep, _ := w.Poll(ctx)
	got := notifications(t, st)
	if rep.Notified != 1 || len(got) != 1 || episodeOf(got[0]) != 9 {
		t.Fatalf("report = %+v, notifications = %+v; want episode 9 alone", rep, got)
	}
	if rep, _ := w.Poll(ctx); rep.Notified != 0 || len(notifications(t, st)) != 1 {
		t.Fatalf("a later poll announced more: %+v", rep)
	}
}

// Episodes 4 and 5 aired while Yoru was off: auto-download fetches both, oldest first; the bell names only 5.
func TestAutoDownloadCatchesUpWhatAiredWhileOff(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 6, time.Now().Add(time.Hour))
	ctx := context.Background()
	if err := st.SetAnimePref(ctx, 1, "autodownload.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	// The watch saw episode 3 before Yoru was closed.
	if err := st.RecordNotified(ctx, "old:3", 1, 3); err != nil {
		t.Fatal(err)
	}
	w := watcherWith(st,
		release(goodHash, "[Best] Show - 04 [1080p].mkv", 500),
		release(deadHash, "[Best] Show - 05 [1080p].mkv", 500),
	).WithDownloader(NewDownloader(st, nil, nil, discard()))

	for range 2 {
		if _, err := w.Poll(ctx); err != nil {
			t.Fatal(err)
		}
	}
	queued, err := st.QueuedDownloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var episodes []int
	for _, q := range queued {
		episodes = append(episodes, q.Episode)
	}
	if fmt.Sprint(episodes) != "[4 5]" {
		t.Errorf("queued %v, want [4 5]", episodes)
	}
	if got := notifications(t, st); len(got) != 1 || episodeOf(got[0]) != 5 {
		t.Errorf("notifications = %+v, want episode 5 alone", got)
	}
}

// A long absence is caught up a few episodes a poll, and one no site has yet isn't searched again each time.
func TestCatchUpIsSpreadOverPolls(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 12, time.Now().Add(time.Hour))
	ctx := context.Background()
	if err := st.SetAnimePref(ctx, 1, "autodownload.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordNotified(ctx, "old:3", 1, 3); err != nil {
		t.Fatal(err)
	}
	var releases []indexer.Torrent
	for ep := 4; ep <= 11; ep++ {
		if ep == 6 {
			continue // no site has episode 6
		}
		releases = append(releases, release(fmt.Sprintf("%040d", ep), fmt.Sprintf("[Best] Show - %02d [1080p].mkv", ep), 500))
	}
	w := watcherWith(st, releases...).WithDownloader(NewDownloader(st, nil, nil, discard()))

	count := func() int {
		queued, err := st.QueuedDownloads(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(queued)
	}
	w.Poll(ctx)
	// Four tried (4-7, 6 found nowhere), and the latest.
	if n := count(); n != 4 {
		t.Fatalf("first poll queued %d, want 4, 5, 7 and 11", n)
	}
	w.Poll(ctx)
	w.Poll(ctx)
	// 4-10 but 6, and 11: the missing one waits instead of using up each poll.
	if n := count(); n != 7 {
		t.Errorf("after three polls %d queued, want 7", n)
	}
}

// The group posts a fixed v2 of an episode already taken and unwatched: it's queued over the v1, a single time.
func TestAFixedReleaseReplacesTheTakenOne(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 5, time.Now().Add(time.Hour))
	ctx := context.Background()
	if err := st.SetAnimePref(ctx, 1, "autodownload.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	const v1, v2 = "1111111111111111111111111111111111111111", "2222222222222222222222222222222222222222"
	if err := st.RecordNotified(ctx, v1, 1, 4); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: v1, Name: "[Best] Show - 04 [1080p].mkv",
		AnimeID: 1, EpKey: "4", TotalSize: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	w := watcherWith(st,
		release(v1, "[Best] Show - 04 [1080p].mkv", 500),
		release(v2, "[Best] Show - 04v2 [1080p].mkv", 20),
	).WithDownloader(NewDownloader(st, nil, nil, discard()))

	for range 2 {
		w.Poll(ctx)
	}
	queued, err := st.QueuedDownloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || queued[0].Episode != 4 || queued[0].InfoHash != v2 {
		t.Fatalf("queued %+v, want episode 4 pinned to the v2", queued)
	}
}

// A same-version re-upload or another group's release never replaces a taken episode without a wait set.
func TestNothingReplacesATakenEpisodeButAFix(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 5, time.Now().Add(time.Hour))
	ctx := context.Background()
	st.SetAnimePref(ctx, 1, "autodownload.enabled", "true")
	const taken = "1111111111111111111111111111111111111111"
	st.RecordNotified(ctx, taken, 1, 4)
	st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: taken, Name: "[Best] Show - 04 [720p].mkv", AnimeID: 1, EpKey: "4", TotalSize: 1 << 29})
	w := watcherWith(st,
		release(taken, "[Best] Show - 04 [720p].mkv", 500),
		release("3333333333333333333333333333333333333333", "[Other] Show - 04 [1080p].mkv", 900),
	).WithDownloader(NewDownloader(st, nil, nil, discard()))
	w.Poll(ctx)
	if queued, _ := st.QueuedDownloads(ctx); len(queued) != 0 {
		t.Fatalf("queued %+v; only a fix replaces a taken episode unless a wait is set", queued)
	}

	// With a wait set, the release it waits for (here the top resolution) does.
	st.SetAnimePref(ctx, 1, "autodownload.wait_hours", "3")
	w.lookedAt = nil
	w.Poll(ctx)
	if queued, _ := st.QueuedDownloads(ctx); len(queued) != 1 || queued[0].InfoHash != "3333333333333333333333333333333333333333" {
		t.Fatalf("queued %+v, want the 1080p over the taken 720p", queued)
	}
}

// With a wait set, an equally good release with more seeders never replaces the taken one.
func TestAWaitNeverSwapsForSeeders(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 5, time.Now().Add(time.Hour))
	ctx := context.Background()
	st.SetAnimePref(ctx, 1, "autodownload.enabled", "true")
	st.SetAnimePref(ctx, 1, "autodownload.wait_hours", "3")
	const taken = "1111111111111111111111111111111111111111"
	st.RecordNotified(ctx, taken, 1, 4)
	st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: taken, Name: "[Best] Show - 04 [1080p].mkv", AnimeID: 1, EpKey: "4", TotalSize: 1 << 30})
	w := watcherWith(st,
		release(taken, "[Best] Show - 04 [1080p].mkv", 20),
		release("3333333333333333333333333333333333333333", "[Other] Show - 04 [1080p].mkv", 900),
	).WithDownloader(NewDownloader(st, nil, nil, discard()))
	w.Poll(ctx)
	if queued, _ := st.QueuedDownloads(ctx); len(queued) != 0 {
		t.Fatalf("queued %+v; a better-seeded equal isn't an upgrade", queued)
	}
}

// With a wait set, a new episode is held until a preferred group's release lands, then taken straight away.
func TestAutoDownloadWaitsForThePreferredGroup(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 5, time.Now().Add(time.Hour))
	ctx := context.Background()
	st.SetAnimePref(ctx, 1, "autodownload.enabled", "true")
	st.SetAnimePref(ctx, 1, "autodownload.wait_hours", "6")
	st.SetSetting(ctx, "release.prefer_groups", `["Best"]`)
	idx := &fixedIndexer{results: []indexer.Torrent{release(deadHash, "[Other] Show - 04 [1080p].mkv", 900)}}
	w := NewWatcher(st, NewFinder(st, idx, discard()), idx, discard()).WithDownloader(NewDownloader(st, nil, nil, discard()))

	w.Poll(ctx)
	if queued, _ := st.QueuedDownloads(ctx); len(queued) != 0 {
		t.Fatalf("queued %+v while the wait runs", queued)
	}
	if waits, _ := st.Waits(ctx, 0); len(waits) != 1 || waits[0].Episode != 4 {
		t.Fatalf("waits = %+v, want episode 4", waits)
	}

	idx.results = append(idx.results, release(goodHash, "[Best] Show - 04 [1080p].mkv", 40))
	w.Poll(ctx)
	queued, _ := st.QueuedDownloads(ctx)
	if len(queued) != 1 || queued[0].InfoHash != goodHash {
		t.Fatalf("queued %+v, want the preferred group's release", queued)
	}
	if waits, _ := st.Waits(ctx, 0); len(waits) != 0 {
		t.Errorf("the wait outlived its episode: %+v", waits)
	}
}

// airsNext saves show 1 as AniList would answer: releasing, its next episode due at airs, or undated (nil).
func airsNext(t *testing.T, st *store.Store, next int, airs *time.Time) {
	t.Helper()
	episodes, current, status := 12, "CURRENT", "RELEASING"
	a := store.Anime{ID: 1, Romaji: "Show", Synonyms: "[]", Genres: "[]", Episodes: &episodes, Status: &status}
	if airs != nil {
		at := int(airs.Unix())
		a.NextEpisode, a.NextAiringAt = &next, &at
	}
	if _, err := st.ImportList(context.Background(), []store.Anime{a},
		[]store.Entry{{ID: 11, AnimeID: 1, Status: &current, Progress: 3}}, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
}

// AniList moves a followed show's episode two days later: told a single time, with the new date.
func TestADelayedEpisodeIsAnnounced(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	soon, later := time.Now().Add(time.Hour), time.Now().Add(49*time.Hour)
	airsNext(t, st, 5, &soon)
	airsNext(t, st, 5, &later)
	w := watcherWith(st)
	w.Poll(ctx)
	w.Poll(ctx)
	var delays []store.Notification
	for _, n := range notifications(t, st) {
		if n.Kind == store.NotifyDelay {
			delays = append(delays, n)
		}
	}
	if len(delays) != 1 || delays[0].Title != "Show · Episode 5 delayed" || !strings.HasPrefix(delays[0].Body, "Now ") {
		t.Fatalf("delay notifications = %+v, want one naming the new date", delays)
	}
	if d, ok := st.ShowDelay(ctx, 1); !ok || d.Episode != 5 || d.WasAt != soon.Unix() {
		t.Errorf("show delay = %+v, %v", d, ok)
	}
}

// A future episode losing its date is a delay, said with no date and again when one comes; a finale whose
// date AniList drops after it aired isn't.
func TestADroppedDateIsADelayAndAFinaleIsNot(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	soon, later := time.Now().Add(time.Hour), time.Now().Add(9*24*time.Hour)
	airsNext(t, st, 5, &soon)
	airsNext(t, st, 5, nil)
	w := watcherWith(st)
	w.Poll(ctx)
	airsNext(t, st, 5, &later)
	w.Poll(ctx)
	var bodies []string
	for _, n := range notifications(t, st) {
		if n.Kind == store.NotifyDelay {
			bodies = append(bodies, n.Body)
		}
	}
	if len(bodies) != 2 || !slices.Contains(bodies, "No new date yet") {
		t.Fatalf("delay notifications %q, want no date then the new one", bodies)
	}

	fin := prefetchStore(t)
	aired := time.Now().Add(-time.Hour)
	airsNext(t, fin, 12, &aired)
	airsNext(t, fin, 12, nil)
	if d, err := fin.UnannouncedDelays(ctx); err != nil || len(d) != 0 {
		t.Fatalf("a finale read as a delay: %+v, %v", d, err)
	}
}

// An episode asked for from the player downloads when it's out, alone: the show's later ones don't.
func TestAWantedEpisodeDownloadsWhenOut(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	setAiring(t, st, 4, time.Now().Add(time.Hour))
	if err := st.SetAnimePref(ctx, 1, "autodownload.want", "4"); err != nil {
		t.Fatal(err)
	}
	idx := &fixedIndexer{}
	w := NewWatcher(st, NewFinder(st, idx, discard()), idx, discard()).WithDownloader(NewDownloader(st, nil, nil, discard()))
	w.Poll(ctx)
	if queued, _ := st.QueuedDownloads(ctx); len(queued) != 0 {
		t.Fatalf("queued %+v before it aired", queued)
	}

	setAiring(t, st, 5, time.Now().Add(time.Hour))
	idx.results = []indexer.Torrent{release(goodHash, "[Best] Show - 04 [1080p].mkv", 500)}
	w.Poll(ctx)
	if queued, _ := st.QueuedDownloads(ctx); len(queued) != 1 || queued[0].Episode != 4 {
		t.Fatalf("queued %+v, want episode 4", queued)
	}
	if f, _ := st.FollowOf(ctx, 1); f.Wanted != 0 {
		t.Errorf("still wanted after it was taken: %+v", f)
	}

	setAiring(t, st, 6, time.Now().Add(time.Hour))
	idx.results = append(idx.results, release(deadHash, "[Best] Show - 05 [1080p].mkv", 500))
	w.Poll(ctx)
	if queued, _ := st.QueuedDownloads(ctx); len(queued) != 1 {
		t.Fatalf("queued %+v; only the asked-for episode downloads", queued)
	}
}

// Asked for from the player, an episode isn't held by the show's auto-download wait.
func TestAWantedEpisodeSkipsTheWait(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	setAiring(t, st, 5, time.Now().Add(time.Hour))
	st.SetAnimePref(ctx, 1, "autodownload.enabled", "true")
	st.SetAnimePref(ctx, 1, "autodownload.wait_hours", "6")
	st.SetAnimePref(ctx, 1, "autodownload.want", "4")
	// 720p: not the release a wait holds out for.
	w := watcherWith(st, release(goodHash, "[Other] Show - 04 [720p].mkv", 500)).WithDownloader(NewDownloader(st, nil, nil, discard()))
	w.Poll(ctx)
	if queued, _ := st.QueuedDownloads(ctx); !slices.ContainsFunc(queued, func(q store.QueuedDownload) bool { return q.Episode == 4 }) {
		t.Fatalf("queued %+v, want the asked episode 4 at once", queued)
	}
}

// The asked episode is the one taken, though a later one aired meanwhile (Yoru closed, or a raw-only week).
func TestAWantedEpisodeIsTheOneTakenWhenLaterOnesAired(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	setAiring(t, st, 7, time.Now().Add(time.Hour))
	st.SetAnimePref(ctx, 1, "autodownload.want", "4")
	idx := &fixedIndexer{results: []indexer.Torrent{
		release(goodHash, "[Best] Show - 04 [1080p].mkv", 500),
		release(deadHash, "[Best] Show - 06 [1080p].mkv", 500),
	}}
	w := NewWatcher(st, NewFinder(st, idx, discard()), idx, discard()).WithDownloader(NewDownloader(st, nil, nil, discard()))
	w.Poll(ctx)
	if queued, _ := st.QueuedDownloads(ctx); len(queued) != 1 || queued[0].Episode != 4 {
		t.Fatalf("queued %+v, want episode 4 alone", queued)
	}
	if f, _ := st.FollowOf(ctx, 1); f.Wanted != 0 {
		t.Errorf("still wanted after it was taken: %+v", f)
	}
}

// A release picked by hand, or one being watched, is never swapped for auto-download's choice.
func TestAHandPickedOrStartedEpisodeIsNeverSwapped(t *testing.T) {
	for _, c := range []struct {
		name    string
		manual  bool
		started bool
	}{{"picked by hand", true, false}, {"being watched", false, true}} {
		st := prefetchStore(t)
		setAiring(t, st, 5, time.Now().Add(time.Hour))
		ctx := context.Background()
		st.SetAnimePref(ctx, 1, "autodownload.enabled", "true")
		st.SetAnimePref(ctx, 1, "autodownload.wait_hours", "3")
		const taken = "1111111111111111111111111111111111111111"
		st.RecordNotified(ctx, taken, 1, 4)
		st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: taken, Name: "[Best] Show - 04 [720p].mkv", AnimeID: 1, EpKey: "4",
			TotalSize: 1 << 29, Manual: c.manual})
		if c.started {
			st.SavePlayback(ctx, store.PlaybackState{AnimeID: 1, EpKey: "4", Position: 300, Duration: 1400, Played: 10})
		}
		w := watcherWith(st,
			release(taken, "[Best] Show - 04 [720p].mkv", 500),
			release("3333333333333333333333333333333333333333", "[Other] Show - 04 [1080p].mkv", 900),
		).WithDownloader(NewDownloader(st, nil, nil, discard()))
		w.Poll(ctx)
		if queued, _ := st.QueuedDownloads(ctx); len(queued) != 0 {
			t.Errorf("%s: queued %+v over the taken copy", c.name, queued)
		}
	}
}

// A hold nothing will settle (auto-download turned off) is let go, not listed forever.
func TestAHoldEndsWhenAutoDownloadIsTurnedOff(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 5, time.Now().Add(time.Hour))
	ctx := context.Background()
	st.SetAnimePref(ctx, 1, "autodownload.enabled", "true")
	st.SetAnimePref(ctx, 1, "autodownload.wait_hours", "6")
	st.SetSetting(ctx, "release.prefer_groups", `["Best"]`)
	idx := &fixedIndexer{results: []indexer.Torrent{release(deadHash, "[Other] Show - 04 [1080p].mkv", 900)}}
	w := NewWatcher(st, NewFinder(st, idx, discard()), idx, discard()).WithDownloader(NewDownloader(st, nil, nil, discard()))
	w.Poll(ctx)
	if waits, _ := st.Waits(ctx, 0); len(waits) != 1 {
		t.Fatalf("waits = %+v, want episode 4 held", waits)
	}
	st.SetAnimePref(ctx, 1, "autodownload.enabled", "false")
	w.Poll(ctx)
	if waits, _ := st.Waits(ctx, 0); len(waits) != 0 {
		t.Errorf("the hold outlived auto-download: %+v", waits)
	}
}

// Notify me says why it's on, and off means off whatever turned it on.
func TestNotifyMeFollowsTheWatchsRules(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	setAiring(t, st, 5, time.Now().Add(time.Hour))
	if f, _ := st.FollowOf(ctx, 1); !f.On || f.Because != "watching" {
		t.Fatalf("a show on Watching: %+v", f)
	}
	if err := st.SetNotify(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if f, _ := st.FollowOf(ctx, 1); f.On {
		t.Fatalf("turned off, yet %+v", f)
	}
	follows, _ := st.Follows(ctx)
	for _, f := range follows {
		if f.AnimeID == 1 {
			t.Fatal("the release watch still follows a show turned off")
		}
	}
	if err := st.SetNotify(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if f, _ := st.FollowOf(ctx, 1); !f.On || f.Because != "followed" {
		t.Fatalf("turned on again: %+v", f)
	}
}

// Preview says what auto-download would take and why, and takes, records and queues nothing.
func TestPreviewTakesNothing(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 5, time.Now().Add(time.Hour))
	ctx := context.Background()
	w := watcherWith(st, release(goodHash, "[Best] Show - 04 [1080p].mkv", 500)).WithDownloader(NewDownloader(st, nil, nil, discard()))
	if rows, err := w.Preview(ctx); err != nil || len(rows) != 0 {
		t.Fatalf("rows %+v, %v; no show has auto-download on", rows, err)
	}

	st.SetAnimePref(ctx, 1, "autodownload.enabled", "true")
	rows, err := w.Preview(ctx)
	if err != nil || len(rows) != 1 || rows[0].State != "download" || rows[0].Episode != 4 || rows[0].Release == "" {
		t.Fatalf("rows %+v, %v; want episode 4 to download", rows, err)
	}
	st.SetAnimePref(ctx, 1, "autodownload.wait_hours", "3")
	st.SetSetting(ctx, "release.prefer_groups", `["Other"]`)
	if rows, _ := w.Preview(ctx); rows[0].State != "wait" || !strings.Contains(rows[0].Reason, "Other at 1080p") {
		t.Fatalf("rows %+v; want a wait for the preferred group", rows)
	}

	if queued, _ := st.QueuedDownloads(ctx); len(queued) != 0 || st.EpisodeFetched(ctx, 1, 4) || len(notifications(t, st)) != 0 {
		t.Fatal("the preview took, recorded or announced something")
	}
	if waits, _ := st.Waits(ctx, 0); len(waits) != 0 {
		t.Fatalf("the preview started a wait: %+v", waits)
	}
}

// Auto-download turned on for a show far behind: the latest comes, the backlog is left to Download all.
func TestCatchUpLeavesABacklogAlone(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 10, time.Now().Add(time.Hour))
	ctx := context.Background()
	if err := st.SetAnimePref(ctx, 1, "autodownload.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	var releases []indexer.Torrent
	for ep := 4; ep <= 9; ep++ {
		releases = append(releases, release(fmt.Sprintf("%040d", ep), fmt.Sprintf("[Best] Show - %02d [1080p].mkv", ep), 500))
	}
	w := watcherWith(st, releases...).WithDownloader(NewDownloader(st, nil, nil, discard()))
	for range 2 {
		w.Poll(ctx)
	}
	queued, err := st.QueuedDownloads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || queued[0].Episode != 9 {
		t.Errorf("queued %+v, want episode 9 alone", queued)
	}
}

// The viewer caught up past what has aired: nothing to say until the next episode airs.
func TestWatcherIsQuietWhenTheViewerIsUpToDate(t *testing.T) {
	st := prefetchStore(t)
	setAiring(t, st, 5, time.Now().Add(time.Hour))
	ctx := context.Background()
	if _, err := st.MarkWatched(ctx, 1, 4); err != nil {
		t.Fatal(err)
	}
	w := watcherWith(st, release(goodHash, "[Best] Show - 04 [1080p].mkv", 500), release(deadHash, "[Best] Show - 05 [1080p].mkv", 500))
	if rep, _ := w.Poll(ctx); rep.Notified != 0 || len(notifications(t, st)) != 0 {
		t.Fatalf("announced an episode already watched or not yet aired: %+v", rep)
	}
}

func finishedShow(t *testing.T, ended time.Time, progress int) *store.Store {
	t.Helper()
	st := prefetchStore(t)
	episodes, current := 12, "CURRENT"
	if _, err := st.ImportList(context.Background(),
		[]store.Anime{{ID: 1, Romaji: "Show", Synonyms: "[]", Genres: "[]", Episodes: &episodes, EndDate: ended.Format("2006-01-02")}},
		[]store.Entry{{ID: 11, AnimeID: 1, Status: &current, Progress: progress}}, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	return st
}

// A show that finished long ago has nothing new, however far behind the viewer is.
func TestWatcherIsSilentAboutAShowFinishedLongAgo(t *testing.T) {
	st := finishedShow(t, time.Now().AddDate(0, 0, -30), 3)
	ctx := context.Background()
	w := watcherWith(st, release(goodHash, "[Best] Show - 04 [1080p].mkv", 500), release(deadHash, "[Best] Show - 12 [1080p].mkv", 500))
	for range 3 {
		if rep, _ := w.Poll(ctx); rep.Notified != 0 || len(notifications(t, st)) != 0 {
			t.Fatalf("an old episode was announced: %+v", rep)
		}
	}
}

// The finale: the catalogue drops the airing schedule once a show ends, but its last episode is still news.
func TestWatcherAnnouncesTheFinaleOfAShowThatJustEnded(t *testing.T) {
	st := finishedShow(t, time.Now().AddDate(0, 0, -1), 11)
	ctx := context.Background()
	w := watcherWith(st, release(goodHash, "[Best] Show - 12 [1080p].mkv", 500))
	rep, _ := w.Poll(ctx)
	if got := notifications(t, st); rep.Notified != 1 || len(got) != 1 || episodeOf(got[0]) != 12 {
		t.Fatalf("report = %+v, notifications = %+v; want the finale announced", rep, got)
	}
}

// A show muted on its own page is left alone while the rest are announced.
func TestWatcherSkipsAShowMutedOnItsPage(t *testing.T) {
	st := watchedShow(t, time.Now().Add(-time.Hour))
	ctx := context.Background()
	if err := st.SetAnimePref(ctx, 1, "notify.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	w := watcherWith(st, release(goodHash, "[Best] Show - 04 [1080p].mkv", 500))
	if rep, _ := w.Poll(ctx); rep.Checked != 0 || len(notifications(t, st)) != 0 {
		t.Fatalf("a muted show was checked: %+v", rep)
	}
}

func TestWatcherIsQuietWhenNotificationsAreOff(t *testing.T) {
	st := watchedShow(t, time.Now().Add(-time.Hour))
	ctx := context.Background()
	if err := st.SetSetting(ctx, "notify.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	w := watcherWith(st, release(goodHash, "[Best] Show - 04 [1080p].mkv", 500))
	if rep, _ := w.Poll(ctx); rep.Checked != 0 || len(notifications(t, st)) != 0 {
		t.Fatalf("notifications off, yet %+v", rep)
	}
}
