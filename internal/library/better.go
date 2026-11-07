package library

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"yoru/internal/parse"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

type BetterCopy struct {
	AnimeID int     `json:"animeId"`
	Title   string  `json:"title"`
	Cover   *string `json:"cover,omitempty"`
	Kept    string  `json:"kept"`

	Episodes []string `json:"episodes"`
	Size     int64    `json:"size"`

	InfoHash   string `json:"infoHash"`
	Group      string `json:"group"`
	Resolution string `json:"resolution"`
	Source     string `json:"source,omitempty"`
	DualAudio  bool   `json:"dualAudio,omitempty"`
	URL        string `json:"url,omitempty"`
	Release    string `json:"release"`

	hashes map[string]string
	all    []string
	rank   float64
	held   bool
}

type ScopeFor func(ctx context.Context, animeID int) torrent.Scope

const dismissedPref = "seadex.dismissed"

func BetterCopies(ctx context.Context, st *store.Store, scope ScopeFor, animeID int) ([]BetterCopy, error) {
	kept, err := st.KeptCopies(ctx)
	if err != nil {
		return nil, err
	}
	byShow := map[int][]store.KeptCopy{}
	var order []int
	for _, k := range kept {
		if animeID != 0 && k.AnimeID != animeID {
			continue
		}
		if _, seen := byShow[k.AnimeID]; !seen {
			order = append(order, k.AnimeID)
		}
		byShow[k.AnimeID] = append(byShow[k.AnimeID], k)
	}

	var out []BetterCopy
	for _, id := range order {
		prefs, err := st.Prefs(ctx, id)
		if err != nil {
			continue
		}
		if offer, ok := betterFor(ctx, st, byShow[id], Preferences(prefs), prefs.String(dismissedPref), scope); ok {
			out = append(out, offer)
		}
	}
	return out, nil
}

func betterFor(ctx context.Context, st *store.Store, kept []store.KeptCopy, prefs score.Preferences, dismissed string, scopeFor ScopeFor) (BetterCopy, bool) {
	animeID := kept[0].AnimeID
	picks, err := st.SeaDexBests(ctx, animeID)
	if err != nil || len(picks) == 0 {
		return BetterCopy{}, false
	}
	var scope torrent.Scope
	if scopeFor != nil {
		scope = scopeFor(ctx, animeID)
	}
	queued, _ := st.QueuedPins(ctx, animeID)
	turnedDown := strings.Split(dismissed, ",")

	isBest := map[string]bool{}
	parsed := make([]seaPick, len(picks))
	for i, p := range picks {
		isBest[strings.ToLower(p.InfoHash)] = true
		files, rel := pickFiles(p)
		parsed[i] = seaPick{SeaDexPick: p, files: files, rel: rel}
	}
	numbers := map[string][]int{}
	keptRank := 0.0
	heldGroups := map[string]bool{}
	for _, k := range kept {
		rel := keptRelease(k)
		keptRank = max(keptRank, score.ResolutionRank(rel.Resolution, prefs))
		if rel.Group != "" {
			heldGroups[strings.ToLower(rel.Group)] = true
		}
		if n, err := strconv.Atoi(k.EpKey); err == nil && numbers[k.EpKey] == nil {
			numbers[k.EpKey] = fileNumbers(ctx, st, animeID, n)
		}
	}

	var open []store.KeptCopy
	for _, k := range kept {
		if len(numbers[k.EpKey]) == 0 || isBest[strings.ToLower(k.InfoHash)] || isBest[queued[k.EpKey]] {
			continue
		}
		if alreadyBest(k, parsed, numbers[k.EpKey], scope, prefs) {
			continue
		}
		open = append(open, k)
	}
	if len(open) == 0 {
		return BetterCopy{}, false
	}

	offers := map[string]*BetterCopy{}
	all := map[string][]string{}
	var keys []string
	for _, p := range parsed {
		hash := strings.ToLower(p.InfoHash)
		if !strings.EqualFold(p.Tracker, "Nyaa") || slices.Contains(turnedDown, hash) {
			continue
		}
		files, rel := p.files, p.rel
		rank := score.ResolutionRank(rel.Resolution, prefs)
		if rank < keptRank {
			continue
		}
		key := strings.ToLower(p.Group) + "\x00" + rel.Resolution + "\x00" + strconv.FormatBool(p.DualAudio)
		all[key] = append(all[key], hash)
		o := offers[key]
		for _, k := range open {
			if o != nil && o.hashes[k.EpKey] != "" {
				continue
			}
			nums := numbers[k.EpKey]
			f, _, ok := torrent.PickInPack(files, nums[0], nums, scope)
			if !ok {
				continue
			}
			if o == nil {
				name := fmt.Sprintf("[%s] %s %s", p.Group, rel.Resolution, rel.Source)
				if p.DualAudio {
					name += " Dual audio"
				}
				o = &BetterCopy{
					AnimeID: animeID, Title: kept[0].Title, Cover: kept[0].Cover,
					Group: p.Group, Resolution: rel.Resolution, Source: rel.Source, DualAudio: p.DualAudio, URL: p.URL,
					Release: strings.Join(strings.Fields(name), " "),
					hashes:  map[string]string{}, rank: rank, held: heldGroups[strings.ToLower(p.Group)],
				}
				offers[key] = o
				keys = append(keys, key)
			}
			o.hashes[k.EpKey] = hash
			o.Episodes = append(o.Episodes, k.EpKey)
			o.Size += f.Length
		}
	}

	var best *BetterCopy
	for _, key := range keys {
		o := offers[key]
		o.all = all[key]
		if best == nil || preferOffer(*o, *best, prefs) {
			best = o
		}
	}
	if best == nil {
		return BetterCopy{}, false
	}
	slices.SortFunc(best.Episodes, func(a, b string) int {
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)
		return x - y
	})
	best.InfoHash = best.hashes[best.Episodes[0]]
	for _, k := range kept {
		if k.EpKey == best.Episodes[0] {
			best.Kept = k.Name
			break
		}
	}
	return *best, true
}

