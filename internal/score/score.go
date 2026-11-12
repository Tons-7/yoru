package score

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"yoru/internal/film"
	"yoru/internal/indexer"
	"yoru/internal/parse"
)

type Tier struct {
	Resolution string
	Source     string
	Codec      string
	BitDepth   int
}

func ParseTier(s string) Tier {
	var t Tier
	for part := range strings.SplitSeq(s, ":") {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case strings.EqualFold(part, "BD"), strings.EqualFold(part, "WEB"),
			strings.EqualFold(part, "BDRemux"), strings.EqualFold(part, "TV"),
			strings.EqualFold(part, "DVD"):
			t.Source = canonicalSource(part)
		case strings.EqualFold(part, "HEVC"), strings.EqualFold(part, "H264"),
			strings.EqualFold(part, "AV1"), strings.EqualFold(part, "VP9"):
			t.Codec = strings.ToUpper(part)
		case isDigits(part):
			if n, _ := strconv.Atoi(part); n == 8 || n == 10 {
				t.BitDepth = n
			}
		default:
			t.Resolution = strings.ToLower(part)
		}
	}
	return t
}

func (t Tier) matches(r parse.Release) bool {
	if t.Resolution != "" && !strings.EqualFold(t.Resolution, r.Resolution) {
		return false
	}
	if t.Source != "" && !strings.EqualFold(t.Source, r.Source) {
		return false
	}
	if t.Codec != "" && !strings.EqualFold(t.Codec, r.Codec) {
		return false
	}
	if t.BitDepth != 0 && t.BitDepth != r.BitDepth {
		return false
	}
	return true
}

type Preferences struct {
	Ladder       []Tier
	MaxAutoBytes int64
	AllowHi10P   bool
	PreferGroups []string

	AvoidGroups  []string
	ExcludeTerms []string
	AvoidLQ      bool

	Audio string

	AllowRaw bool

	SubLanguages []string

	HardwareTranscode bool
}

func needsSoftwareTranscode(rel parse.Release) bool {
	switch rel.Codec {
	case "HEVC":
		return true
	case "H264":
		return rel.BitDepth == 10
	}
	return false
}

func DefaultPreferences() Preferences {
	return Preferences{
		Ladder: []Tier{
			ParseTier("1080p:BD:HEVC:10"),
			ParseTier("1080p:BD"),
			ParseTier("1080p:WEB"),
			ParseTier("720p"),
		},
		MaxAutoBytes: 3 << 30,
		Audio:        "sub",
		SubLanguages: []string{"en"},
	}
}

var (
	dubbed   = regexp.MustCompile(`(?i)\b(dub|dubbed|english[\s._-]?dub|eng[\s._-]?dub)\b`)
	subbed   = regexp.MustCompile(`(?i)\b(sub|subbed|softsub|hardsub|multi[\s._-]?subs?|eng[\s._-]?subs?)\b`)
	dualAudi = regexp.MustCompile(`(?i)\bdual[\s._-]?audio\b`)
	otherDub = regexp.MustCompile(`(?i)\b(hindi|tamil|telugu|spanish|latino|castellano|portuguese|brazilian|` +
		`german|french|italian|mandarin|chinese|cantonese|korean|tagalog|arabic|russian|indonesian|polish|` +
		`turkish|thai|vietnamese)[\s._-]*dub(bed|s)?\b|中配|國語|国语|粵語|粤语`)
	englishDub = regexp.MustCompile(`(?i)\b(english|eng)[\s._-]?dub(bed|s)?\b`)
)

func audioMismatch(rel parse.Release, want string) bool {
	if want == "" || want == "either" {
		return false
	}
	raw := rel.Raw
	if rel.DualAudio || dualAudi.MatchString(raw) {
		return false
	}

	switch want {
	case "dub":
		return !dubbed.MatchString(raw) || otherDub.MatchString(raw) && !englishDub.MatchString(raw)
	case "sub":
		return dubbed.MatchString(raw) && !subbed.MatchString(raw)
	}
	return false
}

type Candidate struct {
	Torrent indexer.Torrent
	Release parse.Release

	SeaDexBest  bool
	SeaDexGroup bool

	SeaDexListed     bool     `json:",omitempty"`
	SeaDexNotes      string   `json:",omitempty"`
	SeaDexCompare    []string `json:",omitempty"`
	SeaDexIncomplete bool     `json:",omitempty"`

	TotalEpisodes int

	RuntimeMinutes int

	Confirmed bool

	WrongShow bool

	Numbers []int `json:"-"`

	Episode int `json:"-"`

	Film *film.Key `json:"-"`

	WrongFilm bool

	ByHand bool

	Season  int              `json:"-"`
	Counted int              `json:"-"`
	Others  []string         `json:"-"`
	Named   map[int][]string `json:"-"`
	Held    bool             `json:"held,omitempty"`
}

