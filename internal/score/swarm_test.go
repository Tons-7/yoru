package score

import (
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/parse"
)

func seeded(title string, size int64, seeders int) Candidate {
	return Candidate{
		Torrent:   indexer.Torrent{Title: title, Size: size, Seeders: seeders, SeedersKnown: true},
		Release:   parse.Parse(title),
		Confirmed: true,
	}
}

func TestSwarmDecidesBetweenEqualReleases(t *testing.T) {
	prefs := DefaultPreferences()
	thin := seeded("[GroupA] Show - 07 [1080p].mkv", 1<<30, 3)
	healthy := seeded("[GroupB] Show - 07 [1080p].mkv", 1<<30, 60)

	ranked := Rank([]Candidate{thin, healthy}, prefs)
	if ranked[0].Torrent.Seeders != 60 {
		t.Errorf("picked the %d-seeder release over the 60", ranked[0].Torrent.Seeders)
	}
}

func TestSwarmNeverBuysALowerResolution(t *testing.T) {
	prefs := DefaultPreferences()
	small := seeded("[GroupA] Show - 07 [720p].mkv", 700<<20, 900)
	big := seeded("[GroupB] Show - 07 [1080p].mkv", 1<<30, 6)

	ranked := Rank([]Candidate{small, big}, prefs)
	if ranked[0].Release.Resolution != "1080p" {
		t.Errorf("a %d-seeder 720p outranked the 1080p", small.Torrent.Seeders)
	}
}

func TestAWellSeededBatchReplacesADyingEpisode(t *testing.T) {
	prefs := DefaultPreferences()
	single := seeded("[GroupA] Show - 07 [1080p].mkv", 1<<30, 4)
	batch := seeded("[GroupB] Show (01-170) [1080p][Batch]", 200<<30, 300)
	batch.TotalEpisodes = 170

	ranked := Rank([]Candidate{batch, single}, prefs)
	if !ranked[0].Release.Batch {
		t.Errorf("the 4-seeder episode won over a 300-seeder batch: %s", ranked[0].Torrent.Title)
	}
}

func TestAHealthyEpisodeStillBeatsABiggerBatch(t *testing.T) {
	prefs := DefaultPreferences()
	single := seeded("[GroupA] Show - 07 [1080p].mkv", 1<<30, 12)
	batch := seeded("[GroupB] Show (01-170) [1080p][Batch]", 200<<30, 300)
	batch.TotalEpisodes = 170

	ranked := Rank([]Candidate{batch, single}, prefs)
	if ranked[0].Release.Batch {
		t.Errorf("a 12-seeder episode lost to the batch: %s", ranked[0].Torrent.Title)
	}
}

func TestADoubledSwarmOfATenSeederPackBeatsAFiveSeederEpisode(t *testing.T) {
	prefs := DefaultPreferences()
	single := seeded("[HorribleSubs] Show - 03 [1080p].mkv", 800<<20, 5)
	pack := seeded("[HorribleSubs] Show (01-12) [1080p] (Unofficial Batch)", 8<<30, 10)
	pack.TotalEpisodes = 12

	if ranked := Rank([]Candidate{single, pack}, prefs); !ranked[0].Release.Batch {
		t.Errorf("the 5-seeder episode beat the 10-seeder pack: %s", ranked[0].Torrent.Title)
	}
	close := seeded("[G] Show (01-12) [1080p][Batch]", 8<<30, 7)
	close.TotalEpisodes = 12
	if ranked := Rank([]Candidate{close, single}, prefs); ranked[0].Release.Batch {
		t.Errorf("a pack only two seeders ahead replaced the episode: %s", ranked[0].Torrent.Title)
	}
}

