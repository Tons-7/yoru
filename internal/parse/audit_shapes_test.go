package parse

import "testing"

func TestAuditShapes(t *testing.T) {
	for _, c := range []struct {
		name                       string
		title                      string
		season, seasonEnd, ep, end int
		batch, special             bool
	}{
		{"[Group] Attack on Titan Episode 05 [1080p].mkv", "Attack on Titan", 0, 0, 5, 0, false, false},
		{"Show.Name.Episode.12.1080p.WEB-DL", "Show Name", 0, 0, 12, 0, false, false},
		{"[Group] Show Name - 05 [1080p] - Episode Title", "Show Name", 0, 0, 5, 0, false, false},
		{"Bocchi the Rock S01 1080p AMZN WEB-DL DDP2.0 H 264 MULTi-VARYG (Bocchi the Rock!, Multi-Audio, Multi-Subs)", "Bocchi the Rock", 1, 0, 0, 0, true, false},
		{"[Group] Show S01 [1080p H 265 HEVC]", "Show", 1, 0, 0, 0, true, false},
		{"[chasa] Bocchi the Rock! S00E01 (11.5) (1080p Bluray FLAC x265 HEVC)", "Bocchi the Rock!", 0, 0, 1, 0, false, true},
		{"[Group] Re Zero S01E00 [1080p]", "Re Zero", 1, 0, 0, 0, false, true},
		{"[Judas] Shingeki no Kyojin (Season 1-3) [BD 1080p]", "Shingeki no Kyojin", 1, 3, 0, 0, true, false},
		{"[Group] Show Seasons 1-4 [Batch]", "Show", 1, 4, 0, 0, true, false},
		{"Show S01-S03 1080p", "Show", 1, 3, 0, 0, true, false},
		{"[Group] Show S1+S2 [1080p]", "Show", 1, 2, 0, 0, true, false},
		{"Show - S01E05 - 1000 Years Later.mkv", "Show", 1, 0, 5, 0, false, false},
		{"[Group] Show - 05 - 100 Days Later [1080p]", "Show", 0, 0, 5, 0, false, false},
		{"[Group] Show Name S2 05 [1080p].mkv", "Show Name", 2, 0, 5, 0, false, false},
		{"[Group] Uchuu Senkan Yamato 2199 [BD 1080p][Batch]", "Uchuu Senkan Yamato 2199", 0, 0, 0, 0, true, false},
		{"[Group] Golgo 13 [1080p][Batch]", "Golgo 13", 0, 0, 0, 0, true, false},
		{"[Group] Show S01E01-12 [1080p]", "Show", 1, 0, 1, 12, true, false},
		{"[neoDESU] Frieren - Beyond Journey's End [Season 2] [BD 1080p AV1 OPUS AAC] [Dual Audio] Sousou no Frieren", "Frieren - Beyond Journey's End | Sousou no Frieren", 2, 0, 0, 0, true, false},
		{"[Good] Show (01-12) [1080p][WEB-DL][English Sub][Batch]", "Show", 0, 0, 1, 12, true, false},
		{"[Group] Show 1st Season - 05 [1080p]", "Show", 1, 0, 5, 0, false, false},
		{"[Group] Show - 05 - Incomplete Memories [1080p]", "Show", 0, 0, 5, 0, false, false},
	} {
		r := Parse(c.name)
		if r.Title != c.title || r.Season != c.season || r.SeasonEnd != c.seasonEnd || r.Episode != c.ep ||
			r.EpisodeEnd != c.end || r.Batch != c.batch || r.Special != c.special {
			t.Errorf("%s\n got  title=%q s=%d-%d e=%d-%d batch=%v special=%v\n want title=%q s=%d-%d e=%d-%d batch=%v special=%v",
				c.name, r.Title, r.Season, r.SeasonEnd, r.Episode, r.EpisodeEnd, r.Batch, r.Special,
				c.title, c.season, c.seasonEnd, c.ep, c.end, c.batch, c.special)
		}
	}
}
