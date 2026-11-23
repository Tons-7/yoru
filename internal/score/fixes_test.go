package score

import (
	"testing"

	"yoru/internal/parse"
)

func TestFixesOnlyAFixOfTheSameRelease(t *testing.T) {
	held := parse.Parse("[G] Show - 05 [1080p].mkv")
	for _, c := range []struct {
		title string
		want  bool
	}{
		{"[G] Show - 05v2 [1080p].mkv", true},
		{"[g] Show - 05v2 [1080p].mkv", true},
		{"[G] Show - 05 [1080p].mkv", false},
		{"[G] Show - 06v2 [1080p].mkv", false},
		{"[G] Show (01-12) [1080p][Batch]", false},
		{"Show - 05v2 [1080p].mkv", false},
		{"[G] Show - 05v2 [720p].mkv", false},
	} {
		r := Result{Candidate: candidate(c.title, 10, 1<<30)}
		if got := Fixes(r, held); got != c.want {
			t.Errorf("%s fixes the held copy: %v, want %v", c.title, got, c.want)
		}
	}
}

func TestBetterWhenTheHeldReleaseIsNoLongerListed(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.PreferGroups = []string{"Pref"}
	ranked := Rank([]Candidate{candidate("[Pref] Show - 05 [1080p].mkv", 50, 1<<30)}, prefs)
	if len(ranked) == 0 {
		t.Fatal("nothing ranked")
	}
	best := ranked[0]
	if !Better(best, "gone", parse.Parse("[Other] Show - 05 [1080p].mkv"), ranked, prefs) {
		t.Error("the preferred group is not better than an unlisted copy from another group")
	}
	if Better(best, "gone", parse.Parse("[Pref] Show - 05 [1080p].mkv"), ranked, prefs) {
		t.Error("an unlisted copy that is already ideal was called worse")
	}
}
