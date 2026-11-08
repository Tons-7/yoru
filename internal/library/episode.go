package library

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"yoru/internal/film"
	"yoru/internal/indexer"
	"yoru/internal/parse"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

type Finder struct {
	store   *store.Store
	indexer indexer.Source

	adult indexer.Source
	log   *slog.Logger

	hwTranscode atomic.Bool

	hydrate func(ctx context.Context, ids []int) (int, error)

	franchise func(ctx context.Context, animeID int) error

	askedMu sync.Mutex
	asked   map[[2]int]asked

	seriesMu   sync.Mutex
	seriesSeen map[int]seriesMemo
}

type seriesMemo struct {
	series *filmSeries
	at     time.Time
}

type asked struct {
	words  string
	pasted *indexer.Torrent
	until  time.Time
}

const askedFor = 10 * time.Minute

const Pasted = "pasted"

func (f *Finder) Ask(animeID, episode int, words string, pasted *indexer.Torrent) {
	f.askedMu.Lock()
	defer f.askedMu.Unlock()
	if f.asked == nil {
		f.asked = map[[2]int]asked{}
	}
	key := [2]int{animeID, episode}
	a := f.asked[key]
	if time.Now().After(a.until) {
		a = asked{}
	}
	if words = strings.TrimSpace(words); words != "" {
		a.words = words
	}
	if pasted != nil {
		a.pasted = pasted
	}
	a.until = time.Now().Add(askedFor)
	f.asked[key] = a
}

func (f *Finder) askedOf(animeID, episode int) asked {
	f.askedMu.Lock()
	defer f.askedMu.Unlock()
	a := f.asked[[2]int{animeID, episode}]
	if time.Now().After(a.until) {
		return asked{}
	}
	return a
}

func (f *Finder) WithFranchise(ensure func(ctx context.Context, animeID int) error) *Finder {
	f.franchise = ensure
	return f
}

func (f *Finder) WithHydrator(h func(ctx context.Context, ids []int) (int, error)) *Finder {
	f.hydrate = h
	return f
}

func NewFinder(s *store.Store, idx indexer.Source, log *slog.Logger) *Finder {
	return &Finder{store: s, indexer: idx, log: log}
}

var ErrNoIndexers = errors.New("no release sites yet: add one under Settings › Downloads › Release sites")

func (f *Finder) WithHardwareTranscode(ok bool) *Finder {
	f.hwTranscode.Store(ok)
	return f
}

func (f *Finder) WithAdultIndexer(idx indexer.Source) *Finder {
	f.adult = idx
	return f
}

type Request struct {
	AnimeID int
	Episode int
	Season  int

	Alias store.EpisodeAlias

	Part int

	Numerals []int

	Counted int

	Apart, Film bool

	Subtitle [][]string

	Shows []string

	Cour Cour

	Prefs score.Preferences
}

type Cour struct {
	Own      []string
	Siblings []string

	Later bool

	Bases [][]string
}

type Candidates struct {
	Queries []string       `json:"queries"`
	Results []score.Result `json:"results"`
	Best    *score.Result  `json:"best,omitempty"`
	req     Request
}

func (f *Finder) learnRelations(ctx context.Context, animeID int) {
	if f.franchise == nil {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, relationsDeadline)
	defer cancel()
	if err := f.franchise(bounded, animeID); err != nil {
		f.log.Warn("franchise for matching", "anime", animeID, "err", err)
	}
}