const nominalRuntime = 24

func (c Candidate) SizeLimit(prefs Preferences) int64 {
	if prefs.MaxAutoBytes <= 0 || c.RuntimeMinutes <= nominalRuntime {
		return prefs.MaxAutoBytes
	}
	scaled := float64(prefs.MaxAutoBytes) * float64(c.RuntimeMinutes) / nominalRuntime
	if limit := int64(scaled); limit > prefs.MaxAutoBytes {
		return limit
	}
	return prefs.MaxAutoBytes
}

func (c Candidate) EpisodeBytes() int64 {
	if c.Film != nil {
		if c.Release.Batch && c.Film.Count > 1 {
			return c.Torrent.Size / int64(c.Film.Count)
		}
		return c.Torrent.Size
	}
	if !c.Release.Batch && c.Release.Episode > 0 {
		return c.Torrent.Size
	}

	count := c.Release.EpisodeEnd - c.Release.Episode + 1
	if count < 2 {
		count = c.TotalEpisodes
	}
	if count < 2 {
		return c.Torrent.Size
	}
	return c.Torrent.Size / int64(count)
}

type Result struct {
	Candidate
	Score   float64  `json:"score"`
	Reasons []string `json:"reasons"`

	Playable   bool        `json:"playable"`
	AutoPick   bool        `json:"autoPick"`
	Blocked    string      `json:"blocked,omitempty"`
	Rejections []Rejection `json:"rejections,omitempty"`
	Fallback   bool        `json:"fallback,omitempty"`

	resRank    float64
	unreadable bool
	starved    bool
	vouched    bool
	outdated   bool
	sticky     bool
}

const (
	tierStep            = 100.0
	partialTier         = 0.6
	seaDexBestBonus     = 260.0
	seaDexGroup         = 90.0
	preferredGroup      = 70.0
	trustedBonus        = 25.0
	dualAudioBonus      = 5.0
	batchPenalty        = 15.0
	unknownScopePenalty = 25.0
	seedWeight          = 22.0

	maxSeedBonus = 50.0

	thinSwarm        = 5
	thinSwarmPenalty = 30.0

	subLanguageBonus  = 15.0
	wrongLanguage     = 170.0
	hardSubbedPenalty = 60.0
)

