package score

import (
	"slices"
	"strings"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/parse"
)

func candidate(title string, seeders int, size int64) Candidate {
	return Candidate{
		Torrent: indexer.Torrent{
			Title: title, Seeders: seeders, SeedersKnown: true, Size: size, InfoHash: title,
		},
		Release: parse.Parse(title),
	}
}

func TestParseTier(t *testing.T) {
	tests := []struct {
		in   string
		want Tier
	}{
		{"1080p:BD:HEVC:10", Tier{Resolution: "1080p", Source: "BD", Codec: "HEVC", BitDepth: 10}},
		{"1080p:WEB", Tier{Resolution: "1080p", Source: "WEB"}},
		{"720p", Tier{Resolution: "720p"}},
		{"", Tier{}},
		{"1080p::HEVC:", Tier{Resolution: "1080p", Codec: "HEVC"}},
		{"1080p:BD:9", Tier{Resolution: "1080p", Source: "BD"}},
	}

	for _, tt := range tests {
		if got := ParseTier(tt.in); got != tt.want {
			t.Errorf("ParseTier(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

// A tier requires only what it names, so a broad tier accepts a release stating more.
func TestTierMatchesOnSpecifiedFieldsOnly(t *testing.T) {
	rel := parse.Release{Resolution: "1080p", Source: "BD", Codec: "HEVC", BitDepth: 10}

	if !ParseTier("1080p").matches(rel) {
		t.Error("resolution-only tier should match")
	}
	if !ParseTier("1080p:BD:HEVC:10").matches(rel) {
		t.Error("fully specified tier should match")
	}
	if ParseTier("1080p:WEB").matches(rel) {
		t.Error("source mismatch should not match")
	}
	if ParseTier("720p").matches(rel) {
		t.Error("resolution mismatch should not match")
	}
}

func TestLadderOrderDecidesRanking(t *testing.T) {
	// Assume a hardware transcoder so the ladder, not playability, decides.
	prefs := DefaultPreferences()
	prefs.HardwareTranscode = true
	cands := []Candidate{
		candidate("[G] Show - 01 [720p][AAC].mkv", 50, 500<<20),
		candidate("[G] Show - 01 [1080p][WEB-DL][AAC].mkv", 50, 1<<30),
		candidate("[G] Show - 01 [BD 1080p HEVC 10bit FLAC].mkv", 50, 1<<30),
	}

	ranked := Rank(cands, prefs)
	if got := ranked[0].Release.Source; got != "BD" {
		t.Fatalf("top result source = %q, want the highest tier BD", got)
	}
	if got := ranked[len(ranked)-1].Release.Resolution; got != "720p" {
		t.Fatalf("last result = %q, want the lowest tier", got)
	}
}

// SeaDex's pick wins among releases of its resolution, never over a higher one.
func TestSeaDexWinsWithinItsResolution(t *testing.T) {
	prefs := DefaultPreferences()

	web := candidate("[G] Show - 01 [1080p][WEB-DL].mkv", 500, 1<<30)
	curated := candidate("[sam] Show - 01 [1080p].mkv", 20, 1<<30)
	curated.SeaDexBest = true
	best, ok := Best([]Candidate{web, curated}, prefs)
	if !ok {
		t.Fatal("expected an automatic pick")
	}
	if best.Release.Group != "sam" {
		t.Fatalf("picked %q; the curated best should outrank another 1080p", best.Release.Group)
	}
	if !containsReason(best.Reasons, "SeaDex") {
		t.Errorf("reasons do not mention SeaDex: %v", best.Reasons)
	}

	lower := candidate("[sam] Show - 01 [720p].mkv", 20, 500<<20)
	lower.SeaDexBest = true
	if best, _ := Best([]Candidate{lower, web}, prefs); best.Release.Resolution != "1080p" {
		t.Fatalf("a 720p SeaDex pick outranked a 1080p release")
	}
}

// A group's v2 beats its own v1 however many more seed the v1, and never a higher resolution.
func TestFixedReleaseBeatsItsFirstVersion(t *testing.T) {
	prefs := DefaultPreferences()
	v1 := candidate("[SubsPlease] Show - 05 (1080p) [AAAA1111].mkv", 500, 1400<<20)
	v2 := candidate("[SubsPlease] Show - 05v2 (1080p) [BBBB2222].mkv", 30, 1400<<20)
	v1.Torrent.InfoHash, v2.Torrent.InfoHash = "v1", "v2"
	best, _ := Best([]Candidate{v1, v2}, prefs)
	if best.Torrent.InfoHash != "v2" {
		t.Fatalf("picked %s; the fixed v2 should beat the v1", best.Torrent.InfoHash)
	}
	if !containsReason(best.Reasons, "fixed release") {
		t.Errorf("reasons don't say why: %v", best.Reasons)
	}

	lower := candidate("[SubsPlease] Show - 05v2 (720p) [CCCC3333].mkv", 30, 700<<20)
	lower.Torrent.InfoHash = "720v2"
	if best, _ := Best([]Candidate{v1, lower}, prefs); best.Torrent.InfoHash != "v1" {
		t.Fatalf("picked %s; a 720p v2 beat the 1080p v1", best.Torrent.InfoHash)
	}
	other := candidate("[Erai-raws] Show - 05 [1080p].mkv", 900, 1400<<20)
	other.Torrent.InfoHash = "other"
	if best, _ := Best([]Candidate{other, v2}, prefs); best.Torrent.InfoHash != "other" {
		t.Fatalf("picked %s; a version only orders one group's releases", best.Torrent.InfoHash)
	}
}

// An avoided group or an excluded word is never picked on its own, but stays listed with the reason.
func TestAvoidedReleasesStayListedButAreNeverPicked(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.AvoidGroups = []string{"badgroup"}
	prefs.ExcludeTerms = []string{"x265 mini"}
	bad := candidate("[BadGroup] Show - 05 [1080p].mkv", 900, 1<<30)
	mini := candidate("[Good] Show - 05 [1080p x265 Mini].mkv", 900, 300<<20)
	fine := candidate("[Fine] Show - 05 [720p].mkv", 10, 500<<20)
	ranked := Rank([]Candidate{bad, mini, fine}, prefs)
	if ranked[0].Release.Group != "Fine" {
		t.Fatalf("picked %s; avoided releases must not be taken on their own", ranked[0].Release.Group)
	}
	for _, r := range ranked[1:] {
		if r.AutoPick || r.Blocked == "" {
			t.Errorf("%s: autoPick %v, blocked %q; want listed with a reason", r.Torrent.Title, r.AutoPick, r.Blocked)
		}
	}
}

// TRaSH's grades order groups within a source and resolution; a low-quality group ranks last, or with
// AvoidLQ is never picked on its own.
func TestGroupTiersOrderWithinAResolution(t *testing.T) {
	SetGroupTiers([]GroupTier{
		NewGroupTier("web", 1, "Top", `\b(Top)\b`),
		NewGroupTier("web", 6, "Low", `\b(Low)\b`),
		NewGroupTier("lq", 1, "Mini", `\[Mini\]`),
	})
	t.Cleanup(func() { SetGroupTiers(nil) })
	prefs := DefaultPreferences()

	top := candidate("[Top] Show - 05 (WEB 1080p).mkv", 40, 1<<30)
	low := candidate("[Low] Show - 05 (WEB 1080p).mkv", 40, 1<<30)
	mini := candidate("[Mini] Show - 05 (WEB 1080p).mkv", 40, 1<<30)
	if best, _ := Best([]Candidate{mini, low, top}, prefs); best.Release.Group != "Top" {
		t.Fatalf("picked %s; the tier-1 group should lead", best.Release.Group)
	}
	ranked := Rank([]Candidate{mini, low}, prefs)
	if ranked[0].Release.Group != "Low" || !ranked[1].AutoPick {
		t.Fatalf("ranked %s first, mini autoPick %v; a low-quality group ranks last, still allowed", ranked[0].Release.Group, ranked[1].AutoPick)
	}
	prefs.AvoidLQ = true
	if r := Rank([]Candidate{mini}, prefs)[0]; r.AutoPick {
		t.Fatal("with low-quality groups avoided, one was still picked")
	}
	// A grade never buys a lower resolution.
	lower := candidate("[Top] Show - 05 (WEB 720p).mkv", 40, 500<<20)
	if best, _ := Best([]Candidate{lower, low}, DefaultPreferences()); best.Release.Resolution != "1080p" {
		t.Fatal("a tier-1 720p outranked a 1080p")
	}
}

// A season pack at 1080p beats a single episode at 720p: a pack only waits behind singles of its resolution.
func TestHigherResolutionPackBeatsLowerSingle(t *testing.T) {
	prefs := DefaultPreferences()
	single := Candidate{
		Torrent: indexer.Torrent{Title: "[G] Show - 05 [720p].mkv", Seeders: 200, Size: 500 << 20, InfoHash: "single"},
		Release: parse.Parse("[G] Show - 05 [720p].mkv"),
	}
	pack := Candidate{
		Torrent:       indexer.Torrent{Title: "[G] Show (01-12) [1080p][BD][Batch]", Seeders: 40, Size: 12 << 30, InfoHash: "pack"},
		Release:       parse.Parse("[G] Show (01-12) [1080p][BD][Batch]"),
		TotalEpisodes: 12,
	}
	if best, _ := Best([]Candidate{single, pack}, prefs); best.Torrent.InfoHash != "pack" {
		t.Fatalf("picked %s; the 720p single outranked the 1080p pack", best.Torrent.InfoHash)
	}
}

// An indexer without peer counts has releases read as neither dead nor healthy.
func TestUncountedSwarmIsNeitherRejectedNorRewarded(t *testing.T) {
	prefs := DefaultPreferences()

	uncounted := candidate("[A] Show - 01 [1080p][WEB-DL].mkv", 0, 1<<30)
	uncounted.Torrent.SeedersKnown = false
	got := Rank([]Candidate{uncounted}, prefs)[0]

	if !got.AutoPick {
		t.Fatalf("blocked as if dead: %q", got.Blocked)
	}
	if !slices.Contains(got.Reasons, "swarm size unknown") {
		t.Errorf("reasons = %v, want the unknown swarm stated", got.Reasons)
	}

	counted := candidate("[B] Show - 01 [1080p][WEB-DL].mkv", 20, 1<<30)
	if Rank([]Candidate{uncounted, counted}, prefs)[0].Release.Group != "B" {
		t.Error("a counted swarm should outrank an uncounted one of the same tier")
	}
}

func TestCountedZeroIsStillDead(t *testing.T) {
	dead := candidate("[A] Show - 01 [1080p][WEB-DL].mkv", 0, 1<<30)
	got := Rank([]Candidate{dead}, DefaultPreferences())[0]

	if got.AutoPick {
		t.Fatal("a counted empty swarm should not be picked")
	}
	if !strings.Contains(got.Blocked, "no seeders") {
		t.Errorf("blocked = %q, want it to name the dead swarm", got.Blocked)
	}
}

func TestDisqualifiers(t *testing.T) {
	prefs := DefaultPreferences()

	tests := []struct {
		name string
		cand Candidate
		want string
	}{
		{
			name: "no seeders",
			cand: candidate("[G] Show - 01 [1080p].mkv", 0, 1<<30),
			want: "no seeders",
		},
		{
			name: "raw transport stream",
			cand: candidate("[shincaps] Show 01 (BS11 1920x1080 MPEG2 AAC).ts", 30, 1<<30),
			want: "no subtitles",
		},
		{
			name: "hi10p",
			cand: candidate("[G] Show - 01 [1080p Hi10P AAC].mkv", 30, 1<<30),
			want: "hardware decode",
		},
		{
			name: "over the size ceiling",
			cand: candidate("[G] Show - 01 [BD 1080p FLAC].mkv", 30, 20<<30),
			want: "size limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Rank([]Candidate{tt.cand}, prefs)[0]
			if got.AutoPick {
				t.Fatal("should not be eligible for automatic selection")
			}
			if !strings.Contains(got.Blocked, tt.want) {
				t.Fatalf("blocked = %q, want it to mention %q", got.Blocked, tt.want)
			}
		})
	}
}

// Blocked releases stay listed for the manual picker, barred only from starting automatically.
func TestBlockedCandidatesAreStillReturned(t *testing.T) {
	prefs := DefaultPreferences()
	ranked := Rank([]Candidate{
		candidate("[G] Show - 01 [BD 1080p FLAC].mkv", 30, 20<<30),
	}, prefs)

	if len(ranked) != 1 {
		t.Fatalf("got %d results, want the blocked release listed", len(ranked))
	}
	if _, ok := Best([]Candidate{candidate("[G] Show - 01 [BD 1080p].mkv", 30, 20<<30)}, prefs); ok {
		t.Fatal("Best returned a release that exceeds the size limit")
	}
}

func TestHi10PAllowedByPreference(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.AllowHi10P = true

	got := Rank([]Candidate{candidate("[G] Show - 01 [1080p Hi10P AAC].mkv", 30, 1<<30)}, prefs)[0]
	if !got.AutoPick {
		t.Fatalf("blocked despite the preference: %q", got.Blocked)
	}
}

func TestSeedersBreakTiesWithoutDominating(t *testing.T) {
	prefs := DefaultPreferences()

	// Same tier, wildly different health: seeders should decide.
	few := candidate("[A] Show - 01 [1080p][WEB-DL].mkv", 2, 1<<30)
	many := candidate("[B] Show - 01 [1080p][WEB-DL].mkv", 900, 1<<30)
	if Rank([]Candidate{few, many}, prefs)[0].Release.Group != "B" {
		t.Error("healthier torrent should win a tie")
	}

	// Different tiers: a well-seeded low tier must not beat a higher one.
	seeded720 := candidate("[A] Show - 01 [720p].mkv", 5000, 500<<20)
	quiet1080 := candidate("[B] Show - 01 [BD 1080p HEVC 10bit].mkv", 4, 1<<30)
	if Rank([]Candidate{seeded720, quiet1080}, prefs)[0].Release.Group != "B" {
		t.Error("seeder count outweighed the quality ladder")
	}
}

// A release with no source tag matches no tier, yet beats a fully tagged lower resolution.
func TestUnlabelledHighResBeatsLabelledLowRes(t *testing.T) {
	prefs := DefaultPreferences()

	bare1080 := candidate("[G] Show - 01 [1080p].mkv", 40, 1<<30)
	tagged720 := candidate("[G] Show - 01 [720p][WEB-DL].mkv", 40, 1<<30)

	if got := Rank([]Candidate{tagged720, bare1080}, prefs)[0].Release.Resolution; got != "1080p" {
		t.Fatalf("top result is %q; an untagged 1080p should outrank a tagged 720p", got)
	}
}

// The partial credit must stay below a real tier match, so a properly labelled Blu-ray still wins.
func TestFullTierBeatsPartialCredit(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.HardwareTranscode = true

	bare1080 := candidate("[G] Show - 01 [1080p].mkv", 40, 1<<30)
	bd1080 := candidate("[G] Show - 01 [BD 1080p HEVC 10bit].mkv", 40, 1<<30)

	top := Rank([]Candidate{bare1080, bd1080}, prefs)[0]
	if top.Release.Source != "BD" {
		t.Fatalf("top result source = %q, want the labelled Blu-ray", top.Release.Source)
	}
}

// Unlabelled is worth less than any labelled tier at its resolution: "1080p HEVC" < 1080p WEB-DL.
func TestUnlabelledSourceSitsUnderEveryTierOfItsResolution(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.HardwareTranscode = true
	bare := candidate("[DKB] Show - 42 [1080p][HEVC x265 10bit][Multi-Subs]", 139, 1<<30)
	web := candidate("[ToonsHub] Show S01E42 1080p HULU WEB-DL AAC2.0 H.264 (English-Sub)", 764, 1<<30)
	sd := candidate("[Group] Show - 42 [720p][WEB][Multi-Subs]", 900, 1<<29)

	ranked := Rank([]Candidate{bare, web, sd}, prefs)
	if ranked[0].Torrent.Title != web.Torrent.Title {
		t.Errorf("ranked first: %s, want the labelled WEB-DL", ranked[0].Torrent.Title)
	}
	if ranked[1].Torrent.Title != bare.Torrent.Title {
		t.Errorf("ranked second: %s, want the unlabelled 1080p above 720p", ranked[1].Torrent.Title)
	}
}

// 2160p anime is nearly always upscaled, so a resolution off the ladder can't outrank one on it.
func TestResolutionRankFollowsTheLadder(t *testing.T) {
	prefs := DefaultPreferences()

	if resolutionRank("1080p", prefs) <= resolutionRank("720p", prefs) {
		t.Error("1080p should rank above 720p")
	}
	if resolutionRank("2160p", prefs) >= resolutionRank("720p", prefs) {
		t.Error("2160p is not on the ladder and should rank below anything that is")
	}
	if resolutionRank("", prefs) != 0 {
		t.Error("an unknown resolution should rank zero")
	}

	// A resolution takes its best place: 720p BD listed between two 1080p tiers stays under 1080p.
	prefs.Ladder = []Tier{ParseTier("1080p:BD:HEVC:10"), ParseTier("720p:BD"), ParseTier("1080p")}
	if resolutionRank("1080p", prefs) <= resolutionRank("720p", prefs) {
		t.Error("an interleaved ladder put 720p above 1080p")
	}
}

func TestUpscaleDoesNotBeatLadderResolution(t *testing.T) {
	prefs := DefaultPreferences()

	upscale := candidate("[G] Show - 01 [2160p].mkv", 40, 2<<30)
	native := candidate("[G] Show - 01 [1080p][WEB-DL].mkv", 40, 1<<30)

	if got := Rank([]Candidate{upscale, native}, prefs)[0].Release.Resolution; got != "1080p" {
		t.Fatalf("top result is %q; an off-ladder 2160p outranked a listed 1080p", got)
	}
}

// A season pack is a fallback for when no single episode exists.
func TestSingleEpisodeBeatsBetterSeededBatch(t *testing.T) {
	prefs := DefaultPreferences()

	single := Candidate{
		Torrent: indexer.Torrent{Title: "[G] Show - 05 [1080p][WEB-DL].mkv", Seeders: 20, Size: 1 << 30, InfoHash: "single"},
		Release: parse.Parse("[G] Show - 05 [1080p][WEB-DL].mkv"),
	}
	batch := Candidate{
		Torrent:       indexer.Torrent{Title: "[G] Show (01-25) [1080p][WEB-DL][Batch]", Seeders: 900, Size: 25 << 30, InfoHash: "batch"},
		Release:       parse.Parse("[G] Show (01-25) [1080p][WEB-DL][Batch]"),
		TotalEpisodes: 25,
	}

	best, ok := Best([]Candidate{batch, single}, prefs)
	if !ok {
		t.Fatal("expected a pick")
	}
	if best.Release.Batch {
		t.Fatal("a well-seeded batch outranked an available single episode")
	}
}

// A Blu-ray remux is usually named for the show alone, with no episode number
// and no batch marker. Judged as a single file its size is absurd.
func TestUnstatedScopeSizedPerEpisode(t *testing.T) {
	pack := Candidate{
		Torrent:       indexer.Torrent{Title: "[PMR] Show (BD Remux 1080p AVC FLAC)", Seeders: 40, Size: 28 << 30},
		Release:       parse.Parse("[PMR] Show (BD Remux 1080p AVC FLAC)"),
		TotalEpisodes: 28,
	}

	if pack.Release.Episode != 0 || pack.Release.Batch {
		t.Fatalf("precondition: parsed as episode %d batch %v", pack.Release.Episode, pack.Release.Batch)
	}
	if got, want := pack.EpisodeBytes(), int64(1<<30); got != want {
		t.Fatalf("episode size = %d, want the per-episode share %d", got, want)
	}
}

// Only one file is downloaded from a batch, so judging a season pack by its
// total size rejects the only well-seeded source older shows have.
func TestBatchJudgedByEpisodeSizeNotTotal(t *testing.T) {
	prefs := DefaultPreferences()

	batch := Candidate{
		Torrent:       indexer.Torrent{Title: "[G] Show (01-25) [1080p][WEB-DL][Batch]", Seeders: 100, Size: 25 << 30, InfoHash: "b"},
		Release:       parse.Parse("[G] Show (01-25) [1080p][WEB-DL][Batch]"),
		TotalEpisodes: 25,
	}

	if got := batch.EpisodeBytes(); got != 1<<30 {
		t.Fatalf("episode size = %d, want the per-episode share", got)
	}
	if r := Rank([]Candidate{batch}, prefs)[0]; !r.AutoPick {
		t.Fatalf("blocked a usable batch: %q", r.Blocked)
	}
}

func TestPreferredGroup(t *testing.T) {
	prefs := DefaultPreferences()
	prefs.PreferGroups = []string{"erai-raws"}

	a := candidate("[SubsPlease] Show - 01 [1080p][WEB-DL].mkv", 100, 1<<30)
	b := candidate("[Erai-raws] Show - 01 [1080p][WEB-DL].mkv", 100, 1<<30)

	best, _ := Best([]Candidate{a, b}, prefs)
	if best.Release.Group != "Erai-raws" {
		t.Fatalf("picked %q; group preference should be case-insensitive", best.Release.Group)
	}
}

func TestBatchIsPenalisedButUsable(t *testing.T) {
	prefs := DefaultPreferences()

	single := candidate("[G] Show - 01 [1080p][WEB-DL].mkv", 100, 1<<30)
	batch := candidate("[G] Show (01-12) [1080p][WEB-DL][Batch]", 100, 2<<30)

	ranked := Rank([]Candidate{batch, single}, prefs)
	if ranked[0].Release.Batch {
		t.Error("a single episode should be preferred over a batch")
	}
	if !ranked[1].AutoPick {
		t.Error("a batch should still be usable, just less preferred")
	}
}

func TestEmptyAndNoEligibleCandidates(t *testing.T) {
	prefs := DefaultPreferences()

	if got := Rank(nil, prefs); len(got) != 0 {
		t.Errorf("Rank(nil) = %v", got)
	}
	if _, ok := Best(nil, prefs); ok {
		t.Error("Best(nil) should report no pick")
	}
	if _, ok := Best([]Candidate{candidate("[G] Show - 01 [1080p].mkv", 0, 1<<30)}, prefs); ok {
		t.Error("a dead torrent should not be picked")
	}
}

func TestEveryScoredResultExplainsItself(t *testing.T) {
	prefs := DefaultPreferences()
	ranked := Rank([]Candidate{
		candidate("[SubsPlease] Show - 01 (1080p) [ABCD1234].mkv", 120, 1<<30),
	}, prefs)

	if len(ranked[0].Reasons) == 0 {
		t.Fatal("no reasons recorded; a pick has to be explainable")
	}
}

func containsReason(reasons []string, substr string) bool {
	for _, r := range reasons {
		if strings.Contains(r, substr) {
			return true
		}
	}
	return false
}

// Without a hardware encoder, H.264 beats HEVC at one resolution; a lower resolution never wins on it.
func TestPlayabilityBreaksTiesWithinAResolution(t *testing.T) {
	software := DefaultPreferences() // HardwareTranscode defaults false
	hardware := DefaultPreferences()
	hardware.HardwareTranscode = true

	h264 := candidate("[Erai-raws] Show - 01 [1080p][Multiple Subtitle].mkv", 40, 1<<30)
	hevc := candidate("[G] Show - 01 [BD 1080p HEVC 10bit FLAC].mkv", 400, 1<<30)
	lower := candidate("[G] Show - 01 [720p][WEB-DL].mkv", 900, 500<<20)

	// No hardware: playable 1080p H.264 beats a better-seeded Blu-ray HEVC.
	if top := Rank([]Candidate{hevc, h264}, software)[0]; top.Release.Codec == "HEVC" {
		t.Errorf("software machine chose the stall-prone HEVC over a playable H.264")
	}
	// But playability never drops a whole resolution: 1080p HEVC still beats 720p.
	if top := Rank([]Candidate{lower, hevc}, software)[0]; top.Release.Resolution != "1080p" {
		t.Errorf("playability dropped a resolution: got %q", top.Release.Resolution)
	}
	// With hardware, HEVC transcodes fine, so the quality ladder wins again.
	if top := Rank([]Candidate{h264, hevc}, hardware)[0]; top.Release.Codec != "HEVC" {
		t.Errorf("hardware machine should keep the higher-quality HEVC, got %q", top.Release.Codec)
	}
}