func TestSizeLimitFollowsRuntime(t *testing.T) {
	prefs := DefaultPreferences()
	film := seeded("[Group] The Garden of Sinners 1 [BD 1080p].mkv", 3400<<20, 42)

	if _, ok := Best([]Candidate{film}, prefs); ok {
		t.Error("a 3.4 GB file passed the 3 GiB cap with no runtime known")
	}

	film.RuntimeMinutes = 50
	best, ok := Best([]Candidate{film}, prefs)
	if !ok {
		t.Fatalf("a 50-minute film was still refused: %v", first(Rank([]Candidate{film}, prefs)).Blocked)
	}
	if best.Torrent.Size != 3400<<20 {
		t.Errorf("picked %d bytes", best.Torrent.Size)
	}

	episode := seeded("[Group] Show - 07 [1080p].mkv", 3400<<20, 42)
	episode.RuntimeMinutes = 24
	if _, ok := Best([]Candidate{episode}, prefs); ok {
		t.Error("an oversized episode passed on a film's allowance")
	}
}

func first(rs []Result) Result {
	if len(rs) == 0 {
		return Result{}
	}
	return rs[0]
}

func TestSubtitleLabelNeverBeatsResolution(t *testing.T) {
	prefs := DefaultPreferences()
	plain := seeded("[SubsPlease] Show - 05 (1080p) [ABCD1234].mkv", 1<<30, 50)
	labelled := seeded("[Erai-raws] Show - 05 [720p][Multiple Subtitle] [ENG]", 700<<20, 50)
	low := seeded("[Erai-raws] Show - 05 [480p][Multiple Subtitle] [ENG]", 300<<20, 50)

	ranked := Rank([]Candidate{labelled, low, plain}, prefs)
	if ranked[0].Release.Resolution != "1080p" {
		t.Fatalf("picked %s; a label outranked a resolution", ranked[0].Torrent.Title)
	}
}

func TestPickDoesNotDependOnOrder(t *testing.T) {
	prefs := DefaultPreferences()
	a := seeded("[G1] Show - 05 [1080p WEB-DL AVC AAC].mkv", 1<<30, 20)
	b := seeded("[G2] Show - 05 [BD 1080p HEVC 10bit FLAC].mkv", 2<<30, 20)
	c := seeded("[G3] Show - 05 [720p][Multi-Subs].mkv", 700<<20, 20)
	d := seeded("[G4] Show (01-12) [1080p][Batch]", 9<<30, 8)
	d.TotalEpisodes = 12
	e := seeded("[G5] Show - 05 [1080p].mkv", 1<<30, 3)

	f := seeded("[G6] Show + Movies [BD][1080p]", 6<<30, 40)
	f.Confirmed = false
	f.TotalEpisodes = 12
	g := seeded("[G7] Show - 05 [720p].mkv", 700<<20, 0)
	g.Torrent.SeedersKnown = false

	h := seeded("[G8] Show [BD][1080p][Dual Audio]", 6<<30, 12)
	h.Confirmed, h.TotalEpisodes, h.Held = false, 12, true

	sets := [][]Candidate{{a, b, c}, {a, d, e}, {b, d, e, c}, {e, f, g, c}, {f, g, e, d}, {h, f, e, a}, {h, d, g, c}}
	for _, set := range sets {
		var want string
		for i, order := range permutations(set) {
			got := Rank(order, prefs)[0].Torrent.Title
			if i == 0 {
				want = got
			} else if got != want {
				t.Fatalf("order %d picked %q, order 0 picked %q", i, got, want)
			}
		}
	}
}

func permutations(in []Candidate) [][]Candidate {
	if len(in) <= 1 {
		return [][]Candidate{in}
	}
	var out [][]Candidate
	for i := range in {
		rest := append(append([]Candidate{}, in[:i]...), in[i+1:]...)
		for _, p := range permutations(rest) {
			out = append(out, append([]Candidate{in[i]}, p...))
		}
	}
	return out
}

func TestUnlabelledEpisodeLosesToALabelledPack(t *testing.T) {
	prefs := DefaultPreferences()
	avi := Candidate{
		Torrent:   indexer.Torrent{Title: "[Animanda] Death Note - 12 [400AF72B].avi"},
		Release:   parse.Parse("[Animanda] Death Note - 12 [400AF72B].avi"),
		Confirmed: true,
	}
	pack := seeded("[AnimeRG] Death Note Bluray The Complete Series [1080p] [Dual Audio] [HEVC]", 40<<30, 25)
	pack.Confirmed = false
	pack.TotalEpisodes = 37

	ranked := Rank([]Candidate{avi, pack}, prefs)
	if ranked[0].Release.Extension == "avi" {
		t.Errorf("the unlabelled .avi won: %s", ranked[0].Torrent.Title)
	}
}