func (f *Finder) Find(ctx context.Context, req Request) (Candidates, error) {
	f.learnRelations(ctx, req.AnimeID)

	titles, err := f.store.SearchTitles(ctx, req.AnimeID)
	if err != nil {
		return Candidates{}, err
	}
	if len(titles) == 0 && f.hydrate != nil {
		if _, err := f.hydrate(ctx, []int{req.AnimeID}); err != nil {
			f.log.Warn("fetch unknown show", "anime", req.AnimeID, "err", err)
		}
		if titles, err = f.store.SearchTitles(ctx, req.AnimeID); err != nil {
			return Candidates{}, err
		}
	}
	total, _ := f.store.EpisodeCount(ctx, req.AnimeID)
	runtime := f.store.EpisodeRuntime(ctx, req.AnimeID)
	bestByHash, bestByGroup, _ := f.store.SeaDexFor(ctx, req.AnimeID)

	groups, _ := f.store.BestGroups(ctx, req.AnimeID)
	english, _ := f.store.EnglishTitle(ctx, req.AnimeID)
	req = f.numbering(ctx, req, titles, english)
	req.Prefs.HardwareTranscode = f.hwTranscode.Load()

	var series *filmSeries
	if req.Episode <= 1 {
		series = f.filmSeries(ctx, req.AnimeID)
		f.rememberSeries(req.AnimeID, series)
	}
	tvdb := req.Alias.Tvdb
	if tvdb < req.Episode {
		tvdb = 0
	}
	queries := searchTerms(titles, english, req.Episode, tvdb, req.Alias.Absolute, groups,
		req.Prefs.Audio == "dub", series.queries()...)
	ask := f.askedOf(req.AnimeID, req.Episode)
	if ask.words != "" {
		queries = append([]string{ask.words}, queries...)
	}

	source := f.indexer
	adult, adultErr := f.store.IsAdult(ctx, req.AnimeID)
	if adult && indexer.Configured(f.adult) {
		source = f.adult
		f.log.Info("searching the adult tracker", "anime", req.AnimeID)
	} else if adultErr != nil {
		f.log.Warn("adult flag unreadable", "anime", req.AnimeID, "err", adultErr)
	}
	var batches []searchBatch
	switch {
	case indexer.Configured(source):
		batches = f.searchAll(ctx, source, queries)
		if err := allFailed(batches); err != nil {
			if ask.pasted == nil {
				return Candidates{}, err
			}
			break
		}
		if countResults(batches) == 0 {
			if short := shortTerms(titles, english); len(short) > 0 {
				batches = append(batches, f.searchAll(ctx, source, short)...)
			}
		}
	case ask.pasted == nil:
		return Candidates{}, ErrNoIndexers
	}
	if ask.pasted != nil {
		batches = append(batches, searchBatch{query: "pasted magnet", results: []indexer.Torrent{*ask.pasted}})
	}

	identity := newShowIdentity(append(append([]string{}, titles...), english))
	others := otherEntries(req, append(append([]string{}, titles...), english))
	held, _ := f.store.ShowTorrents(ctx, req.AnimeID)

	seen := map[string]struct{}{}
	var cands []score.Candidate
	var used []string

	for _, batch := range batches {
		results := batch.results
		used = append(used, batch.query)
		if batch.err != nil {
			f.log.Warn("indexer search failed", "query", batch.query, "err", batch.err)
			continue
		}

		for _, t := range results {
			if _, dup := seen[t.InfoHash]; dup {
				continue
			}
			seen[t.InfoHash] = struct{}{}

			rel := parse.Parse(t.Title)
			pasted := t.Indexer == Pasted
			c := score.Candidate{
				Torrent: t, Release: rel, TotalEpisodes: total, RuntimeMinutes: runtime,
				WrongShow: !pasted && !identity.matches(rel, t.Title),
				ByHand:    pasted,
				Season:    req.Season,
				Counted:   req.Counted,
				Others:    others,
				Held:      held[strings.ToLower(t.InfoHash)],
			}
			if series != nil {
				c.Film = &series.key
				c.WrongShow = c.WrongShow || (!pasted && !series.named(t.Title))
				switch series.key.Match(t.Title) {
				case film.Ours:
					c.Confirmed = series.key.Named(t.Title)
				case film.Other:
					c.WrongFilm = true
				default:
					c.Release.Batch = true
					c.Confirmed = wholeSeries.MatchString(t.Title)
				}
			} else {
				if !pasted && !verifies(rel, req) {
					continue
				}
				c.Confirmed, c.Numbers, c.Episode = confirms(rel, req), numbersFor(rel, req), req.Episode
			}
			if info, ok := bestByHash[strings.ToLower(t.InfoHash)]; ok {
				c.SeaDexBest, c.SeaDexGroup, c.SeaDexListed = info.IsBest, true, true
				c.WrongShow = false
				c.SeaDexNotes, c.SeaDexCompare, c.SeaDexIncomplete = info.Notes, info.Comparison, info.Incomplete
			} else if info, ok := bestByGroup[strings.ToLower(rel.Group)]; ok {
				c.SeaDexGroup = true
				c.SeaDexNotes, c.SeaDexCompare, c.SeaDexIncomplete = info.Notes, info.Comparison, info.Incomplete
			}
			cands = append(cands, c)
		}
	}

	out := Candidates{Queries: used, Results: score.Rank(cands, req.Prefs), req: req}
	if len(out.Results) > 0 && out.Results[0].AutoPick {
		best := out.Results[0]
		out.Best = &best
	}
	if i := slices.IndexFunc(out.Results, func(r score.Result) bool { return r.Torrent.Indexer == Pasted }); i > 0 {
		pasted := out.Results[i]
		out.Results = append([]score.Result{pasted}, slices.Delete(out.Results, i, i+1)...)
	}
	return out, nil
}

