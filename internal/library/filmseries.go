package library

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"

	"yoru/internal/film"
	"yoru/internal/parse"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

type filmSeries struct {
	key       film.Key
	names     []string
	colliders *showIdentity
}

const seriesMemoTTL = time.Minute

func (f *Finder) rememberSeries(animeID int, s *filmSeries) {
	f.seriesMu.Lock()
	defer f.seriesMu.Unlock()
	if f.seriesSeen == nil {
		f.seriesSeen = map[int]seriesMemo{}
	}
	f.seriesSeen[animeID] = seriesMemo{series: s, at: time.Now()}
}

func (f *Finder) knownSeries(ctx context.Context, animeID int) *filmSeries {
	f.seriesMu.Lock()
	m, ok := f.seriesSeen[animeID]
	f.seriesMu.Unlock()
	if ok && time.Since(m.at) < seriesMemoTTL {
		return m.series
	}
	s := f.filmSeries(ctx, animeID)
	f.rememberSeries(animeID, s)
	return s
}

func (f *Finder) anotherFilm(ctx context.Context, animeID, episode int, names ...string) bool {
	if f == nil || episode > 1 {
		return false
	}
	s := f.knownSeries(ctx, animeID)
	if s == nil {
		return false
	}
	judged := false
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		judged = true
		if s.named(name) && s.key.Match(name) != film.Other {
			return false
		}
	}
	return judged
}

func (s *filmSeries) named(title string) bool {
	return s.colliders == nil || !s.colliders.matches(parse.Parse(title), title)
}

func sideStory(format *string) bool {
	if format == nil {
		return false
	}
	switch strings.ToUpper(*format) {
	case "OVA", "SPECIAL":
		return true
	}
	return false
}

func (s *filmSeries) queries() []string {
	if s == nil {
		return nil
	}
	return s.names
}

func single(format *string, episodes *int) bool {
	if format == nil || (episodes != nil && *episodes > 1) {
		return false
	}
	switch strings.ToUpper(*format) {
	case "MOVIE", "OVA", "SPECIAL", "ONA":
		return true
	}
	return false
}

func (f *Finder) filmSeries(ctx context.Context, animeID int) *filmSeries {
	members := f.seriesMembers(ctx, animeID)

	var own *store.Season
	for i := range members {
		if members[i].ID == animeID {
			own = &members[i]
		}
	}
	if own == nil || !single(own.Format, own.Episodes) {
		return nil
	}

	titlesOf := func(s store.Season) []string {
		titles, _ := f.store.SearchTitles(ctx, s.ID)
		titles = append(titles, s.Romaji)
		if s.English != nil {
			titles = append(titles, *s.English)
		}
		return titles
	}

	bases := seriesBases(own.Romaji)
	var romaji, english []string
	chosen := -1
	for i, base := range bases {
		if romaji, english = filmsSharing(members, base); len(romaji) >= 2 {
			chosen = i
			break
		}
	}
	if chosen < 0 {
		return nil
	}
	var siblings []film.Entry
	var colliding []string
	for _, m := range members {
		shares := slices.Contains(seriesBases(m.Romaji), bases[chosen])
		if chosen > 0 && !shares && slices.Contains(seriesBases(m.Romaji), bases[0]) && !single(m.Format, m.Episodes) {
			colliding = append(colliding, titlesOf(m)...)
		}
		if m.ID != animeID && (chosen == 0 || shares) {
			siblings = append(siblings, film.Entry{Titles: titlesOf(m), Extra: sideStory(m.Format)})
		}
	}

	out := &filmSeries{key: film.New(film.Entry{Titles: titlesOf(*own), Extra: sideStory(own.Format)}, siblings)}
	if len(colliding) > 0 {
		out.colliders = newShowIdentity(colliding)
	}
	for _, name := range []string{commonPrefix(romaji), commonPrefix(english)} {
		if name = cleanQuery(name); len(strings.Fields(name)) >= 2 && mostlyLatin(name) {
			out.names = append(out.names, name)
		}
	}
	return out
}

