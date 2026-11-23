package store

import (
	"testing"
	"time"
)

func TestAiredKnowsOnlyTheNextEpisode(t *testing.T) {
	const next, at = 7, int64(1_000_000)
	for _, tt := range []struct {
		episode int
		now     int64
		want    bool
	}{
		{6, at - 1, true},
		{7, at - 1, false},
		{7, at, true},
		{8, at + 3600, false},
		{8, at + 7*86400, false},
	} {
		if got := aired(tt.episode, next, at, tt.now); got != tt.want {
			t.Errorf("aired(%d) at %+d = %v, want %v", tt.episode, tt.now-at, got, tt.want)
		}
	}
	if !aired(3, 0, 0, at) {
		t.Error("with no schedule at all, nothing can be held back")
	}
}

func TestLatestIsTheEpisodeThatAiredLast(t *testing.T) {
	now := time.Now()
	day := func(d int) string { return now.AddDate(0, 0, d).Format("2006-01-02") }
	for _, tt := range []struct {
		name string
		f    Follow
		want int
	}{
		{"next airs tomorrow", Follow{NextEpisode: 6, NextAiringAt: now.Unix() + 3600}, 5},
		{"next's time has passed", Follow{NextEpisode: 6, NextAiringAt: now.Unix() - 60}, 6},
		{"next known, time unknown", Follow{NextEpisode: 6}, 5},
		{"first episode still to air", Follow{NextEpisode: 1, NextAiringAt: now.Unix() + 3600}, 0},
		{"ended yesterday", Follow{Episodes: 12, EndDate: day(-1)}, 12},
		{"ended today", Follow{Episodes: 12, EndDate: day(0)}, 12},
		{"ended last month", Follow{Episodes: 12, EndDate: day(-30)}, 0},
		{"planned end next month, no schedule", Follow{Episodes: 12, EndDate: day(30)}, 0},
		{"planned end in two days", Follow{Episodes: 12, EndDate: day(2)}, 0},
		{"no airing data", Follow{Episodes: 12}, 0},
		{"end date unreadable", Follow{Episodes: 12, EndDate: "2024"}, 0},
	} {
		if got := tt.f.Latest(now.Unix()); got != tt.want {
			t.Errorf("%s: Latest = %d, want %d", tt.name, got, tt.want)
		}
	}
}