func allFailed(batches []searchBatch) error {
	var errs []error
	for _, b := range batches {
		if b.err == nil {
			return nil
		}
		errs = append(errs, b.err)
	}
	if len(errs) == 0 {
		return nil
	}
	return searchFailed{errs}
}

type searchFailed struct{ errs []error }

func (e searchFailed) Unwrap() []error { return e.errs }

func (e searchFailed) Error() string {
	var parts []string
	for _, err := range flatten(e.errs) {
		if p := siteProblem(err); !slices.Contains(parts, p) {
			parts = append(parts, p)
		}
	}
	return "couldn't search for releases: " + strings.Join(parts, "; ")
}

func flatten(errs []error) []error {
	var out []error
	for _, err := range errs {
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			out = append(out, flatten(joined.Unwrap())...)
		} else {
			out = append(out, err)
		}
	}
	return out
}

func siteProblem(err error) string {
	site, rest, ok := strings.Cut(err.Error(), ": ")
	if !ok {
		return err.Error()
	}
	var timeout interface{ Timeout() bool }
	var dns *net.DNSError
	switch {
	case errors.As(err, &timeout) && timeout.Timeout():
		return site + " timed out"
	case errors.As(err, &dns):
		return site + " could not be found"
	case errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(rest, "refused"):
		return site + " refused the connection"
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return site + " is unreachable"
	}
	if len(rest) > 80 {
		rest = rest[:80] + "…"
	}
	return site + ": " + rest
}

func countResults(batches []searchBatch) int {
	var n int
	for _, b := range batches {
		n += len(b.results)
	}
	return n
}