func Rank(cands []Candidate, prefs Preferences) []Result {
	out := make([]Result, 0, len(cands))
	for _, c := range cands {
		out = append(out, evaluate(c, prefs))
	}
	lastResort(out, prefs)
	top := 0.0
	for _, r := range out {
		if r.AutoPick {
			top = max(top, r.resRank)
		}
	}
	for i := range out {
		for j := range out {
			if i != j && sameRelease(out[i].Release, out[j].Release) && out[j].Release.Version > out[i].Release.Version {
				out[i].outdated = true
			}
		}
		out[i].sticky = out[i].AutoPick && out[i].Held && !out[i].outdated && out[i].resRank >= top
	}
	for i := range out {
		for j := range out {
			if i == j || !out[i].AutoPick || !out[j].AutoPick || out[i].resRank != out[j].resRank ||
				(!out[j].Playable && out[i].Playable) || (out[j].unreadable && !out[i].unreadable) ||
				(sameRelease(out[i].Release, out[j].Release) && out[j].Release.Version < out[i].Release.Version) ||
				!outseeds(out[j], out[i]) {
				continue
			}
			if !out[i].starved {
				out[i].Reasons = append(out[i].Reasons, "a far better-seeded copy is listed")
			}
			out[i].starved = true
			if out[i].Confirmed || out[i].sticky {
				out[j].vouched = true
			}
		}
	}
	healthy := map[float64]bool{}
	for i, r := range out {
		if r.AutoPick && (r.Confirmed || r.sticky) && !r.starved {
			healthy[r.resRank] = true
		}
		if r.sticky {
			out[i].Reasons = append(out[i].Reasons, "already downloading this show from it")
		}
	}
	for i := range out {
		out[i].vouched = out[i].vouched && !healthy[out[i].resRank]
	}

	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && better(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func better(a, b Result) bool {
	if a.AutoPick != b.AutoPick {
		return a.AutoPick
	}
	if a.AutoPick && (a.Release.Resolution == "") != (b.Release.Resolution == "") {
		return b.Release.Resolution == ""
	}
	if ac, bc := a.Confirmed || a.vouched || a.sticky, b.Confirmed || b.vouched || b.sticky; ac != bc {
		return ac
	}
	if a.AutoPick && a.resRank != b.resRank {
		return a.resRank > b.resRank
	}
	if a.AutoPick && a.starved != b.starved {
		return !a.starved
	}
	if a.AutoPick && a.outdated != b.outdated {
		return !a.outdated
	}
	if a.AutoPick && a.sticky != b.sticky {
		return a.sticky
	}
	if a.AutoPick && a.Film != nil && b.Film != nil && thin(a) != thin(b) {
		return !thin(a)
	}
	if a.AutoPick && a.Release.Batch != b.Release.Batch {
		return !a.Release.Batch
	}
	if a.AutoPick && a.SeaDexBest != b.SeaDexBest {
		return a.SeaDexBest
	}
	if a.AutoPick && a.unreadable != b.unreadable {
		return !a.unreadable
	}
	if a.AutoPick && a.Playable != b.Playable {
		return a.Playable
	}
	if a.AutoPick && a.Release.Version != b.Release.Version && sameRelease(a.Release, b.Release) {
		return a.Release.Version > b.Release.Version
	}
	return a.Score > b.Score
}

const (
	weakSwarm    = 10
	outseedRatio = 2
	outseedLead  = 5
)

func outseeds(a, b Result) bool {
	if !a.Torrent.SeedersKnown {
		return false
	}
	if !b.Torrent.SeedersKnown {
		return a.Torrent.Seeders >= weakSwarm
	}
	if b.Torrent.Seeders >= weakSwarm {
		return false
	}
	return a.Torrent.Seeders >= outseedRatio*max(b.Torrent.Seeders, 1) && a.Torrent.Seeders-b.Torrent.Seeders >= outseedLead
}

func sameRelease(a, b parse.Release) bool {
	return a.Group != "" && strings.EqualFold(a.Group, b.Group) && a.Resolution == b.Resolution &&
		a.Batch == b.Batch && a.Episode == b.Episode && a.EpisodeEnd == b.EpisodeEnd
}

func Ideal(r Result, prefs Preferences) bool {
	top := 0.0
	for _, t := range prefs.Ladder {
		top = max(top, resolutionRank(t.Resolution, prefs))
	}
	if r.resRank < top {
		return false
	}
	if r.SeaDexBest || len(prefs.PreferGroups) == 0 {
		return true
	}
	for _, g := range prefs.PreferGroups {
		if strings.EqualFold(g, r.Release.Group) {
			return true
		}
	}
	return false
}

func Wanted(prefs Preferences) string {
	top, res := 0.0, ""
	for _, t := range prefs.Ladder {
		if rank := resolutionRank(t.Resolution, prefs); rank > top {
			top, res = rank, t.Resolution
		}
	}
	if len(prefs.PreferGroups) == 0 {
		return strings.Join(strings.Fields("a "+res+" release"), " ")
	}
	groups := strings.Join(prefs.PreferGroups, " or ")
	if res == "" {
		return groups
	}
	return groups + " at " + res
}

func Fixes(r Result, held parse.Release) bool {
	return sameRelease(r.Release, held) && r.Release.Version > held.Version
}

func Better(r Result, heldHash string, held parse.Release, ranked []Result, prefs Preferences) bool {
	if !Ideal(r, prefs) {
		return false
	}
	for _, x := range ranked {
		if strings.EqualFold(x.Torrent.InfoHash, heldHash) {
			return !Ideal(x, prefs)
		}
	}
	return !Ideal(Result{Candidate: Candidate{Release: held}, resRank: resolutionRank(held.Resolution, prefs)}, prefs)
}

func thin(r Result) bool { return r.Torrent.SeedersKnown && r.Torrent.Seeders < thinSwarm }

func Best(cands []Candidate, prefs Preferences) (Result, bool) {
	ranked := Rank(cands, prefs)
	if len(ranked) == 0 || !ranked[0].AutoPick {
		return Result{}, false
	}
	return ranked[0], true
}

func evaluate(c Candidate, prefs Preferences) Result {
	r := Result{Candidate: c, AutoPick: true}
	rel := c.Release
	r.resRank = resolutionRank(rel.Resolution, prefs)

	r.Playable = prefs.HardwareTranscode || !needsSoftwareTranscode(rel)
	if !r.Playable {
		r.add("needs software transcoding on this machine; a seek may stall")
	}

	if r.Rejections = evaluateSpecs(c, prefs); len(r.Rejections) > 0 {
		r.Blocked = r.Rejections[0].Reason
		r.AutoPick = false
	}
	if c.ByHand {
		r.AutoPick = false
		r.add("pasted magnet: plays only when picked")
	}
	if rel.Version >= 2 {
		r.addf("v%d, the group's fixed release", rel.Version)
	}

	if !r.applyLadder(rel, prefs) {
		if rank := ladderPlace(rel.Resolution, prefs, false); rank > 0 {
			r.Score += rank * tierStep * partialTier
			r.addf("%s, unlabelled source", rel.Resolution)
		}
	}

	switch {
	case c.SeaDexBest:
		r.Score += seaDexBestBonus
		r.add("curated best release on SeaDex")
	case c.SeaDexGroup:
		r.Score += seaDexGroup
		r.add("group appears in SeaDex picks")
	}

	for _, g := range prefs.PreferGroups {
		if strings.EqualFold(g, rel.Group) {
			r.Score += preferredGroup
			r.addf("preferred group %s", rel.Group)
			break
		}
	}
	r.applyTiers(c)

	r.applySubtitles(rel, prefs)

	if c.Torrent.Trusted {
		r.Score += trustedBonus
		r.add("trusted uploader")
	}
	if rel.DualAudio {
		r.Score += dualAudioBonus
		r.add("dual audio, Japanese track selectable")
	}
	switch {
	case rel.Batch:
		r.Score -= batchPenalty
		r.add("batch, needs an extra step to find the episode")
	case rel.Episode == 0:
		r.Score -= unknownScopePenalty
		r.add("episode not stated in the name")
	}

	switch {
	case !c.Torrent.SeedersKnown:
		r.add("swarm size unknown")
	case c.Torrent.Seeders > 0:
		r.Score += min(math.Log1p(float64(c.Torrent.Seeders))*seedWeight, maxSeedBonus)
		r.addf("%d seeders", c.Torrent.Seeders)
		if c.Torrent.Seeders < thinSwarm {
			r.Score -= thinSwarmPenalty
			r.add("few seeders, expect a slow download")
		}
	}

	if r.Score < 0 {
		r.Score = 0
	}
	return r
}

func (r *Result) applySubtitles(rel parse.Release, prefs Preferences) {
	if len(prefs.SubLanguages) == 0 || len(rel.Subtitles) == 0 {
		return
	}

	if parse.HasSubtitleLanguage(rel, prefs.SubLanguages) {
		r.Score += subLanguageBonus
		r.addf("subtitles in %s", strings.Join(rel.Subtitles, ", "))
		return
	}

	r.Score -= wrongLanguage
	r.unreadable = true
	r.addf("subtitles only in %s", strings.Join(rel.Subtitles, ", "))
	if rel.HardSub {
		r.Score -= hardSubbedPenalty
		r.add("hardsubbed, the track cannot be turned off")
	}
}

func (r *Result) applyLadder(rel parse.Release, prefs Preferences) bool {
	for i, tier := range prefs.Ladder {
		if tier.matches(rel) {
			r.Score += float64(len(prefs.Ladder)-i) * tierStep
			r.addf("matches quality tier %d", i+1)
			return true
		}
	}
	return false
}

func ResolutionRank(res string, prefs Preferences) float64 { return resolutionRank(res, prefs) }

func resolutionRank(res string, prefs Preferences) float64 { return ladderPlace(res, prefs, true) }

func ladderPlace(res string, prefs Preferences, best bool) float64 {
	res = strings.ToLower(res)
	if res == "" {
		return 0
	}

	var seen int
	var rank float64
	for i, tier := range prefs.Ladder {
		if tier.Resolution == "" {
			continue
		}
		seen++
		if tier.Resolution == res && (rank == 0 || !best) {
			rank = float64(len(prefs.Ladder) - i)
		}
	}
	if rank > 0 {
		return rank
	}
	if seen == 0 {
		return 0
	}
	return 0.5
}

func (r *Result) add(reason string) { r.Reasons = append(r.Reasons, reason) }

const fallbackAge = 24 * time.Hour

func lastResort(out []Result, prefs Preferences) {
	if prefs.AllowHi10P {
		return
	}
	lifted := prefs
	lifted.AllowHi10P = true
	for i, r := range out {
		fresh := !r.Torrent.Published.IsZero() && time.Since(r.Torrent.Published) < fallbackAge
		out[i].Fallback = !r.ByHand && !fresh && len(r.Rejections) > 0 && len(evaluateSpecs(r.Candidate, lifted)) == 0
	}
	if slices.ContainsFunc(out, func(r Result) bool { return r.AutoPick }) {
		return
	}
	for i := range out {
		if out[i].Fallback {
			out[i] = liftFallback(out[i])
		}
	}
}

func liftFallback(r Result) Result {
	r.AutoPick, r.Blocked, r.Rejections = true, "", nil
	r.Reasons = append(slices.Clone(r.Reasons), "no other release would do: 10-bit H.264, converted on the processor to play")
	return r
}

func Fallbacks(results []Result) []Result {
	var out []Result
	for _, r := range results {
		if r.Fallback && !r.AutoPick {
			out = append(out, liftFallback(r))
		}
	}
	return out
}

func (r *Result) addf(format string, args ...any) {
	r.Reasons = append(r.Reasons, fmt.Sprintf(format, args...))
}

func canonicalSource(s string) string {
	switch strings.ToLower(s) {
	case "bdremux":
		return "BDRemux"
	case "bd":
		return "BD"
	case "web":
		return "WEB"
	case "tv":
		return "TV"
	case "dvd":
		return "DVD"
	}
	return s
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
