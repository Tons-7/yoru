package library

import (
	"context"
	"slices"
	"strings"
	"testing"

	"yoru/internal/score"
	"yoru/internal/store"
)

func recordPack(t *testing.T, st *store.Store, hash string, eps ...string) {
	t.Helper()
	for i, ep := range eps {
		if err := st.RecordTorrent(context.Background(), store.TorrentRecord{InfoHash: hash, EngineID: 50,
			Name: "[Pack] Show (01-12) [1080p][Batch]", AnimeID: 1, EpKey: ep,
			FilePath: "Show - 0" + ep + ".mkv", FileIndex: i + 2, TotalSize: 100}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAnEpisodePausedInsideAPackKeepsItsPlaceInLine(t *testing.T) {
	d, st, _ := workerQueueWith(t)
	ctx := context.Background()
	recordPack(t, st, otherHash, "2", "3")
	d.SetUserPaused(FileKey(otherHash, 2), true)
	if _, err := st.Enqueue(ctx, 1, 1, []int{2, 3}); err != nil {
		t.Fatal(err)
	}

	d.step(ctx)

	rows := queued(t, st)
	if len(rows) != 2 || rows[0].Episode != 2 || rows[0].State != "paused" {
		t.Fatalf("queue %+v; want episode 2 kept first in line, paused", rows)
	}
	if next, ok, _ := st.NextQueued(ctx); !ok || next.Episode != 3 {
		t.Errorf("the queue should move on to episode 3, took %+v", next)
	}
}

func TestAStreamedPackGetsNoPlaceInLine(t *testing.T) {
	_, st, _ := workerQueueWith(t)
	ctx := context.Background()
	recordPack(t, st, otherHash, "2", "3")
	if n, err := st.SetReleaseQueuedPaused(ctx, otherHash, -1, true); err != nil || n != 0 {
		t.Fatalf("placed %d, %v; a pack only streamed is not a queued download", n, err)
	}
	if rows := queued(t, st); len(rows) != 0 {
		t.Errorf("queue %+v", rows)
	}
}

func TestRemovingADownloadForgetsItsLineAndPauses(t *testing.T) {
	d, st, _ := workerQueueWith(t)
	ctx := context.Background()
	recordPack(t, st, otherHash, "2", "3")
	if _, err := st.KeepDownload(ctx, otherHash, true); err != nil {
		t.Fatal(err)
	}
	d.SetUserPaused(otherHash, true)
	d.SetUserPaused(FileKey(otherHash, 2), true)
	if _, err := st.SetReleaseQueuedPaused(ctx, otherHash, -1, true); err != nil {
		t.Fatal(err)
	}
	episodes, err := st.ReleaseEpisodes(ctx, otherHash, -1)
	if err != nil || len(episodes) != 2 {
		t.Fatalf("episodes %v, %v", episodes, err)
	}

	d.Forget(ctx, otherHash, -1, episodes)

	if rows := queued(t, st); len(rows) != 0 {
		t.Errorf("queue %+v; the removed download's episodes are still in line", rows)
	}
	if keys, _ := st.HandPaused(ctx); len(keys) != 0 || d.PausedByHand(otherHash) || d.PausedByHand(FileKey(otherHash, 2)) {
		t.Errorf("pauses still remembered: %v; a later copy of this release would count as paused", keys)
	}
}

func TestASlowDownloadIsNeverSwappedForALowerResolution(t *testing.T) {
	ctx := context.Background()
	d, _, _ := workerQueueWith(t,
		release(otherHash, "[Sharp] Show - 02 [1080p].mkv", 6),
		release(thirdHash, "[Small] Show - 02 [720p].mkv", 60),
	)
	if d.prefetch.alternative(ctx, 1, 2, 1, score.DefaultPreferences(), otherHash) {
		t.Error("a 720p counted as a replacement for a crawling 1080p")
	}
}

func TestPausingAPackKeepsItsUnfinishedEpisodesInLine(t *testing.T) {
	_, st, _ := workerQueueWith(t)
	ctx := context.Background()
	recordPack(t, st, otherHash, "2", "3")
	if _, err := st.KeepDownload(ctx, otherHash, true); err != nil {
		t.Fatal(err)
	}
	if n, err := st.SetReleaseQueuedPaused(ctx, otherHash, -1, true); err != nil || n != 2 {
		t.Fatalf("paused %d, %v; want both unfinished episodes placed in line", n, err)
	}
	rows := queued(t, st)
	if len(rows) != 2 || rows[0].State != "paused" || rows[1].State != "paused" || rows[0].InfoHash != otherHash {
		t.Fatalf("queue %+v", rows)
	}
	if n, err := st.SetReleaseQueuedPaused(ctx, otherHash, 3, false); err != nil || n != 1 {
		t.Fatalf("resumed %d, %v; want only episode 3", n, err)
	}
	rows = queued(t, st)
	if rows[0].State != "paused" || rows[1].State != "pending" {
		t.Errorf("queue %+v; resuming one episode resumes only it", rows)
	}
}

func TestEpisodesMoveUpAndDownTheLine(t *testing.T) {
	_, st, _ := workerQueueWith(t)
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{2, 3, 4, 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetQueuedPaused(ctx, 1, "3", true); err != nil {
		t.Fatal(err)
	}
	order := func() []int {
		var out []int
		for _, q := range queued(t, st) {
			out = append(out, q.Episode)
		}
		return out
	}
	steps := []struct {
		ep, to string
		want   []int
	}{
		{"4", "up", []int{2, 4, 3, 5}},
		{"2", "bottom", []int{4, 3, 5, 2}},
		{"3", "down", []int{4, 5, 3, 2}},
		{"2", "top", []int{2, 4, 5, 3}},
	}
	for _, s := range steps {
		if moved, err := st.MoveQueued(ctx, 1, s.ep, s.to); err != nil || !moved {
			t.Fatalf("move %s %s: %v %v", s.ep, s.to, moved, err)
		}
		if got := order(); !slices.Equal(got, s.want) {
			t.Fatalf("after %s %s: %v, want %v", s.ep, s.to, got, s.want)
		}
	}
	if moved, _ := st.MoveQueued(ctx, 1, "2", "up"); moved {
		t.Error("the first in line moved further up")
	}
	if moved, err := st.MoveQueuedNear(ctx, 1, "3", 1, "4", false); err != nil || !moved {
		t.Fatalf("drop 3 before 4: %v %v", moved, err)
	}
	if got := order(); !slices.Equal(got, []int{2, 3, 4, 5}) {
		t.Fatalf("after dropping 3 before 4: %v", got)
	}
	if moved, err := st.MoveQueuedNear(ctx, 1, "2", 1, "5", true); err != nil || !moved {
		t.Fatalf("drop 2 after 5: %v %v", moved, err)
	}
	if got := order(); !slices.Equal(got, []int{3, 4, 5, 2}) {
		t.Fatalf("after dropping 2 after 5: %v", got)
	}
	if moved, _ := st.MoveQueuedNear(ctx, 1, "3", 1, "4", false); moved {
		t.Error("dropping 3 where it already is counted as a move")
	}
	if next, _, _ := st.NextQueued(ctx); next.Episode != 4 {
		t.Errorf("the queue took %d; 3 is first but paused, so 4 goes next", next.Episode)
	}
}

func TestDownloadNowLiftsAPauseByHand(t *testing.T) {
	d, st, _ := workerQueueWith(t)
	ctx := context.Background()
	recordPack(t, st, otherHash, "2", "3")
	key := FileKey(otherHash, 2)
	d.SetUserPaused(key, true)
	if _, err := st.Enqueue(ctx, 1, 1, []int{2}); err != nil {
		t.Fatal(err)
	}
	d.step(ctx)

	if moved, err := d.Now(ctx, 1, "2"); err != nil || !moved {
		t.Fatalf("moved %v, err %v", moved, err)
	}
	if d.prefetch.pausedByUser(key) || d.userPausedHash(key) {
		t.Error("the episode asked for now is still held as paused; the queue would postpone it again")
	}
	if hashes, _ := st.HandPaused(ctx); len(hashes) != 0 {
		t.Errorf("the pause is still remembered for the next start: %v", hashes)
	}
	if rows := queued(t, st); len(rows) != 1 || rows[0].State != "pending" || rows[0].RetryAt != 0 {
		t.Errorf("queue %+v; want it first in line, no longer paused", rows)
	}
}

func TestRemovingOneEpisodeOfAPackKeepsTheOthers(t *testing.T) {
	_, st, _ := workerQueueWith(t)
	ctx := context.Background()
	recordPack(t, st, otherHash, "2", "3")
	c := NewCache(st, nil, t.TempDir(), discard())

	if _, err := c.RemoveFile(ctx, otherHash, 2); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.TorrentForEpisode(ctx, 1, "2"); ok {
		t.Error("episode 2 is still recorded after removing it")
	}
	if rec, ok, _ := st.TorrentForEpisode(ctx, 1, "3"); !ok || !strings.EqualFold(rec.InfoHash, otherHash) {
		t.Errorf("episode 3 went with episode 2: %+v", rec)
	}
	if parts, _ := st.DownloadParts(ctx); len(parts[otherHash]) != 1 || parts[otherHash][0].EpKey != "3" {
		t.Errorf("the list still shows %+v", parts[otherHash])
	}
}