func (f *Finder) EpisodeNumbers(ctx context.Context, animeID int) ([]int, error) {
	f.learnRelations(ctx, animeID)
	if f.filmSeries(ctx, animeID) != nil {
		return nil, nil
	}
	found, err := f.Find(ctx, Request{AnimeID: animeID})
	if err != nil {
		return nil, err
	}

	seen := map[int]struct{}{}
	for _, r := range found.Results {
		if r.WrongShow {
			continue
		}
		rel := r.Release
		if bare(rel, found.req) {
			continue
		}
		switch {
		case rel.EpisodeEnd > rel.Episode && rel.Episode > 0:
			for n := rel.Episode; n <= rel.EpisodeEnd && n-rel.Episode < 2000; n++ {
				seen[n] = struct{}{}
			}
		case rel.Episode > 0:
			seen[rel.Episode] = struct{}{}
		}
	}

	limit, firm := f.store.EpisodeLimit(ctx, animeID)
	offsets := f.earlierCounts(ctx, animeID)
	out := make([]int, 0, len(seen))
	for n := range seen {
		if n = ownNumber(n, limit, store.Slack(firm), offsets); n > 0 {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

func ownNumber(n, limit, slack int, offsets []int) int {
	if limit <= 0 || n <= limit {
		return n
	}
	for _, off := range offsets {
		if own := n - off; own >= 1 && own <= limit {
			return own
		}
	}
	if n <= limit+slack {
		return n
	}
	return 0
}

func (f *Finder) earlierCounts(ctx context.Context, animeID int) []int {
	franchise, err := f.store.Franchise(ctx, animeID)
	if err != nil {
		return nil
	}
	var earlier []store.Season
	for _, s := range franchise.Seasons {
		if s.ID == animeID {
			break
		}
		if s.Format != nil && series(s.Format) {
			earlier = append(earlier, s)
		}
	}
	var offsets []int
	sum := 0
	for i := len(earlier) - 1; i >= 0; i-- {
		if earlier[i].Episodes == nil || *earlier[i].Episodes <= 0 {
			break
		}
		sum += *earlier[i].Episodes
		offsets = append(offsets, sum)
	}
	return offsets
}

func shortTerms(titles []string, english string) []string {
	var out []string
	seen := map[string]struct{}{}

	add := func(s string) {
		s = cleanQuery(s)
		if utf8.RuneCountInString(s) < 4 || len(out) >= 3 || !mostlyLatin(s) {
			return
		}
		key := strings.ToLower(s)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}

	for _, title := range append(append([]string{}, titles...), english) {
		words := significantWords(title)
		if len(words) == 0 {
			continue
		}
		longest := words[0]
		for _, w := range words {
			if len(w) > len(longest) {
				longest = w
			}
		}
		add(longest)
		if len(words) >= 2 {
			add(words[0] + " " + words[1])
		}
	}
	return out
}

var wordSplit = regexp.MustCompile(`[^\p{L}\p{N}]+`)

var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "of": true, "and": true, "to": true,
	"no": true, "ga": true, "wa": true, "ni": true, "de": true, "wo": true,
	"season": true, "part": true, "movie": true, "tv": true,
}

func significantWords(title string) []string {
	var out []string
	for _, w := range wordSplit.Split(title, -1) {
		if utf8.RuneCountInString(w) < 4 || stopWords[strings.ToLower(w)] {
			continue
		}
		if _, err := strconv.Atoi(w); err == nil {
			continue
		}
		out = append(out, w)
	}
	return out
}

func (f *Finder) numbering(ctx context.Context, req Request, titles []string, english string) Request {
	for _, t := range append([]string{english}, titles...) {
		if n := parse.PartOf(t); n > 0 {
			req.Part = n
			break
		}
	}
	for _, t := range append([]string{english}, titles...) {
		for _, name := range []string{t, baseTitle(t)} {
			if n, _ := parse.NumeralSeason(name); n > 0 && !slices.Contains(req.Numerals, n) {
				req.Numerals = append(req.Numerals, n)
			}
		}
	}

	if req.Season == 0 {
		req.Season = f.defaultSeason(ctx, req.AnimeID, append([]string{english}, titles...))
		if req.Season == 1 {
			req.Counted = f.franchiseSeason(ctx, req.AnimeID)
		}
	}

	format := f.store.Format(ctx, req.AnimeID)
	req.Apart = !series(format)
	if req.Apart {
		total, _ := f.store.EpisodeCount(ctx, req.AnimeID)
		req.Film = total == 1 || (total == 0 && strings.EqualFold(*format, "MOVIE"))
		if req.Film && !strings.EqualFold(*format, "MOVIE") {
			req.Subtitle = subtitleWords(append([]string{english}, titles...))
		}
	}
	if req.Film {
		req.Shows = f.showNames(ctx, req.AnimeID, append([]string{english}, titles...))
	}

	req.Alias, _ = f.store.EpisodeAlias(ctx, req.AnimeID, req.Episode)
	if !req.Apart && (!req.Alias.StatedTvdb || !req.Alias.StatedAbsolute) {
		derived := f.derivedAlias(ctx, req, titles, english)
		if !req.Alias.StatedTvdb {
			req.Alias.Tvdb = derived.Tvdb
		}
		if !req.Alias.StatedAbsolute {
			req.Alias.Absolute = derived.Absolute
		}
	}
	req.Cour = f.cour(ctx, req, titles, english)
	return req
}

func (f *Finder) PackScope(ctx context.Context, animeID, episode, season int) torrent.Scope {
	titles, _ := f.store.SearchTitles(ctx, animeID)
	english, _ := f.store.EnglishTitle(ctx, animeID)
	req := f.numbering(ctx, Request{AnimeID: animeID, Episode: episode, Season: season}, titles, english)
	return torrent.Scope{Season: req.Season, Counted: req.Counted, Others: otherEntries(req, append(append([]string{}, titles...), english))}
}

func (f *Finder) defaultSeason(ctx context.Context, animeID int, titles []string) int {
	for _, t := range titles {
		if n := parse.SeasonOf(t); n > 0 {
			return n
		}
	}
	for _, t := range titles {
		n, roman := parse.NumeralSeason(t)
		if n == 0 {
			continue
		}
		if roman || f.laterSeason(ctx, animeID) {
			return n
		}
	}
	return 1
}

func (f *Finder) franchiseSeason(ctx context.Context, animeID int) int {
	franchise, err := f.store.Franchise(ctx, animeID)
	if err != nil {
		return 0
	}
	season := 0
	for _, s := range franchise.Seasons {
		if !series(s.Format) {
			if s.ID == animeID {
				return 0
			}
			continue
		}
		english := ""
		if s.English != nil {
			english = *s.English
		}
		if season == 0 || max(parse.PartOf(s.Romaji), parse.PartOf(english)) < 2 {
			season++
		}
		if s.ID == animeID {
			return season
		}
	}
	return 0
}

func (f *Finder) laterSeason(ctx context.Context, animeID int) bool {
	franchise, err := f.store.Franchise(ctx, animeID)
	if err != nil {
		return false
	}
	for _, s := range franchise.Seasons {
		if s.ID == animeID {
			return s.Ordinal > 1
		}
	}
	return false
}

func (f *Finder) derivedAlias(ctx context.Context, req Request, titles []string, english string) store.EpisodeAlias {
	if req.Episode <= 0 {
		return store.EpisodeAlias{}
	}
	franchise, err := f.store.Franchise(ctx, req.AnimeID)
	if err != nil || len(franchise.Seasons) < 2 {
		return store.EpisodeAlias{}
	}

	bases := map[string]bool{}
	for _, t := range append([]string{english}, titles...) {
		if b := baseTitle(t); b != "" {
			bases[cleanQuery(b)] = true
		}
	}

	var absolute, run int
	var counted, split bool
	for _, s := range franchise.Seasons {
		if s.ID == req.AnimeID {
			break
		}
		if !series(s.Format) {
			continue
		}
		if s.Episodes == nil || *s.Episodes <= 0 {
			return store.EpisodeAlias{}
		}
		absolute += *s.Episodes

		names, _ := f.store.SearchTitles(ctx, s.ID)
		run += *s.Episodes
		if namedBy(bases, append(names, s.Romaji)) {
			counted = true
			continue
		}
		split = split || counted
		run = 0
	}

	alias := store.EpisodeAlias{}
	if run > 0 && !split {
		alias.Tvdb = run + req.Episode
	}
	if absolute > 0 {
		alias.Absolute = absolute + req.Episode
	}
	return alias
}

func namedBy(bases map[string]bool, names []string) bool {
	for _, n := range names {
		if n == "" {
			continue
		}
		if bases[cleanQuery(n)] {
			return true
		}
		if b := baseTitle(n); b != "" && bases[cleanQuery(b)] {
			return true
		}
	}
	return false
}

func (f *Finder) cour(ctx context.Context, req Request, titles []string, english string) Cour {
	all := append([]string{english}, titles...)
	mine := wordSet(all...)
	own := wordSet(all...)
	c := Cour{
		Later: req.Season > 1 || req.Part > 1 || req.Alias.Tvdb > req.Episode,
	}
	for _, t := range all {
		base := baseTitle(t)
		if base == "" {
			base = t
		}
		if words := significantWords(base); len(words) > 0 {
			c.Bases = append(c.Bases, lower(words))
		}
	}

	franchise, err := f.store.Franchise(ctx, req.AnimeID)
	if err != nil {
		return c
	}
	ordinal := 0
	for _, s := range franchise.Seasons {
		if s.ID == req.AnimeID {
			ordinal = s.Ordinal
		}
	}

	count := map[string]int{}
	for _, s := range franchise.Seasons {
		if s.ID == req.AnimeID {
			continue
		}
		names, _ := f.store.SearchTitles(ctx, s.ID)
		theirs := wordSet(names...)
		for w := range theirs {
			if !mine[w] {
				count[w]++
			}
		}
		if s.Ordinal < ordinal && series(s.Format) {
			for _, name := range names {
				for _, t := range all {
					if b := baseTitle(t); b != "" && strings.EqualFold(cleanQuery(b), cleanQuery(name)) {
						c.Later = true
					}
				}
			}
		}
		for w := range theirs {
			delete(own, w)
		}
	}
	for w, n := range count {
		if n == 1 {
			c.Siblings = append(c.Siblings, w)
		}
	}
	for w := range own {
		c.Own = append(c.Own, w)
	}
	slices.Sort(c.Siblings)
	slices.Sort(c.Own)
	return c
}

func (f *Finder) showNames(ctx context.Context, animeID int, own []string) []string {
	mine := map[string]bool{}
	for _, t := range own {
		mine[releaseName(t)] = true
	}
	var out []string
	for _, m := range f.seriesMembers(ctx, animeID) {
		if m.ID == animeID || !series(m.Format) {
			continue
		}
		names, _ := f.store.SearchTitles(ctx, m.ID)
		names = append(names, m.Romaji)
		if m.English != nil {
			names = append(names, *m.English)
		}
		for _, n := range names {
			for _, k := range []string{releaseName(n), releaseName(baseTitle(n))} {
				if k != "" && !mine[k] && !slices.Contains(out, k) {
					out = append(out, k)
				}
			}
		}
	}
	return out
}

func releaseName(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(cleanQuery(title)), " "))
}