func preferOffer(a, b BetterCopy, prefs score.Preferences) bool {
	if len(a.Episodes) != len(b.Episodes) {
		return len(a.Episodes) > len(b.Episodes)
	}
	if a.rank != b.rank {
		return a.rank > b.rank
	}
	if a.held != b.held {
		return a.held
	}
	if a.DualAudio != b.DualAudio {
		return a.DualAudio == (prefs.Audio == "dub")
	}
	return false
}

type seaPick struct {
	store.SeaDexPick
	files []torrent.File
	rel   parse.Release
}

func alreadyBest(k store.KeptCopy, picks []seaPick, nums []int, scope torrent.Scope, prefs score.Preferences) bool {
	mine := keptRelease(k)
	if mine.Group == "" {
		return false
	}
	myRank := score.ResolutionRank(mine.Resolution, prefs)
	sameGroup := false
	for _, p := range picks {
		if !strings.EqualFold(p.Group, mine.Group) {
			continue
		}
		sameGroup = true
		if score.ResolutionRank(p.rel.Resolution, prefs) > myRank ||
			(mine.Source != "" && sourceRank(p.rel.Source) > sourceRank(mine.Source)) ||
			(prefs.Audio == "dub" && p.DualAudio && !mine.DualAudio) {
			return false
		}
		if f, _, ok := torrent.PickInPack(p.files, nums[0], nums, scope); ok && parse.Parse(parse.BaseName(f.Name)).Version > mine.Version {
			return false
		}
	}
	return sameGroup
}

func sourceRank(source string) int {
	switch strings.ToLower(source) {
	case "bdremux":
		return 3
	case "bd":
		return 2
	case "web":
		return 1
	}
	return 0
}

func keptRelease(k store.KeptCopy) parse.Release {
	rel := parse.Parse(parse.BaseName(k.Path))
	whole := parse.Parse(k.Name)
	if rel.Group == "" {
		rel.Group = whole.Group
	}
	if rel.Resolution == "" {
		rel.Resolution = whole.Resolution
	}
	if rel.Source == "" {
		rel.Source = whole.Source
	}
	rel.DualAudio = rel.DualAudio || whole.DualAudio
	return rel
}

func pickFiles(p store.SeaDexPick) ([]torrent.File, parse.Release) {
	files := make([]torrent.File, len(p.Files))
	var rel parse.Release
	for i, f := range p.Files {
		files[i] = torrent.File{Name: f.Name, Length: f.Length}
		if rel.Resolution == "" {
			rel = parse.Parse(parse.BaseName(f.Name))
		}
	}
	return files, rel
}

func fileNumbers(ctx context.Context, st *store.Store, animeID, episode int) []int {
	out := []int{episode}
	alias, _ := st.EpisodeAlias(ctx, animeID, episode)
	if alias.Tvdb > episode {
		out = append(out, alias.Tvdb)
	}
	if alias.Absolute > 0 {
		out = append(out, alias.Absolute)
	}
	return out
}

func offerWith(ctx context.Context, st *store.Store, scope ScopeFor, animeID int, infoHash string) (BetterCopy, bool, error) {
	offers, err := BetterCopies(ctx, st, scope, animeID)
	if err != nil {
		return BetterCopy{}, false, err
	}
	hash := strings.ToLower(infoHash)
	for _, o := range offers {
		for _, h := range o.hashes {
			if h == hash {
				return o, true, nil
			}
		}
	}
	return BetterCopy{}, false, nil
}

func ReplaceWithBetter(ctx context.Context, st *store.Store, scope ScopeFor, animeID int, infoHash string) (int, error) {
	o, ok, err := offerWith(ctx, st, scope, animeID, infoHash)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("that release is no longer offered for this show")
	}
	var queued int
	for _, ep := range o.Episodes {
		n, _ := strconv.Atoi(ep)
		if err := st.EnqueueRelease(ctx, animeID, 0, n, o.hashes[ep], o.Release); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

func DismissBetter(ctx context.Context, st *store.Store, scope ScopeFor, animeID int, infoHash string) error {
	hashes := []string{strings.ToLower(infoHash)}
	if o, ok, err := offerWith(ctx, st, scope, animeID, infoHash); err == nil && ok {
		for _, h := range o.all {
			if !slices.Contains(hashes, h) {
				hashes = append(hashes, h)
			}
		}
	}
	prefs, err := st.Prefs(ctx, animeID)
	if err != nil {
		return err
	}
	list := strings.FieldsFunc(prefs.String(dismissedPref), func(r rune) bool { return r == ',' })
	for _, h := range hashes {
		if !slices.Contains(list, h) {
			list = append(list, h)
		}
	}
	list = list[max(0, len(list)-maxDismissed):]
	return st.SetAnimePref(ctx, animeID, dismissedPref, strings.Join(list, ","))
}

const maxDismissed = 200
