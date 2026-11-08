package torrent

import (
	"fmt"
	"strings"
	"testing"
)

func seasonFolders(folders ...string) []File {
	var files []File
	for _, folder := range folders {
		for ep := 1; ep <= 12; ep++ {
			files = append(files, File{Name: fmt.Sprintf("%s/[G] Show - %02d [1080p].mkv", folder, ep), Length: 1 << 30})
		}
	}
	return files
}

func TestAMultiSeasonPackPlaysTheSeasonAsked(t *testing.T) {
	files := seasonFolders("Show S01", "Show S02", "Show S03")
	for season := 1; season <= 3; season++ {
		f, _, ok := PickInPack(files, 3, []int{3}, Scope{Season: season})
		if !ok || !strings.Contains(f.Name, fmt.Sprintf("S0%d/", season)) {
			t.Errorf("season %d picked %q", season, f.Name)
		}
	}
}

func TestTheFirstSeasonIsTheFolderNamingNoSeason(t *testing.T) {
	files := seasonFolders("Haikyuu!!", "Haikyuu!! Second Season", "Haikyuu!! 3rd Season")
	f, _, ok := PickInPack(files, 3, []int{3}, Scope{Season: 1})
	if !ok || !strings.HasPrefix(f.Name, "Haikyuu!!/") {
		t.Errorf("season 1 picked %q", f.Name)
	}
	f, _, ok = PickInPack(files, 3, []int{3}, Scope{Season: 2})
	if !ok || !strings.HasPrefix(f.Name, "Haikyuu!! Second Season/") {
		t.Errorf("season 2 picked %q", f.Name)
	}
}

func TestAnotherEntryInThePackIsLeftAlone(t *testing.T) {
	files := seasonFolders("Steins;Gate", "Steins;Gate Zero")
	f, _, ok := PickInPack(files, 3, []int{3}, Scope{Season: 1, Others: []string{"zero"}})
	if !ok || !strings.HasPrefix(f.Name, "Steins;Gate/") {
		t.Errorf("picked %q, want the first series' episode", f.Name)
	}
}

func TestTwoShowsTheScopeCannotTellApartAreRefused(t *testing.T) {
	var files []File
	for ep := 1; ep <= 12; ep++ {
		files = append(files,
			File{Name: fmt.Sprintf("Steins;Gate/[G] Steins;Gate - %02d [1080p].mkv", ep), Length: 1 << 30},
			File{Name: fmt.Sprintf("Steins;Gate 0/[G] Steins;Gate 0 - %02d [1080p].mkv", ep), Length: 1 << 30})
	}
	if f, _, ok := PickInPack(files, 3, []int{3}, Scope{Season: 1}); ok {
		t.Errorf("guessed %q between two shows' episode 3", f.Name)
	}
}

func TestTwoCutsOfTheSameEpisodePlayTheLargest(t *testing.T) {
	files := []File{
		{Name: "Show/TV/Show - 03.mkv", Length: 1 << 30},
		{Name: "Show/Uncensored/Show - 03.mkv", Length: 2 << 30},
		{Name: "Show/TV/Show - 04.mkv", Length: 1 << 30},
		{Name: "Show/Uncensored/Show - 04.mkv", Length: 2 << 30},
	}
	for _, scope := range []Scope{{}, {Season: 1}} {
		if f, _, ok := PickInPack(files, 3, []int{3}, scope); !ok || !strings.Contains(f.Name, "Uncensored") {
			t.Errorf("scope %+v picked %q (%v), want the larger cut", scope, f.Name, ok)
		}
	}
	if f, _, ok := PickInPack(files, 3, []int{3}, Scope{Season: 2}); ok {
		t.Errorf("a season 2 request took %q from a pack that states no season", f.Name)
	}
}

func TestASequelCountedThroughTheFranchiseFindsItsSeasonFolder(t *testing.T) {
	files := seasonFolders("Dr. Stone S01", "Dr. Stone S02", "Dr. Stone S03")
	f, _, ok := PickInPack(files, 5, []int{5}, Scope{Season: 1, Counted: 3})
	if !ok || !strings.Contains(f.Name, "S03/") {
		t.Errorf("picked %q (%v), want the third season's episode", f.Name, ok)
	}
	only := []File{{Name: "Dr.Stone.S03/Dr.Stone.S03E05.1080p.mkv", Length: 1 << 30}, {Name: "Dr.Stone.S03/Dr.Stone.S03E06.1080p.mkv", Length: 1 << 30}}
	if f, _, ok := PickInPack(only, 5, []int{5}, Scope{Season: 1, Counted: 3}); !ok || !strings.Contains(f.Name, "S03E05") {
		t.Errorf("a season pack of the counted season picked %q (%v)", f.Name, ok)
	}
}