func TestAnUncountedEpisodeGivesWayToAWellSeededPack(t *testing.T) {
	prefs := DefaultPreferences()
	single := seeded("[Erai-raws] Show - 03 [1080p].mkv", 1<<30, 0)
	single.Torrent.SeedersKnown = false
	pack := seeded("[GroupB] Show + Movies [BD][1080p][Dual Audio]", 6<<30, 42)
	pack.Confirmed = false
	pack.TotalEpisodes = 12
	if ranked := Rank([]Candidate{single, pack}, prefs); ranked[0].Torrent.Seeders != 42 {
		t.Errorf("an episode with no seeder count beat a 42-seeder pack: %s", ranked[0].Torrent.Title)
	}

	thin := seeded("[GroupC] Show (01-12) [1080p][Batch]", 8<<30, 8)
	thin.TotalEpisodes = 12
	if ranked := Rank([]Candidate{thin, single}, prefs); ranked[0].Torrent.Seeders == 8 {
		t.Errorf("an 8-seeder pack is no evidence against an uncounted episode: %s", ranked[0].Torrent.Title)
	}
}

func TestAWellSeededSeriesPackReplacesADyingLabelledEpisode(t *testing.T) {
	prefs := DefaultPreferences()
	single := seeded("[GroupA] Show - 07 [1080p].mkv", 1<<30, 5)
	pack := seeded("[GroupB] Show + Movies + OVAs [BD][1080p][Dual Audio]", 6<<30, 42)
	pack.Confirmed = false
	pack.TotalEpisodes = 12

	ranked := Rank([]Candidate{single, pack}, prefs)
	if ranked[0].Torrent.Seeders != 42 {
		t.Errorf("the 5-seeder episode beat the 42-seeder pack: %s", ranked[0].Torrent.Title)
	}
}

func TestAPackIsOnlyLiftedWhenNoConfirmedCopyIsHealthy(t *testing.T) {
	prefs := DefaultPreferences()
	dying := seeded("[GroupA] Show - 03 [1080p].mkv", 1<<30, 3)
	healthy := seeded("[GroupB] Show - 01 ~ 26 [1080p][Batch]", 30<<30, 50)
	healthy.TotalEpisodes = 26
	pack := seeded("[GroupC] Show [1080p][BD][x265][10-bit][Dual-Audio]", 20<<30, 17)
	pack.Confirmed = false
	pack.TotalEpisodes = 26

	ranked := Rank([]Candidate{dying, pack, healthy}, prefs)
	if ranked[0].Torrent.Seeders != 50 {
		t.Errorf("picked %s; a healthy confirmed batch was there", ranked[0].Torrent.Title)
	}
}

func TestTheNextEpisodeComesFromAPackAlreadyHeld(t *testing.T) {
	prefs := DefaultPreferences()
	batch := seeded("[HorribleSubs] Show (01-12) [1080p] (Unofficial Batch)", 8<<30, 11)
	batch.TotalEpisodes = 12
	held := seeded("[Sav1or] Show + Movies + OVA [BD][1080p][Dual Audio]", 20<<30, 43)
	held.Confirmed, held.TotalEpisodes, held.Held = false, 12, true

	if ranked := Rank([]Candidate{batch, held}, prefs); !ranked[0].Held {
		t.Errorf("picked %s; the pack already downloading for the show holds the episode", ranked[0].Torrent.Title)
	}

	dying := held
	dying.Torrent.Seeders = 3
	healthy := seeded("[GroupA] Show - 05 [1080p].mkv", 1<<30, 50)
	if ranked := Rank([]Candidate{dying, healthy}, prefs); ranked[0].Held {
		t.Errorf("a dying held pack beat a 50-seeder episode: %s", ranked[0].Torrent.Title)
	}

	low := seeded("[GroupB] Show [720p][Batch]", 6<<30, 80)
	low.Confirmed, low.TotalEpisodes, low.Held = false, 12, true
	if ranked := Rank([]Candidate{low, batch}, prefs); ranked[0].Held {
		t.Errorf("a held 720p pack beat a 1080p release: %s", ranked[0].Torrent.Title)
	}
	unnamed := seeded("[GroupD] Show [BD][1080p]", 9<<30, 40)
	unnamed.Confirmed, unnamed.TotalEpisodes = false, 12
	if ranked := Rank([]Candidate{low, unnamed}, prefs); ranked[0].Held {
		t.Errorf("a held 720p pack beat a 1080p pack neither names the episode in: %s", ranked[0].Torrent.Title)
	}
}