func series(format *string) bool {
	if format == nil {
		return true
	}
	switch strings.ToUpper(*format) {
	case "MOVIE", "SPECIAL", "OVA", "MUSIC":
		return false
	}
	return true
}

var notAnEpisode = []string{"movie", "movies", "film", "films", "ova", "ovas", "oad", "oads", "special", "specials", "recap"}

func otherEntries(req Request, titles []string) []string {
	own := map[string]bool{}
	for _, t := range titles {
		for _, w := range wordSplit.Split(strings.ToLower(t), -1) {
			own[w] = true
		}
	}
	var out []string
	for _, w := range req.Cour.Siblings {
		if !own[w] {
			out = append(out, w)
		}
	}
	if !req.Apart {
		for _, w := range notAnEpisode {
			if !own[w] {
				out = append(out, w)
			}
		}
	}
	return out
}

func subtitleWords(titles []string) [][]string {
	var heads, tails []string
	for _, t := range titles {
		cut := false
		for _, sep := range []string{" - ", " – ", ": "} {
			if i := strings.LastIndex(t, sep); i > 8 {
				heads = append(heads, t[:i])
				tails = append(tails, t[i+len(sep):])
				cut = true
				break
			}
		}
		if !cut {
			heads = append(heads, t)
		}
	}
	shared := wordSet(heads...)
	var out [][]string
	for _, tail := range tails {
		var own []string
		for w := range wordSet(tail) {
			if !shared[w] {
				own = append(own, w)
			}
		}
		if len(own) > 0 {
			slices.Sort(own)
			out = append(out, own)
		}
	}
	return out
}

