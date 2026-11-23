package parse

import "testing"

func TestReleaseEdges(t *testing.T) {
	cases := []struct {
		name            string
		season, episode int
		version         int
		resolution      string
		group           string
	}{
		{name: "[Group] Show 1x05 [1080p].mkv", season: 1, episode: 5, resolution: "1080p", group: "Group"},
		{name: "Show.2x12.720p-Team.mkv", season: 2, episode: 12, resolution: "720p", group: "Team"},
		{name: "[Group] Show - 05 [v2][1080p].mkv", episode: 5, version: 2, resolution: "1080p", group: "Group"},
		{name: "[Group] Show - 05 [4K].mkv", episode: 5, resolution: "2160p", group: "Group"},
		{name: "[Group] Show 01 (1920x1080).mkv", episode: 1, resolution: "1080p", group: "Group"},
		{name: "Show Name (2024) - S01E05", season: 1, episode: 5},
		{name: "Show Name - 05 1080p WEB-DL", episode: 5, resolution: "1080p"},
	}
	for _, c := range cases {
		r := Parse(c.name)
		if r.Season != c.season || r.Episode != c.episode || r.Version != c.version || r.Resolution != c.resolution || r.Group != c.group {
			t.Errorf("%s: S%d E%d v%d %s group %q", c.name, r.Season, r.Episode, r.Version, r.Resolution, r.Group)
		}
	}
}
