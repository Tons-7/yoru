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

// BetterCopy is SeaDex's best release for a show whose kept episodes are another copy; never taken alone.
type BetterCopy struct {
	AnimeID int     `json:"animeId"`
	Title   string  `json:"title"`
	Cover   *string `json:"cover,omitempty"`
	// Kept is one kept episode's release, to say what would be replaced.
	Kept string `json:"kept"`
	// The kept episodes the offer covers, and its own files' size for them.
	Episodes []string `json:"episodes"`
	Size     int64    `json:"size"`

	InfoHash   string `json:"infoHash"`
	Group      string `json:"group"`
	Resolution string `json:"resolution"`
	Source     string `json:"source,omitempty"`
	URL        string `json:"url,omitempty"`
	// Release names the offer for the queue: the group, resolution and source.
	Release string `json:"release"`
}

// dismissedPref holds, per show, the offers the user turned down (comma-separated hashes), so none comes back.
const dismissedPref = "seadex.dismissed"

// BetterCopies lists SeaDex's best release per show (or one, animeID nonzero) where the kept episodes are
// another copy: only from a public tracker, at the kept copies' resolution or higher.
func BetterCopies(ctx context.Context, st *store.Store, animeID int) ([]BetterCopy, error) {
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
		if offer, ok := betterFor(ctx, st, byShow[id], Preferences(prefs), prefs.String(dismissedPref)); ok {
			out = append(out, offer)
		}
	}
	return out, nil
}

func betterFor(ctx context.Context, st *store.Store, kept []store.KeptCopy, prefs score.Preferences, dismissed string) (BetterCopy, bool) {
	picks, err := st.SeaDexBests(ctx, kept[0].AnimeID)
	if err != nil || len(picks) == 0 {
		return BetterCopy{}, false
	}
	held := map[string]bool{}
	keptRank := 0.0
	// The numbers each kept episode's file may carry, read once for every pick.
	numbers := map[string][]int{}
	for _, k := range kept {
		held[strings.ToLower(k.InfoHash)] = true
		keptRank = max(keptRank, score.ResolutionRank(parse.Parse(k.Name).Resolution, prefs))
		if n, err := strconv.Atoi(k.EpKey); err == nil && numbers[k.EpKey] == nil {
			numbers[k.EpKey] = fileNumbers(ctx, st, k.AnimeID, n)
		}
	}

	var best BetterCopy
	for _, p := range picks {
		// Already the copy kept, a private tracker's, or one turned down.
		if held[strings.ToLower(p.InfoHash)] || !strings.EqualFold(p.Tracker, "Nyaa") ||
			slices.Contains(strings.Split(dismissed, ","), strings.ToLower(p.InfoHash)) {
			continue
		}
		episodes, size, rel := pickCovers(p, kept, numbers)
		// Never a lower resolution: what's kept is at least as sharp.
		if len(episodes) == 0 || score.ResolutionRank(rel.Resolution, prefs) < keptRank {
			continue
		}
		if len(episodes) > len(best.Episodes) {
			best = BetterCopy{
				AnimeID: kept[0].AnimeID, Title: kept[0].Title, Cover: kept[0].Cover, Kept: kept[0].Name,
				Episodes: episodes, Size: size, InfoHash: strings.ToLower(p.InfoHash), Group: p.Group,
				Resolution: rel.Resolution, Source: rel.Source, URL: p.URL,
				Release: strings.TrimSpace(fmt.Sprintf("[%s] %s %s", p.Group, rel.Resolution, rel.Source)),
			}
		}
	}
	return best, len(best.Episodes) > 0
}

// pickCovers is the kept episodes a pick's files hold, their size, and the release as its files name it. Files
// are found as the download finds them: a pack counting on through the seasons by their absolute numbers.
func pickCovers(
	p store.SeaDexPick, kept []store.KeptCopy, numbers map[string][]int,
) (episodes []string, size int64, rel parse.Release) {
	files := make([]torrent.File, len(p.Files))
	for i, f := range p.Files {
		files[i] = torrent.File{Name: f.Name, Length: f.Length}
		if rel.Resolution == "" {
			rel = parse.Parse(f.Name)
		}
	}
	for _, k := range kept {
		nums := numbers[k.EpKey]
		if len(nums) == 0 || slices.Contains(episodes, k.EpKey) {
			continue
		}
		if f, _, ok := torrent.PickNumbered(files, nums[0], nums); ok {
			episodes = append(episodes, k.EpKey)
			size += f.Length
		}
	}
	slices.SortFunc(episodes, func(a, b string) int {
		x, _ := strconv.Atoi(a)
		y, _ := strconv.Atoi(b)
		return x - y
	})
	return episodes, size, rel
}

// fileNumbers are the counts an episode's file may carry, its own first; TheTVDB's only when it counts on
// through the cours, as numbersFor takes it.
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

// ReplaceWithBetter queues the show's covered kept episodes from the offered release. Each old copy stays
// watchable until its replacement is complete, as any superseded release does.
func ReplaceWithBetter(ctx context.Context, st *store.Store, animeID int, infoHash string) (int, error) {
	offers, err := BetterCopies(ctx, st, animeID)
	if err != nil {
		return 0, err
	}
	i := slices.IndexFunc(offers, func(o BetterCopy) bool { return strings.EqualFold(o.InfoHash, infoHash) })
	if i < 0 {
		return 0, fmt.Errorf("that release is no longer offered for this show")
	}
	o := offers[i]
	var queued int
	for _, ep := range o.Episodes {
		n, _ := strconv.Atoi(ep)
		if err := st.EnqueueRelease(ctx, animeID, 0, n, o.InfoHash, o.Release); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

// DismissBetter stops offering this release for the show; earlier ones stay turned down.
func DismissBetter(ctx context.Context, st *store.Store, animeID int, infoHash string) error {
	prefs, err := st.Prefs(ctx, animeID)
	if err != nil {
		return err
	}
	hash := strings.ToLower(infoHash)
	list := strings.FieldsFunc(prefs.String(dismissedPref), func(r rune) bool { return r == ',' })
	if !slices.Contains(list, hash) {
		// SeaDex lists a handful per show; the cap only bounds a pref nobody prunes.
		list = append(list, hash)[max(0, len(list)+1-maxDismissed):]
	}
	return st.SetAnimePref(ctx, animeID, dismissedPref, strings.Join(list, ","))
}

const maxDismissed = 20