func namesAny(title string, subtitles [][]string) bool {
	have := wordSet(title)
	for _, words := range subtitles {
		all := true
		for _, w := range words {
			all = all && have[w]
		}
		if all {
			return true
		}
	}
	return false
}

func wordSet(titles ...string) map[string]bool {
	out := map[string]bool{}
	for _, t := range titles {
		for _, w := range significantWords(t) {
			out[strings.ToLower(w)] = true
		}
	}
	return out
}

func lower(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = strings.ToLower(w)
	}
	return out
}

type searchBatch struct {
	query   string
	results []indexer.Torrent
	err     error
}

const searchConcurrency = 4

func (f *Finder) searchAll(ctx context.Context, source indexer.Source, queries []string) []searchBatch {
	out := make([]searchBatch, len(queries))
	slots := make(chan struct{}, searchConcurrency)

	var wg sync.WaitGroup
	for i, q := range queries {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			results, err := source.Search(ctx, indexer.Query{Text: q})
			out[i] = searchBatch{query: q, results: results, err: err}
		})
	}
	wg.Wait()
	return out
}

func marked(rel parse.Release, req Request) (own, sibling bool) {
	words := wordSet(rel.Title)
	for _, w := range req.Cour.Siblings {
		sibling = sibling || words[w]
	}
	for _, w := range req.Cour.Own {
		own = own || words[w]
	}
	own = own || (req.Part > 0 && rel.Part == req.Part) ||
		(req.Season > 1 && (rel.Season == req.Season || numeralSeason(rel, req) == req.Season))
	return own, sibling
}

func numeralSeason(rel parse.Release, req Request) int {
	if rel.Season > 0 || rel.Part > 0 {
		return 0
	}
	n, roman := parse.NumeralSeason(rel.Title)
	if n < 2 || (!roman && slices.Contains(req.Numerals, n) && req.Season != n) {
		return 0
	}
	return n
}