func TestASeasonThenAnEpisodeIsNotARange(t *testing.T) {
	for name, want := range map[string]int{
		"[SubsPlease] Mushoku Tensei S2 - 05 (1080p).mkv": 2,
		"Show Season 2 - 05.mkv":                          2,
		"Show S01-S03/Show - 05.mkv":                      0,
		"Show Seasons 1-3/Show - 05.mkv":                  0,
		"Show Season 1-3/Show - 05.mkv":                   0,
	} {
		if got := pathSeason(name); got != want {
			t.Errorf("%q: season %d, want %d", name, got, want)
		}
	}
	files := []File{
		{Name: "Show - 05 (1080p).mkv", Length: 2 << 30},
		{Name: "Show S2 - 05 (1080p).mkv", Length: 1 << 30},
	}
	if f, _, ok := PickInPack(files, 5, []int{5}, Scope{Season: 2}); !ok || !strings.Contains(f.Name, "S2") {
		t.Errorf("a season 2 request picked %q (%v)", f.Name, ok)
	}
}

func TestTheInnermostSeasonCounts(t *testing.T) {
	var files []File
	for s := 1; s <= 3; s++ {
		for ep := 1; ep <= 12; ep++ {
			files = append(files, File{Name: fmt.Sprintf("Show S01-S03/Season %d/Show - %02d.mkv", s, ep), Length: 1 << 30})
		}
	}
	for season := 1; season <= 3; season++ {
		if f, _, ok := PickInPack(files, 3, []int{3}, Scope{Season: season}); !ok || !strings.Contains(f.Name, fmt.Sprintf("Season %d/", season)) {
			t.Errorf("season %d picked %q (%v)", season, f.Name, ok)
		}
	}
}

func TestAPackNamedForItsMoviesStillPlaysItsEpisodes(t *testing.T) {
	root := "Show + Movies + OVAs [BD][1080p]/"
	files := []File{
		{Name: root + "Show - 04.mkv", Length: 1 << 30},
		{Name: root + "Show - 05.mkv", Length: 1 << 30},
		{Name: root + "Movies/Show Movie - 05.mkv", Length: 3 << 30},
	}
	others := []string{"movie", "movies", "ova", "ovas"}
	if f, _, ok := PickInPack(files, 5, []int{5}, Scope{Season: 1, Others: others}); !ok || f.Name != root+"Show - 05.mkv" {
		t.Errorf("picked %q (%v), want the episode the pack's own name does not rule out", f.Name, ok)
	}
}

func TestAnEpisodeTitleIsNotAnotherEntrysName(t *testing.T) {
	files := []File{
		{Name: "Demon Slayer S01/Demon Slayer - S01E23 - Master of the Mansion.mkv", Length: 1 << 30},
		{Name: "Demon Slayer S01/Demon Slayer - S01E24 - Rehabilitation Training.mkv", Length: 1 << 30},
		{Name: "Demon Slayer S01/Demon Slayer - S01E25 - Tsuguko, Kanao Tsuyuri.mkv", Length: 1 << 30},
	}
	f, _, ok := PickInPack(files, 24, []int{24}, Scope{Season: 1, Others: []string{"hashira", "training", "movie", "special"}})
	if !ok || !strings.Contains(f.Name, "S01E24") {
		t.Errorf("picked %q (%v); 'Training' in the episode title is not the Hashira Training entry", f.Name, ok)
	}
	movie := append(files, File{Name: "Demon Slayer Movie/Demon Slayer Movie - 24.mkv", Length: 3 << 30})
	if f, _, ok := PickInPack(movie, 24, []int{24}, Scope{Season: 1, Others: []string{"movie"}}); !ok || strings.Contains(f.Name, "Movie") {
		t.Errorf("picked %q (%v), want the episode, not the film", f.Name, ok)
	}
}

func TestQualityFoldersStillPickTheLargest(t *testing.T) {
	files := []File{
		{Name: "Show [1080p]/Show - 03.mkv", Length: 2 << 30},
		{Name: "Show [720p]/Show - 03.mkv", Length: 1 << 30},
		{Name: "Show [1080p]/Show - 04.mkv", Length: 2 << 30},
		{Name: "Show [720p]/Show - 04.mkv", Length: 1 << 30},
	}
	f, _, ok := PickInPack(files, 3, []int{3}, Scope{Season: 1})
	if !ok || !strings.Contains(f.Name, "1080p") {
		t.Errorf("picked %q, want the 1080p copy", f.Name)
	}
}

func TestOneSeasonPackIsUnchanged(t *testing.T) {
	files := seasonFolders("Show")
	f, _, ok := PickNumbered(files, 3, []int{3})
	if !ok || !strings.Contains(f.Name, "- 03 ") {
		t.Errorf("picked %q", f.Name)
	}
}

func TestPictureDramaPackPlaysEachEpisode(t *testing.T) {
	var files []File
	for ep := 1; ep <= 6; ep++ {
		files = append(files, File{Name: fmt.Sprintf("[Kametsu] Princess Principal - Picture Dramas (BD 1080p Hi10 FLAC) [Dual-Audio]/[Kametsu] Princess Principal Picture Drama - %02d (BD 1080p Hi10 FLAC) [%08X].mkv", ep, ep*0x1111), Length: 170 << 20})
	}
	for ep := 1; ep <= 6; ep++ {
		f, _, ok := PickInPack(files, ep, []int{ep}, Scope{Season: 1, Others: []string{"crown", "handler"}})
		if !ok || !strings.Contains(f.Name, fmt.Sprintf("Drama - %02d ", ep)) {
			t.Errorf("episode %d picked %q, %v", ep, f.Name, ok)
		}
	}
}
