package parse

import "testing"

// Names the parser used to misread: the episode title after the number, a span of the marked form, a
// number that is the show's name, and Chinese groups' bracketed numbering.
func TestEpisodeTitleAfterTheNumberIsNotTheShow(t *testing.T) {
	for name, want := range map[string]string{
		"[CBM]_Monster_-_02_-_Downfall_[78722558].mkv":                     "Monster",
		"[Group] Show - S01E05 - The Title [1080p].mkv":                    "Show",
		"[Group] Show - E05 - The Title [1080p].mkv":                       "Show",
		"[nekotan] Garden S01E01 Chapter 1: Thanatos (BD 1080p) | Kara no": "Garden | Kara no",
		"[Group] Steins;Gate 0 - 03 [1080p].mkv":                           "Steins;Gate 0",
	} {
		if got := Parse(name); got.Title != want {
			t.Errorf("%s: title %q, want %q", name, got.Title, want)
		}
	}
}

// Names that came from real sites and were read wrong: what each must give.
func TestAuditedShapes(t *testing.T) {
	type want struct {
		season, episode, end, version, part int
		title                               string
	}
	for name, w := range map[string]want{
		"[HatSubs] One Piece 1170 (WEB 1080p) [803A61D5].mkv":             {0, 1170, 0, 0, 0, "One Piece"},
		"[Kawaiika-Raws] Show 05 (BDRip 1920x1080 HEVC FLAC)":             {0, 5, 0, 0, 0, "Show"},
		"[Group] your name. 2016 (1080p BD)":                              {0, 0, 0, 0, 0, "your name. 2016"},
		"[Group] Mobile Suit Gundam 0083 (BD 1080p)":                      {0, 0, 0, 0, 0, "Mobile Suit Gundam 0083"},
		"[Group] Monogatari Series (2009-2019) [BD 1080p HEVC]":           {0, 0, 0, 0, 0, "Monogatari Series"},
		"[Group] Show [EP01-EP12][1080p]":                                 {0, 1, 12, 0, 0, "Show"},
		"[Group] Show (E01-E24) [1080p]":                                  {0, 1, 24, 0, 0, "Show"},
		"[Group] Show Ep 01-12 [1080p]":                                   {0, 1, 12, 0, 0, "Show"},
		"[DKB] Show - S01E06v2 [1080p][HEVC x265 10bit][Multi-Subs]":      {1, 6, 0, 2, 0, "Show"},
		"[realpinkgirl321] Bocchi the Rock! Season 1 v3 (BD Remux 1080p)": {1, 0, 0, 0, 0, "Bocchi the Rock!"},
		"[Recluse] Bocchi the Rock! - Season 01v3 (S01) (BD 1080p HEVC)":  {1, 0, 0, 0, 0, "Bocchi the Rock!"},
		"[Koten_Gars] Bocchi the Rock! {v2} [JP.BD][Hi10][1080p][FLAC]":   {0, 0, 0, 0, 0, "Bocchi the Rock!"},
		"[Group] Sousou no Frieren - 01-28 + OVA [1080p]":                 {0, 1, 28, 0, 0, "Sousou no Frieren"},
		"[Group] Show - 05 - The Final Battle (Part 1) [1080p]":           {0, 5, 0, 0, 0, "Show"},
		"[Group] Show Part 2 - 17 [1080p]":                                {0, 17, 0, 0, 2, "Show Part 2"},
	} {
		got := Parse(name)
		if got.Season != w.season || got.Episode != w.episode || got.EpisodeEnd != w.end ||
			got.Version != w.version || got.Part != w.part || got.Title != w.title {
			t.Errorf("%s:\n got  S%d E%d-%d v%d part %d %q\n want S%d E%d-%d v%d part %d %q", name,
				got.Season, got.Episode, got.EpisodeEnd, got.Version, got.Part, got.Title,
				w.season, w.episode, w.end, w.version, w.part, w.title)
		}
	}
	if got := Parse("[Koten_Gars] Bocchi the Rock! {v2} [JP.BD][Hi10][1080p][FLAC]"); got.BitDepth != 10 || got.Codec != "H264" {
		t.Errorf("Hi10: bit depth %d codec %q", got.BitDepth, got.Codec)
	}
}

func TestMarkedSpanIsAPack(t *testing.T) {
	for _, name := range []string{"[Group] Show S01E01-E12 [1080p HEVC]", "Show.S01E01-12.1080p.WEB-DL-GRP"} {
		got := Parse(name)
		if got.Season != 1 || got.Episode != 1 || got.EpisodeEnd != 12 || !got.Batch {
			t.Errorf("%s: %+v", name, got)
		}
		if got.Title != "Show" {
			t.Errorf("%s: title %q", name, got.Title)
		}
	}
	if got := Parse("[Group] Show S01E05 [1080p]"); got.Episode != 5 || got.EpisodeEnd != 0 || got.Batch {
		t.Errorf("single: %+v", got)
	}
}

func TestNumberBeforeABracketIsTheName(t *testing.T) {
	for name, want := range map[string]struct {
		episode int
		title   string
	}{
		"[Group] Mob Psycho 100 [BD 1080p HEVC]": {0, "Mob Psycho 100"},
		"[Group] Cyborg 009 [BD 1080p]":          {0, "Cyborg 009"},
		"[Group] Show 08 [1080p]":                {8, "Show"},
		"[Group] Show 08 1080p":                  {8, "Show"},
		"[Group] Show 100 1080p":                 {100, "Show"},
	} {
		got := Parse(name)
		if got.Episode != want.episode || got.Title != want.title {
			t.Errorf("%s: episode %d title %q, want %d %q", name, got.Episode, got.Title, want.episode, want.title)
		}
	}
}

func TestBracketedNumberIsTheEpisode(t *testing.T) {
	for name, want := range map[string]struct {
		episode, version int
		title            string
	}{
		"[Nekomoe kissaten][Sousou no Frieren][05][1080p][JPSC]":   {5, 0, "Sousou no Frieren"},
		"[Sakurato] Sousou no Frieren [05v2][AVC-8bit 1080p][CHT]": {5, 2, "Sousou no Frieren"},
		"[VCB-Studio] Show [05][Ma10p_1080p][x265_flac].mkv":       {5, 0, "Show"},
		"[Group] Show (2024) [1080p]":                              {0, 0, "Show"},
	} {
		got := Parse(name)
		if got.Episode != want.episode || got.Version != want.version || got.Title != want.title {
			t.Errorf("%s: episode %d v%d title %q, want %d v%d %q", name, got.Episode, got.Version, got.Title,
				want.episode, want.version, want.title)
		}
	}
}

func TestBaseName(t *testing.T) {
	for in, want := range map[string]string{
		"Show/Show - 01 [1080p].mkv": "Show - 01 [1080p]",
		`Show\Show - 01.mkv`:         "Show - 01",
		"Show - 01.mkv":              "Show - 01",
		"noextension":                "noextension",
	} {
		if got := BaseName(in); got != want {
			t.Errorf("BaseName(%q) = %q, want %q", in, got, want)
		}
	}
	for _, name := range []string{"a.mkv", "b.OGM", "c.m4v", "d.wmv", "e.m2ts"} {
		if !IsVideo(name) {
			t.Errorf("%s is a video", name)
		}
	}
	if IsVideo("x.ass") || IsVideo("y.jpg") {
		t.Error("subtitles and artwork are not videos")
	}
}
