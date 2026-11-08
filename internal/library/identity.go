package library

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"yoru/internal/corpus"
	"yoru/internal/parse"
	"yoru/internal/store"
)

const shortFormCoverage = 0.4

const sameNameOverlap = 0.7

type showIdentity struct {
	names  [][]string
	bases  [][]string
	folded [][]string
}

func newShowIdentity(known []string) *showIdentity {
	id := &showIdentity{}
	for _, k := range known {
		tokens := identityTokens(corpus.Normalise(k))
		if len(tokens) == 0 {
			continue
		}
		id.names = append(id.names, tokens)
		id.folded = append(id.folded, identityTokens(corpus.Fold(k)))
		base := identityTokens(corpus.Normalise(baseOf(k)))
		if n := len(base); n > 1 && isNumber(base[n-1]) {
			base = base[:n-1]
		}
		if len(base) > 0 && len(base) < len(tokens) {
			id.bases = append(id.bases, base)
		}
	}
	return id
}

func (id *showIdentity) matches(rel parse.Release, rawTitle string) bool {
	title := strings.TrimSpace(rel.Title)
	if title == "" {
		title = rawTitle
	}
	for _, part := range altSplit.Split(title, -1) {
		if id.matchesName(strings.Trim(part, " -")) {
			return true
		}
	}
	return false
}

var altSplit = regexp.MustCompile(`\s+[|/]\s+`)

func (id *showIdentity) matchesName(title string) bool {
	release := identityTokens(corpus.Normalise(title))
	if len(release) == 0 {
		return false
	}

	for _, want := range id.names {
		if slices.Equal(release, want) || shortFormOf(release, want) {
			return true
		}
	}
	for _, base := range id.bases {
		if slices.Equal(release, base) || (len(base) > 1 && startsWith(release, base)) {
			return true
		}
	}

	folded := identityTokens(corpus.Fold(title))
	for _, f := range id.folded {
		if len(f) > 0 && (slices.Equal(folded, f) ||
			(len(folded) == len(f) && overlap(folded, f) >= sameNameOverlap) ||
			(len(f) > 1 && slices.Equal(singular(folded), singular(f)))) {
			return true
		}
	}
	return false
}

func singular(tokens []string) []string {
	out := make([]string, len(tokens))
	for i, t := range tokens {
		if len(t) > 3 && strings.HasSuffix(t, "s") && !strings.HasSuffix(t, "ss") {
			t = t[:len(t)-1]
		}
		out[i] = t
	}
	return out
}

func identifiesShow(rel parse.Release, rawTitle string, known []string) bool {
	return newShowIdentity(known).matches(rel, rawTitle)
}

func heldStands(ctx context.Context, st *store.Store, f *Finder, animeID, episode int, rec store.TorrentRecord) bool {
	if rec.Manual {
		return true
	}
	if st.SeaDexListed(ctx, animeID, rec.InfoHash) {
		return true
	}
	return heldNamesShow(ctx, st, animeID, rec.Name) &&
		!f.anotherFilm(ctx, animeID, episode, rec.Name, parse.BaseName(rec.FilePath))
}

func heldNamesShow(ctx context.Context, st *store.Store, animeID int, name string) bool {
	if strings.TrimSpace(name) == "" {
		return true
	}
	titles, err := st.SearchTitles(ctx, animeID)
	if err != nil {
		return true
	}
	titles = slices.DeleteFunc(titles, func(t string) bool { return t == "" || t == "Unknown" })
	if len(titles) == 0 {
		return true
	}
	return identifiesShow(parse.Parse(name), name, titles)
}

func shortFormOf(release, want []string) bool {
	if len(release) >= len(want) || !startsWith(want, release) {
		return false
	}
	return float64(runeLen(release)) >= shortFormCoverage*float64(runeLen(want))
}

func startsWith(tokens, prefix []string) bool {
	return len(tokens) >= len(prefix) && slices.Equal(tokens[:len(prefix)], prefix)
}

func baseOf(title string) string {
	cut := len(title)
	for _, sep := range []string{":", " - ", " – ", " — "} {
		if i := strings.Index(title, sep); i > 0 && i < cut {
			cut = i
		}
	}
	return title[:cut]
}

func overlap(a, b []string) float64 {
	left, right := tokenSet(a), tokenSet(b)
	var shared int
	for t := range left {
		if right[t] {
			shared++
		}
	}
	union := len(left) + len(right) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

func tokenSet(tokens []string) map[string]bool {
	out := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		out[t] = true
	}
	return out
}

func identityTokens(normalised string) []string {
	fields := strings.Fields(stripSymbols(normalised))
	out := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		switch {
		case isYear(fields[i]):
		case fields[i] == "season":
			if i+1 < len(fields) && isNumber(fields[i+1]) {
				i++
			}
		default:
			out = append(out, fields[i])
		}
	}
	return out
}

func stripSymbols(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, s)
}

func isYear(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && len(s) == 4 && n >= 1900 && n <= 2100
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func runeLen(tokens []string) int {
	var n int
	for _, t := range tokens {
		n += utf8.RuneCountInString(t)
	}
	return n
}