func bare(rel parse.Release, req Request) bool {
	own, _ := marked(rel, req)
	return req.Cour.Later && !own
}

func numbersFor(rel parse.Release, req Request) []int {
	var out []int
	if !bare(rel, req) {
		out = append(out, req.Episode)
	}
	if tvdb := req.Alias.Tvdb; tvdb > 0 && tvdbCounts(rel, req) {
		out = append(out, tvdb)
	}
	if abs := req.Alias.Absolute; abs > 0 && !slices.Contains(out, abs) {
		out = append(out, abs)
	}
	return out
}

func tvdbCounts(rel parse.Release, req Request) bool {
	return req.Alias.Tvdb > req.Episode || (req.Alias.TvdbSeason > 0 && rel.Season == req.Alias.TvdbSeason)
}

func covers(rel parse.Release, n int) bool {
	return rel.Batch && rel.Episode <= n && (rel.EpisodeEnd == 0 || n <= rel.EpisodeEnd)
}

func namesShow(rel parse.Release, req Request) bool {
	words := wordSet(rel.Title)
	for _, base := range req.Cour.Bases {
		var hits int
		for _, w := range base {
			if words[w] {
				hits++
			}
		}
		if float64(hits)/float64(len(base)) >= 0.5 {
			return true
		}
	}
	return len(req.Cour.Bases) == 0
}

func names(rel parse.Release, req Request) bool {
	if namesShow(rel, req) {
		return true
	}
	words := wordSet(rel.Title)
	if len(words) == 0 {
		return false
	}
	for _, base := range req.Cour.Bases {
		all := true
		for w := range words {
			all = all && slices.Contains(base, w)
		}
		if all {
			return true
		}
	}
	return false
}

func broadcast(rel parse.Release, req Request) bool {
	return rel.Season > 1 && req.Cour.Later && names(rel, req)
}

func numbered(rel parse.Release, req Request) (stated, inBatch bool) {
	if !bare(rel, req) {
		stated = rel.Episode == req.Episode
		inBatch = covers(rel, req.Episode)
	}
	if tvdb := req.Alias.Tvdb; tvdb > 0 && tvdbCounts(rel, req) && (namesShow(rel, req) || broadcast(rel, req)) {
		stated = stated || rel.Episode == tvdb
		inBatch = inBatch || covers(rel, tvdb)
	}
	if abs := req.Alias.Absolute; abs > 0 && names(rel, req) {
		stated = stated || rel.Episode == abs
		inBatch = inBatch || covers(rel, abs)
	}
	return stated, inBatch
}

func confirms(rel parse.Release, req Request) bool {
	if req.Episode <= 0 {
		return false
	}
	if !rel.Batch && namesAny(rel.Raw, req.Subtitle) {
		return true
	}
	stated, inBatch := numbered(rel, req)
	if stated || (inBatch && rel.Episode > 0) {
		return true
	}
	return rel.Batch && rel.Episode == 0 && rel.Season > 0 && rel.Season == req.Season && !req.Cour.Later
}

func verifies(rel parse.Release, req Request) bool {
	own, sibling := marked(rel, req)
	if sibling && !own {
		return false
	}
	if req.Part > 0 && rel.Part > 0 && rel.Part != req.Part {
		return false
	}
	if len(req.Subtitle) > 0 && !rel.Batch {
		if namesAny(rel.Raw, req.Subtitle) {
			return true
		}
		if rel.Episode > 0 {
			return false
		}
	}
	if req.Film && rel.Episode > 0 && !rel.Batch &&
		(rel.Season > 0 || slices.Contains(req.Shows, releaseName(rel.Title))) {
		return false
	}

	if req.Episode > 0 {
		stated, inBatch := numbered(rel, req)

		unknownScope := rel.Episode == 0 && !rel.Batch &&
			len(numbersFor(rel, req)) > 0 && (own || namesShow(rel, req))

		if !stated && !inBatch && !unknownScope {
			return false
		}
	}
	if req.Alias.Tvdb > req.Episode && rel.Episode == req.Alias.Tvdb && broadcast(rel, req) {
		return true
	}
	if req.Season > 1 && rel.Season > 0 && rel.Season != req.Season {
		return false
	}
	if n := numeralSeason(rel, req); n > 0 && n != max(req.Season, 1) {
		return false
	}
	if req.Season == 1 && rel.Season > 1 && rel.Season != req.Counted {
		return false
	}
	return true
}