func (f *Finder) seriesMembers(ctx context.Context, animeID int) []store.Season {
	collect := func(id int) (chain int, out []store.Season) {
		franchise, err := f.store.Franchise(ctx, id)
		if err != nil {
			return 0, nil
		}
		out = append(out, franchise.Seasons...)
		if related, err := f.store.Related(ctx, id); err == nil {
			for _, r := range related {
				out = append(out, r.Season)
			}
		}
		return len(franchise.Seasons), out
	}

	chain, members := collect(animeID)
	if chain <= 1 {
		for _, m := range members {
			if m.ID == animeID {
				continue
			}
			if longer, theirs := collect(m.ID); longer > 1 {
				members = theirs
				break
			}
		}
	}

	seen := map[int]bool{}
	return slices.DeleteFunc(members, func(m store.Season) bool {
		dup := seen[m.ID]
		seen[m.ID] = true
		return dup
	})
}

func filmsSharing(members []store.Season, base string) (romaji, english []string) {
	for _, m := range members {
		if !slices.Contains(seriesBases(m.Romaji), base) {
			continue
		}
		if !single(m.Format, m.Episodes) {
			return nil, nil
		}
		romaji = append(romaji, m.Romaji)
		if m.English != nil {
			english = append(english, *m.English)
		}
	}
	return romaji, english
}

var seriesSeparator = regexp.MustCompile(`:|\s[-–—]\s`)

func seriesBases(title string) []string {
	var out []string
	for _, at := range seriesSeparator.FindAllStringIndex(title, -1) {
		if base := strings.ToLower(strings.TrimSpace(title[:at[0]])); base != "" {
			out = append(out, base)
		}
	}
	if len(out) == 0 {
		if whole := strings.ToLower(strings.TrimSpace(title)); whole != "" {
			out = append(out, whole)
		}
	}
	return out
}

var trailingMarker = regexp.MustCompile(`(?i)[\s:,-]+(?:chapter|movie|film|part|vol(?:ume)?|the)?\s*\d*\s*$`)

func commonPrefix(titles []string) string {
	if len(titles) < 2 {
		return ""
	}
	prefix := strings.Fields(titles[0])
	for _, t := range titles[1:] {
		words := strings.Fields(t)
		n := 0
		for n < len(prefix) && n < len(words) && strings.EqualFold(prefix[n], words[n]) {
			n++
		}
		prefix = prefix[:n]
	}
	out := strings.Join(prefix, " ")
	for {
		trimmed := trailingMarker.ReplaceAllString(out, "")
		if trimmed == out {
			return strings.TrimSpace(out)
		}
		out = trimmed
	}
}

var specialFile = regexp.MustCompile(`(?i)\bS\d{1,2}SP\d{1,3}\b|\bSP\s?\d{1,3}\b|\bspecials?\b|\bOVA\b|\bOAD\b`)

func pickSpecial(files []torrent.File) (torrent.File, int, bool) {
	found := -1
	for i, f := range files {
		if !parse.IsVideo(f.Name) || !specialFile.MatchString(parse.BaseName(f.Name)) {
			continue
		}
		if found >= 0 {
			return torrent.File{}, -1, false
		}
		found = i
	}
	if found < 0 {
		return torrent.File{}, -1, false
	}
	return files[found], found, true
}

var wholeSeries = regexp.MustCompile(
	`(?i)\ball[\s._-]*movies?\b|\bfull[\s._-]*series\b|\bcomplete\b|\bcollection\b|\btrilogy\b|\bbox\b|\bmovies?\s*\d+\s*[-~]\s*\d+|\[\d{1,2}\s*[-~]\s*\d{1,2}\]|全集`)

func pickFile(files []torrent.File, r score.Result) (torrent.File, int, bool) {
	if r.Film == nil {
		if r.TotalEpisodes == 1 && r.Release.Batch {
			if f, i, ok := pickSpecial(files); ok {
				return f, i, true
			}
		}
		scope := torrent.Scope{Season: r.Season, Counted: r.Counted, Others: r.Others, Named: r.Named}
		if f, i, ok := torrent.PickInPack(files, r.Episode, r.Numbers, scope); ok {
			return f, i, true
		}
		if r.TotalEpisodes == 1 && !r.Release.Batch {
			return torrent.PickVideo(files)
		}
		return torrent.File{}, -1, false
	}
	named := make([]film.File, len(files))
	for i, f := range files {
		named[i] = film.File{Name: f.Name, Length: f.Length}
	}
	if i, ok := r.Film.Pick(named); ok {
		return files[i], i, true
	}
	videos := 0
	for _, f := range named {
		if parse.IsVideo(f.Name) {
			videos++
		}
	}
	if !r.Release.Batch && videos > 1 {
		return torrent.PickVideo(files)
	}
	return torrent.File{}, -1, false
}
