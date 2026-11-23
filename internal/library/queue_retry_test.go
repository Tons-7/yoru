package library

import (
	"context"
	"strings"
	"testing"
	"time"

	"yoru/internal/torrent"
)

func quickQueue(t *testing.T, retries int) {
	t.Helper()
	idle, stall, tries, delay, warm, poll := queueIdle, queueStall, queueRetries, queueRetryDelay, prefetchWarmCap, torrent.AwaitPoll
	queueIdle, queueStall, queueRetries, queueRetryDelay, prefetchWarmCap, torrent.AwaitPoll =
		20*time.Millisecond, 10*time.Millisecond, retries, 200*time.Millisecond, 60*time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() {
		queueIdle, queueStall, queueRetries, queueRetryDelay, prefetchWarmCap, torrent.AwaitPoll = idle, stall, tries, delay, warm, poll
	})
}

func TestWorkerTriesADeadSwarmAgainBeforeGivingUp(t *testing.T) {
	quickQueue(t, 2)
	d, st, engine := workerQueueWith(t, release(otherHash, "[Group] Show - 02 [1080p].mkv", 500))
	engine.stuck[otherHash] = true
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{2}); err != nil {
		t.Fatal(err)
	}

	var seen []string
	runUntil(t, d, func() bool {
		items := queued(t, st)
		if len(items) != 1 {
			return false
		}
		q := items[0]
		line := q.State
		if q.Error != nil {
			line += " " + *q.Error
		}
		if len(seen) == 0 || seen[len(seen)-1] != line {
			seen = append(seen, line)
		}
		return q.State == "failed"
	})

	last := seen[len(seen)-1]
	if !strings.HasPrefix(last, "failed gave up after 3 tries: no peers sent anything") {
		t.Errorf("final row %q, want it to say it gave up after three tries and why", last)
	}
	var waited bool
	for _, line := range seen {
		if strings.HasPrefix(line, "pending the seeders never answered") {
			waited = true
		}
	}
	if !waited {
		t.Errorf("rows seen %q, want a wait saying the seeders never answered", seen)
	}

	rec, ok, err := st.TorrentForEpisode(ctx, 1, "2")
	if err != nil || !ok || !strings.EqualFold(rec.InfoHash, otherHash) {
		t.Errorf("episode 2's torrent = %+v %v %v, want the dead release recorded for the later try", rec, ok, err)
	}
	if !engine.isPausedHash(otherHash) {
		t.Error("the dead release was left fetching between tries")
	}
}

func (f *fakeEngine) isPausedHash(hash string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, h := range f.ids {
		if h == hash {
			return f.paused[id]
		}
	}
	return false
}

func TestWorkerSaysAStallInWords(t *testing.T) {
	quickQueue(t, 0)
	d, st, _ := workerQueueWith(t, release(otherHash, "[Group] Show - 02 [1080p].mkv", 500))
	ctx := context.Background()
	if _, err := st.Enqueue(ctx, 1, 1, []int{2}); err != nil {
		t.Fatal(err)
	}

	runUntil(t, d, func() bool {
		items := queued(t, st)
		return len(items) == 1 && items[0].State == "failed"
	})
	failed := queued(t, st)[0]
	if failed.Error == nil || !strings.HasPrefix(*failed.Error, "no peers sent anything for 1 minute; 0% is on disk") {
		t.Errorf("failed row says %v, want the stall in words", failed.Error)
	}
	if strings.Contains(*failed.Error, "gave up") {
		t.Errorf("failed row %q counts tries though there was only one", *failed.Error)
	}
}
