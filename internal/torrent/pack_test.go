package torrent

import (
	"strings"
	"testing"
)

func mb(n int64) int64 { return n << 20 }

func TestEpisodeInNameReadsBracketsAndMarkedForms(t *testing.T) {
	for name, want := range map[string]int{
		"[VCB-Studio] Shingeki no Kyojin [05][Ma10p_1080p][x265_flac]": 5,
		"[Nekomoe kissaten][Show][05][1080p][JPSC]":                    5,
		"[Sakurato] Show [05v2][AVC-8bit 1080p AAC][CHS]":              5,
		"[Group] Show EP05 [1080p]":                                    5,
		"[Group] Show E05 [1080p]":                                     5,
		"Show.S02.E05.1080p":                                           5,
		"Show.EP05.2160p.WEB-DL":                                       5,
		"[Group] Show (2024) [1080p]":                                  0,
		"Show - 12.5":                                                  0,
		"Show - 12":                                                    12,
	} {
		if got := episodeInName(name); got != want {
			t.Errorf("%q = %d, want %d", name, got, want)
		}
	}
}

func TestBracketNumberedPackAndLoneFile(t *testing.T) {
	var pack []File
	for n := 1; n <= 12; n++ {
		pack = append(pack, File{Name: "[VCB-Studio] Show [" + twoDigits(n) + "][Ma10p_1080p][x265_flac].mkv", Length: mb(900)})
	}
	got, _, ok := PickEpisode(pack, 5)
	if !ok || got.Name != pack[4].Name {
		t.Fatalf("picked %q, %v", got.Name, ok)
	}
	lone := []File{{Name: "[Nekomoe kissaten] Show [05][1080p].mkv", Length: mb(900)}}
	if _, _, ok := PickEpisode(lone, 1); ok {
		t.Error("episode 5's file was taken as episode 1")
	}
	if _, _, ok := PickEpisode(lone, 5); !ok {
		t.Error("episode 5's own file was refused")
	}
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func TestExtrasAreNeverTheEpisode(t *testing.T) {
	files := []File{
		{Name: "Show/Extras/[G] Show - NCOP 01 [1080p].mkv", Length: mb(90)},
		{Name: "Show/Extras/[G] Show - NCED 01 [1080p].mkv", Length: mb(90)},
		{Name: "Show/Extras/[G] Show - PV 01 [1080p].mkv", Length: mb(40)},
		{Name: "Show/Extras/[G] Show - PV 02 [1080p].mkv", Length: mb(40)},
		{Name: "Show/Extras/[G] Show - CM 01 [1080p].mkv", Length: mb(20)},
		{Name: "Show/Extras/[G] Show - Menu 01 [1080p].mkv", Length: mb(30)},
		{Name: "Show/[G] Show - 01 [1080p].mkv", Length: mb(1400)},
		{Name: "Show/[G] Show - 01 [1080p] (preview).mkv", Length: mb(300)},
		{Name: "Show/[G] Show - 02 [1080p].mkv", Length: mb(1400)},
		{Name: "Show/SPs/[G] Show - SP01 [1080p].mkv", Length: mb(600)},
	}
	got, _, ok := PickEpisode(files, 1)
	if !ok || got.Name != "Show/[G] Show - 01 [1080p].mkv" {
		t.Fatalf("picked %q, %v", got.Name, ok)
	}

	if _, _, ok := PickEpisode(files[:6], 1); ok {
		t.Error("an opening was taken as episode 1")
	}
}

func TestRecapIsNotTheEpisode(t *testing.T) {
	files := []File{
		{Name: "Show - 11.mkv", Length: mb(700)},
		{Name: "Show - 12.5.mkv", Length: mb(700)},
		{Name: "Show - 12.mkv", Length: mb(700)},
	}
	got, _, ok := PickEpisode(files, 12)
	if !ok || got.Name != "Show - 12.mkv" {
		t.Fatalf("picked %q, %v", got.Name, ok)
	}
}

func TestPackNumberingIsReadOffItsFiles(t *testing.T) {
	numbered := func(from, to int) []File {
		var out []File
		for n := from; n <= to; n++ {
			out = append(out, File{Name: "[G] Mushoku Tensei - " + twoDigits(n) + " [1080p].mkv", Length: mb(1000)})
		}
		return out
	}

	if got, _, ok := PickNumbered(numbered(12, 23), 12, []int{12, 23}); !ok || got.Name != "[G] Mushoku Tensei - 23 [1080p].mkv" {
		t.Errorf("season count: picked %q, %v", got.Name, ok)
	}
	if got, _, ok := PickNumbered(numbered(1, 12), 12, []int{12, 23}); !ok || got.Name != "[G] Mushoku Tensei - 12 [1080p].mkv" {
		t.Errorf("own count: picked %q, %v", got.Name, ok)
	}

	if got, _, ok := PickNumbered(numbered(7, 12), 8, []int{8, 19}); !ok || got.Name != "[G] Mushoku Tensei - 08 [1080p].mkv" {
		t.Errorf("partial pack: picked %q, %v", got.Name, ok)
	}

	if got, _, ok := PickNumbered(numbered(12, 23), 12, []int{23}); !ok || got.Name != "[G] Mushoku Tensei - 23 [1080p].mkv" {
		t.Errorf("bare later cour: picked %q, %v", got.Name, ok)
	}
}

func TestOtherVideoContainersCount(t *testing.T) {
	files := []File{{Name: "Show - 01.ogm", Length: mb(300)}, {Name: "Show - 02.m4v", Length: mb(300)}}
	if got, _, ok := PickEpisode(files, 2); !ok || got.Name != "Show - 02.m4v" {
		t.Errorf("picked %q, %v", got.Name, ok)
	}
}

func TestAPackNumberedInDiscOrderIsMatchedByItsEpisodeTitles(t *testing.T) {
	names := []string{
		"Season 1/[Sav1or] Princess Principal - S01E07 - case16 Loudly Laundry [BD][1080p][AV1][OPUS][Dual Audio].mkv",
		"Season 1/[Sav1or] Princess Principal - S01E08 - case11 Pell-mell Duel [BD][1080p][AV1][OPUS][Dual Audio].mkv",
		"Season 1/[Sav1or] Princess Principal - S01E09 - case20 Ripper Dipper [BD][1080p][AV1][OPUS][Dual Audio].mkv",
		"Season 1/[Sav1or] Princess Principal - S01E10 - case22 Comfort Comrade [BD][1080p][AV1][OPUS][Dual Audio].mkv",
	}
	files := make([]File, len(names))
	for i, name := range names {
		files[i] = File{Name: name, Length: 700 << 20}
	}
	broadcast := map[int][]string{
		7:  {"Loudly Laundry", "case16 Loudly Laundry"},
		8:  {"Ripper Dipper", "case20 Ripper Dipper"},
		9:  {"Pell-mell Duel", "case11 Pell-mell Duel"},
		10: {"Comfort Comrade", "case22 Comfort Comrade"},
	}
	for episode, want := range map[int]string{7: "Loudly Laundry", 8: "Ripper Dipper", 9: "Pell-mell Duel", 10: "Comfort Comrade"} {
		f, _, ok := PickInPack(files, episode, []int{episode}, Scope{Season: 1, Named: broadcast})
		if !ok || !strings.Contains(f.Name, want) {
			t.Errorf("episode %d played %q, want the file titled %q", episode, f.Name, want)
		}
	}

	for episode, want := range map[int]string{8: "S01E08", 9: "S01E09"} {
		if f, _, ok := PickInPack(files, episode, []int{episode}, Scope{Season: 1}); !ok || !strings.Contains(f.Name, want) {
			t.Errorf("with no titles known, episode %d played %q, want the file numbered %s", episode, f.Name, want)
		}
	}

	plain := []File{
		{Name: "[Group] Show - 08 [1080p].mkv", Length: 700 << 20},
		{Name: "[Group] Show - 09 [1080p].mkv", Length: 700 << 20},
	}
	if f, _, ok := PickInPack(plain, 8, []int{8}, Scope{Named: broadcast}); !ok || !strings.Contains(f.Name, "- 08") {
		t.Errorf("files that carry no titles: episode 8 played %q, want the one numbered 08", f.Name)
	}

	show := map[int][]string{8: {"Princess Principal"}, 9: {"Pell-mell Duel"}}
	if f, _, ok := PickInPack(files, 8, []int{8}, Scope{Season: 1, Named: show}); !ok || !strings.Contains(f.Name, "S01E08") {
		t.Errorf("an episode titled like the show itself: episode 8 played %q, want the file numbered S01E08", f.Name)
	}

	half := []File{
		{Name: "Show - S01E08 - Pell-mell Duel.mkv", Length: 700 << 20},
		{Name: "Show - S01E09.mkv", Length: 700 << 20},
	}
	if f, _, ok := PickInPack(half, 8, []int{8}, Scope{Season: 1, Named: broadcast}); !ok || !strings.Contains(f.Name, "S01E08") {
		t.Errorf("no file is titled for episode 8: played %q, want the one numbered S01E08", f.Name)
	}
}

func TestATitleEndingInANumberDoesNotMatchALongerNumber(t *testing.T) {
	for _, c := range []struct {
		name, key string
		want      bool
	}{
		{"showepisode20", "episode2", false},
		{"showepisode2", "episode2", true},
		{"showepisode2final", "episode2", true},
		{"show12episode2", "2episode2", false},
		{"pellmellduel1080p", "pellmellduel", true},
		{"episode21andepisode2", "episode2", true},
	} {
		if got := containsTitle(c.name, c.key); got != c.want {
			t.Errorf("containsTitle(%q, %q) = %v, want %v", c.name, c.key, got, c.want)
		}
	}
}
