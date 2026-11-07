package library

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"yoru/internal/film"
	"yoru/internal/parse"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

type filmSeries struct {
	key   film.Key
	names []string
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

	base := seriesBase(own.Romaji)
	if base == "" {
		return nil
	}
	var siblings []film.Entry
	var romaji, english []string
	sharing := 0
	for _, m := range members {
		if strings.EqualFold(seriesBase(m.Romaji), base) {
			if !single(m.Format, m.Episodes) {
				return nil
			}
			sharing++
			romaji = append(romaji, m.Romaji)
			if m.English != nil {
				english = append(english, *m.English)
			}
		}
		if m.ID != animeID {
			siblings = append(siblings, film.Entry{Titles: titlesOf(m)})
		}
	}
	if sharing < 2 {
		return nil
	}

	out := &filmSeries{key: film.New(film.Entry{Titles: titlesOf(*own)}, siblings)}
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

func seriesBase(title string) string {
	return strings.ToLower(strings.TrimSpace(baseOf(title)))
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
		if f, i, ok := torrent.PickInPack(files, r.Episode, r.Numbers, torrent.Scope{Season: r.Season, Counted: r.Counted, Others: r.Others}); ok {
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
