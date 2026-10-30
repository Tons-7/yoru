package library

import (
	"testing"
	"time"

	"yoru/internal/store"
)

func TestCheckedAtIsPerEpisode(t *testing.T) {
	w := &Watcher{}
	if _, ok := w.CheckedAt(1, 5); ok {
		t.Fatal("an episode never searched has a time")
	}
	before := time.Now()
	w.searched(1, 5)
	at, ok := w.CheckedAt(1, 5)
	if !ok || at.Before(before) {
		t.Errorf("checked at %v, %v; want a time from now", at, ok)
	}
	if _, ok := w.CheckedAt(1, 6); ok {
		t.Error("searching episode 5 marked episode 6 checked")
	}
}

func TestSearchTimesForgetShowsNoLongerWatched(t *testing.T) {
	w := &Watcher{}
	w.searched(1, 5)
	w.searched(2, 3)
	w.searchedAt[[2]int{1, 4}] = time.Now().Add(-searchedFor - time.Hour)
	w.pruneSearched([]store.Follow{{AnimeID: 1}})
	if _, ok := w.CheckedAt(1, 5); !ok {
		t.Error("a followed show's recent search was dropped")
	}
	if _, ok := w.CheckedAt(1, 4); ok {
		t.Error("a week-old search was kept")
	}
	if _, ok := w.CheckedAt(2, 3); ok {
		t.Error("a show no longer followed kept its search time")
	}
}

func TestCoverageSearchesWhatPollSearches(t *testing.T) {
	notify := Coverage{Latest: 8, Progress: 5}
	if !notify.Searches(8) || notify.Searches(7) || notify.Searches(6) {
		t.Error("with alerts only, the latest episode alone is searched")
	}
	auto := Coverage{Latest: 8, Progress: 5, First: 6, Auto: true}
	if !auto.Searches(7) || auto.Searches(6) || !auto.Searches(8) {
		t.Error("auto-download catches up from after the first recorded episode")
	}
	if (Coverage{Latest: 8, Progress: 8}).Searches(8) {
		t.Error("a watched latest episode is not searched")
	}
	if !(Coverage{Latest: 8, Progress: 8, Wanted: 8}).Searches(8) {
		t.Error("an episode asked for from the player is searched")
	}
	if (Coverage{Latest: 8, Progress: 5, Wanted: 9}).Searches(9) {
		t.Error("an episode asked for is searched only once it is out")
	}
}