func TestAThinFixedVersionStillBeatsItsBrokenFirst(t *testing.T) {
	prefs := DefaultPreferences()
	v1 := seeded("[Group] Show - 05 (1080p).mkv", 1<<30, 40)
	v2 := seeded("[Group] Show - 05v2 (1080p).mkv", 1<<30, 6)
	if ranked := Rank([]Candidate{v1, v2}, prefs); ranked[0].Release.Version != 2 {
		t.Errorf("the 40-seeder v1 beat its own fixed v2: %s", ranked[0].Torrent.Title)
	}

	other := seeded("[Other] Show - 05 (1080p).mkv", 1<<30, 40)
	if ranked := Rank([]Candidate{v1, v2, other}, prefs); ranked[0].Torrent.Title == v1.Torrent.Title {
		t.Errorf("the broken v1 won while its v2 and another group were listed")
	}

	held := v1
	held.Held = true
	if ranked := Rank([]Candidate{held, v2}, prefs); ranked[0].Release.Version != 2 {
		t.Errorf("an episode already taken from the v1 kept it over the v2: %s", ranked[0].Torrent.Title)
	}
}

func TestTheBestSeededPackWinsWhenTwoOutseedADyingEpisode(t *testing.T) {
	prefs := DefaultPreferences()
	dying := seeded("[G1] Show - 05 [1080p].mkv", 1<<30, 3)
	thin := seeded("[G2] Show [BD][1080p]", 8<<30, 8)
	thin.Confirmed, thin.TotalEpisodes = false, 12
	strong := seeded("[G3] Show [BD][1080p][Dual Audio]", 8<<30, 50)
	strong.Confirmed, strong.TotalEpisodes = false, 12
	for _, order := range permutations([]Candidate{dying, thin, strong}) {
		if ranked := Rank(order, prefs); ranked[0].Torrent.Seeders != 50 {
			t.Fatalf("picked the %d-seeder %s", ranked[0].Torrent.Seeders, ranked[0].Torrent.Title)
		}
	}
}

func TestDyingEpisodesNeverHandTheWinToALowerResolution(t *testing.T) {
	prefs := DefaultPreferences()
	low := seeded("[Erai-raws] Show - 03 [720p].mkv", 700<<20, 0)
	low.Torrent.SeedersKnown = false
	dying := seeded("[HorribleSubs] Show - 03 [1080p].mkv", 1<<30, 4)
	uncounted := seeded("[Erai-raws] Show - 03 [1080p].mkv", 1<<30, 0)
	uncounted.Torrent.SeedersKnown = false
	pack := seeded("[GroupB] Show + Movies + OVA [BD][1080p][Dual Audio]", 20<<30, 43)
	pack.Confirmed = false
	pack.TotalEpisodes = 12

	ranked := Rank([]Candidate{low, dying, uncounted, pack}, prefs)
	if ranked[0].Torrent.Seeders != 43 {
		t.Errorf("picked %s; want the 43-seeder 1080p pack", ranked[0].Torrent.Title)
	}

	ranked = Rank([]Candidate{low, dying, uncounted}, prefs)
	if ranked[0].Release.Resolution != "1080p" {
		t.Errorf("picked %s; dying 1080p copies must still beat a 720p", ranked[0].Torrent.Title)
	}
}
