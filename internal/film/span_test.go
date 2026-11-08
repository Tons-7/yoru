package film

import "testing"

func TestAChapterRangeIsASpanNotOneChapter(t *testing.T) {
	for name, want := range map[string]int{
		"[Anime-FLAC] Princess Principal - Crown Handler Movies (Chapter 1-4) (BD 1080p) (Batch) (Ongoing)": 0,
		"Show Chapter 1~6 [BD]": 0,
		"[Techmod] Princess Principal Crown Handler - Movie 4 [1080p]": 4,
		"Show Chapter 4 - 01 [1080p].mkv":                              4,
		"Show Movie 4 - 1080p":                                         4,
	} {
		if got, _ := Number(name); got != want {
			t.Errorf("%q = %d, want %d", name, got, want)
		}
	}
}

var (
	chapter1 = Entry{Titles: []string{"Princess Principal: Crown Handler - Chapter 1", "Princess Principal Movie 1"}}
	busyEasy = Entry{Titles: []string{"Princess Principal: Crown Handler - Chapter 1: BUSY EASY MONEY", "Princess Principal OVA"}, Extra: true}
	chapter2 = Entry{Titles: []string{"Princess Principal: Crown Handler - Chapter 2"}}
	reviews  = Entry{Titles: []string{"Princess Principal: Crown Handler - Chapter 2: Revealing Reviews"}, Extra: true}
)

func TestASideStorySharingTheChapterNumberNeedsItsOwnName(t *testing.T) {
	ovaKey := New(busyEasy, []Entry{chapter1, chapter2, reviews})
	movieKey := New(chapter1, []Entry{busyEasy, chapter2, reviews})

	for name, want := range map[string][2]Verdict{
		"[Erai-raws] Princess Principal - Crown Handler - 01 [1080p][7D25ADF1].mkv":          {Other, Ours},
		"[Moozzi2] Princess Principal Crown Handler Vol.01 [SP09] OVA Busy Easy Money":       {Ours, Other},
		"[Anime Chap] Princess Principal - Crown Handler 1 OVA 1 [BD 1080p].mkv":             {Ours, Other},
		"[Moozzi2] Princess Principal Crown Handler Vol.01 (BD 1920x1080 x265-10Bit 4Audio)": {Unknown, Unknown},
		"[SubsPlease] Princess Principal - Crown Handler - 02 (1080p) [41FDC70B].mkv":        {Other, Other},
		"Princess Principal Crown Handler Movie 1 Special Edition [1080p]":                   {Other, Ours},
		"Princess Principal Crown Handler Chapter 1 [BD 1080p + Extras]":                     {Other, Ours},
		"Princess Principal Crown Handler - 01 [BD 1080p] + Bonus":                           {Other, Ours},
	} {
		if got := ovaKey.Match(name); got != want[0] {
			t.Errorf("side story, %q = %v, want %v", name, got, want[0])
		}
		if got := movieKey.Match(name); got != want[1] {
			t.Errorf("film, %q = %v, want %v", name, got, want[1])
		}
	}
}

func TestAFilmKeepsItsNumberWhenAVolumeSharesIt(t *testing.T) {
	movie := Entry{Titles: []string{"Show Movie 1: First Light"}}
	volume := Entry{Titles: []string{"Show: Vol. 1"}, Extra: true}
	if got := New(movie, []Entry{volume}).Match("[Group] Show Movie 1 [1080p].mkv"); got != Ours {
		t.Errorf("the film's own numbered release = %v", got)
	}
}

func TestTheFilmAndItsSideStoryPickTheirOwnFiles(t *testing.T) {
	ovaKey := New(busyEasy, []Entry{chapter1, chapter2, reviews})
	movieKey := New(chapter1, []Entry{busyEasy, chapter2, reviews})

	pekomux := []File{
		{Name: "[Pekomux] Princess Principal - S00E08 - Crown Handler Chapter 1 [DD645D8F].mkv", Length: 9 << 30},
		{Name: "[Pekomux] Princess Principal - S00E09 - Crown Handler Chapter 2 [2E574C24].mkv", Length: 9 << 30},
		{Name: "[Pekomux] Princess Principal - S00E10 - BUSY EASY MONEY [AE1B00C4].mkv", Length: 1 << 30},
		{Name: "[Pekomux] Princess Principal - S00E11 - Revealing Reviews [819DE009].mkv", Length: 1 << 30},
	}
	if i, ok := ovaKey.Pick(pekomux); !ok || i != 2 {
		t.Errorf("the side story picked %d, %v", i, ok)
	}
	if i, ok := movieKey.Pick(pekomux); !ok || i != 0 {
		t.Errorf("the film picked %d, %v", i, ok)
	}
	flac := []File{
		{Name: "[Anime-FLAC] Princess Principal Crown Handler Chapter 1 - Busy Easy Money (BD 1920x1080 x264 FLAC).mkv", Length: 1 << 30},
		{Name: "[Anime-FLAC] Princess Principal Crown Handler Chapter 2 - Revealing Reviews (BD 1920x1080 x264 FLAC).mkv", Length: 1 << 30},
	}
	if i, ok := ovaKey.Pick(flac); !ok || i != 0 {
		t.Errorf("the side story picked %d, %v in the specials batch", i, ok)
	}
	if _, ok := movieKey.Pick(flac); ok {
		t.Error("the film took a side story from the specials batch")
	}
}
