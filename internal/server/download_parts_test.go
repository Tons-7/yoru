package server

import (
	"testing"

	"yoru/internal/store"
)

func TestEachEpisodeOfAPackReportsItsOwnProgress(t *testing.T) {
	parts := []store.DownloadPart{
		{EpKey: "3", FileIndex: 13, Size: 300},
		{EpKey: "5", FileIndex: 15, Size: 300},
		{EpKey: "8", FileIndex: 18, Size: 400},
	}
	files := make([]int64, 20)
	files[13], files[15], files[18] = 300, 300, 40

	included := make([]bool, 20)
	included[13], included[15] = true, true

	got := liveParts(parts, files, included, false)
	if !got[0].Complete || !got[1].Complete || got[2].Complete || got[2].Done != 40 {
		t.Fatalf("parts %+v; want 3 and 5 done, 8 at 40 of 400", got)
	}
	if got[0].Paused || !got[2].Paused {
		t.Errorf("parts %+v; episode 8 is paused on its own, the finished ones are not", got)
	}

	stored := []store.DownloadPart{{EpKey: "1", FileIndex: 0, Size: 100, Complete: true}, {EpKey: "2", FileIndex: 1, Size: 100}}
	got = liveParts(stored, nil, nil, false)
	if got[0].Done != 100 || got[1].Done != 0 || got[1].Complete || got[1].Paused {
		t.Errorf("not in the engine: %+v; want the stored state", got)
	}
	if got = liveParts(stored, nil, nil, true); !got[1].Complete {
		t.Errorf("a finished download left an episode unfinished: %+v", got)
	}

	files = []int64{100, 30}
	got = liveParts([]store.DownloadPart{{EpKey: "2", FileIndex: 0, Size: 100}, {EpKey: "3", FileIndex: 1, Size: 100}},
		files, []bool{true, false}, true)
	if !got[0].Complete || got[1].Complete || got[1].Done != 30 || !got[1].Paused {
		t.Errorf("parts %+v; a paused episode in a pack whose other files are done is still 30%% and paused", got)
	}
}