const maxSearchTerms = 8

var (
	queryPunct  = regexp.MustCompile(`[?!:;,"'’“”~*<>|/\\()\[\]{}]+`)
	querySeason = regexp.MustCompile(`(?i)\s*\b(?:S0*1|Season\s*1|1st\s+Season)\b`)
	querySpace  = regexp.MustCompile(`\s{2,}`)
)

func cleanQuery(title string) string {
	out := queryPunct.ReplaceAllString(title, " ")
	out = querySeason.ReplaceAllString(out, "")
	out = strings.ReplaceAll(out, ".", " ")
	out = querySpace.ReplaceAllString(out, " ")
	return strings.TrimSpace(strings.Trim(out, "-– "))
}

func searchTerms(
	titles []string, english string, episode, tvdb, absolute int, bestGroups []string, dub bool, series ...string,
) []string {
	var latin []string
	seen := map[string]struct{}{}

	add := func(t string) {
		t = cleanQuery(t)
		if len(t) < 3 || !mostlyLatin(t) {
			return
		}
		key := strings.ToLower(t)
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		latin = append(latin, t)
	}
	for _, t := range titles {
		add(t)
	}
	if len(latin) == 0 {
		return nil
	}

	terms := make([]string, 0, maxSearchTerms)
	push := func(t string) {
		if len(terms) < maxSearchTerms && !slices.ContainsFunc(terms, func(x string) bool { return strings.EqualFold(x, t) }) {
			terms = append(terms, t)
		}
	}

	for _, g := range bestGroups {
		if g = strings.TrimSpace(g); g != "" && mostlyLatin(g) {
			push(fmt.Sprintf("%s %s", g, shortest(spelledOut(latin))))
			break
		}
	}

	if episode > 0 {
		push(fmt.Sprintf("%s %02d", latin[0], episode))
		if dub {
			push(fmt.Sprintf("%s %02d dub", latin[0], episode))
			push(fmt.Sprintf("%s %02d dual audio", latin[0], episode))
		}
		if e := cleanQuery(english); e != "" && mostlyLatin(e) &&
			!strings.EqualFold(e, latin[0]) {
			push(fmt.Sprintf("%s %02d", e, episode))
			if dub {
				push(fmt.Sprintf("%s %02d dub", e, episode))
			}
		}

		for _, t := range append([]string{english}, latin...) {
			base := cleanQuery(baseTitle(strings.TrimSpace(t)))
			if base == "" || !mostlyLatin(base) {
				continue
			}
			key := strings.ToLower(base + " ep")
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			if tvdb > 0 && tvdb != episode {
				push(fmt.Sprintf("%s %02d", base, tvdb))
			}
			if absolute > 0 && absolute != episode && absolute != tvdb {
				push(fmt.Sprintf("%s %02d", base, absolute))
			}
			push(fmt.Sprintf("%s %02d", base, episode))
		}
	}
	for _, name := range series {
		if name != "" {
			push(name)
		}
	}
	for _, t := range latin {
		push(t)
	}
	return terms
}

var courSuffix = regexp.MustCompile(
	`(?i)\s+(part|season|cour|final season)\s*\d*$|(?i)\s+\d(nd|rd|th|st)\s+season$`)

func baseTitle(title string) string {
	if trimmed := courSuffix.ReplaceAllString(title, ""); trimmed != title {
		return strings.TrimSpace(trimmed)
	}
	for _, sep := range []string{" - ", " – "} {
		if i := strings.LastIndex(title, sep); i > 8 {
			return strings.TrimSpace(title[:i])
		}
	}
	return ""
}

func spelledOut(titles []string) []string {
	var out []string
	for _, t := range titles {
		if len(t) >= 6 || strings.Contains(t, " ") {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return titles
	}
	return out
}

func shortest(titles []string) string {
	best := titles[0]
	for _, t := range titles[1:] {
		if len(t) < len(best) {
			best = t
		}
	}
	return best
}

func mostlyLatin(s string) bool {
	var latin, total int
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsDigit(r) {
			continue
		}
		total++
		if r < unicode.MaxASCII || unicode.Is(unicode.Latin, r) {
			latin++
		}
	}
	return total == 0 || float64(latin)/float64(total) >= 0.8
}
