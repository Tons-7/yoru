package film

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"yoru/internal/parse"
)

type Entry struct {
	Titles []string
	Extra  bool
}

type Key struct {
	Number int
	Count  int

	own          [][]string
	others       [][][]string
	extra        bool
	shared       bool
	extraSibling bool
}

type Verdict int

const (
	Unknown Verdict = iota
	Ours
	Other
)

func New(own Entry, siblings []Entry) Key {
	all := append([]Entry{own}, siblings...)

	seen := map[string]int{}
	for _, e := range all {
		words := map[string]bool{}
		for _, t := range e.Titles {
			for _, w := range distinctive(t) {
				words[w] = true
			}
		}
		for w := range words {
			seen[w]++
		}
	}
	common := func(w string) bool { return seen[w] >= 2 && seen[w]*2 >= len(all) }

	sets := func(e Entry) [][]string {
		var out [][]string
		for _, t := range e.Titles {
			words := slices.DeleteFunc(distinctive(t), common)
			if len(words) > 0 {
				out = append(out, words)
			}
		}
		return out
	}

	k := Key{Count: len(all), own: sets(own), extra: own.Extra}
	for _, t := range own.Titles {
		if n := stated(t); n > 0 {
			k.Number = n
			break
		}
	}
	for _, s := range siblings {
		k.others = append(k.others, sets(s))
		if k.Number > 0 && slices.ContainsFunc(s.Titles, func(t string) bool { return stated(t) == k.Number }) {
			k.shared = true
			k.extraSibling = k.extraSibling || s.Extra
		}
	}
	return k
}

func (k Key) Match(name string) Verdict {
	v, _ := k.judge(name)
	return v
}

func (k Key) Named(name string) bool {
	v, grade := k.judge(name)
	return v == Ours && grade >= 2
}

func (k Key) judge(name string) (Verdict, int) {
	words := tokens(name)
	ours := fit(k.own, words)
	var theirs evidence
	for _, o := range k.others {
		if e := fit(o, words); e.beats(theirs) {
			theirs = e
		}
	}
	num, fractional, kind := numbered(name)
	agrees := k.Number > 0 && num == k.Number
	disagrees := k.Number > 0 && num > 0 && num != k.Number

	switch {
	case ours.hits > 0 && ours.beats(theirs):
		if agrees {
			return Ours, 3
		}
		return Ours, 2
	case theirs.beats(ours):
		return Other, 0
	case ours.hits > 0:
		if agrees {
			return Ours, 3
		}
		return Other, 0
	case fractional || disagrees:
		return Other, 0
	case agrees && k.shared && kind == "vol":
		return Unknown, 0
	case agrees && k.shared:
		if side := sideNamed(name); side != k.extra && (k.extra || k.extraSibling) {
			return Other, 0
		}
		return Ours, 1
	case agrees:
		return Ours, 1
	}
	return Unknown, 0
}

type File struct {
	Name   string
	Length int64
}

func (k Key) Pick(files []File) (int, bool) {
	var videos []int
	for i, f := range files {
		if parse.IsVideo(f.Name) {
			videos = append(videos, i)
		}
	}
	switch len(videos) {
	case 0:
		return -1, false
	case 1:
		return videos[0], k.Match(parse.BaseName(files[videos[0]].Name)) != Other
	}

	lengths := make([]int64, 0, len(videos))
	for _, i := range videos {
		lengths = append(lengths, files[i].Length)
	}
	slices.Sort(lengths)
	floor := lengths[len(lengths)/2] / 4

	best, grade := -1, 0
	var tied []int
	for _, i := range videos {
		v, g := k.judge(parse.BaseName(files[i].Name))
		if v != Ours || (g == 1 && files[i].Length < floor) {
			continue
		}
		switch {
		case g > grade:
			best, grade, tied = i, g, nil
		case g == grade:
			tied = append(tied, i)
		}
	}
	if best < 0 {
		return -1, false
	}
	tied = append(tied, best)
	slices.SortFunc(tied, func(a, b int) int { return cmp.Compare(files[b].Length, files[a].Length) })
	if len(tied) == 1 || files[tied[1]].Length < floor || files[tied[1]].Length*4 <= files[tied[0]].Length {
		return tied[0], true
	}
	return best, false
}

type evidence struct {
	ratio float64
	hits  int
}

func (e evidence) beats(o evidence) bool {
	return e.ratio > o.ratio || (e.ratio == o.ratio && e.hits > o.hits)
}

func fit(titles [][]string, words map[string]bool) evidence {
	var best evidence
	for _, t := range titles {
		var hits int
		for _, w := range t {
			if words[w] {
				hits++
			}
		}
		e := evidence{ratio: float64(hits) / float64(len(t)), hits: hits}
		if hits > 0 && e.ratio >= 0.5 && e.beats(best) {
			best = e
		}
	}
	return best
}

var (
	split     = regexp.MustCompile(`[^\p{L}\p{N}]+`)
	square    = regexp.MustCompile(`\[[^\]]*\]`)
	round     = regexp.MustCompile(`\(([^)]*)\)`)
	digits    = regexp.MustCompile(`^\d{1,2}$`)
	layout    = regexp.MustCompile(`(?i)(?:aac|flac|ac3|e-?ac-?3|dts(?:-?hd)?|opus|ddp?|truehd|l?pcm|ma)[\s._-]*\d\.\d`)
	spanned   = regexp.MustCompile(`(?:^|[\s_])\d{1,2}\s*[-~]\s*\d{1,2}(?:$|[\s_])`)
	marker    = regexp.MustCompile(`(?i)\b(chapter|movie|film|vol(?:ume)?)\.?\s*(\d{1,2})\b(?:\s*[-~]\s*(\d{1,2})\b)?`)
	alone     = regexp.MustCompile(`(?:^|[\s_-])(\d{1,2})(?:\.(\d))?(?:v\d)?(?:$|[\s_:-])`)
	sideStory = regexp.MustCompile(`(?i)\b(?:ova|oad|specials?|sp\d{0,3})\b`)
	edition   = regexp.MustCompile(`(?i)\bspecial\s+edition\b`)
)

func sideNamed(name string) bool {
	return sideStory.MatchString(edition.ReplaceAllString(name, ""))
}

var stop = map[string]bool{
	"the": true, "and": true, "with": true, "from": true, "part": true, "movie": true, "film": true,
	"chapter": true, "season": true, "gekijouban": true, "complete": true, "series": true,
}

func tokens(name string) map[string]bool {
	out := map[string]bool{}
	for _, w := range split.Split(strings.ToLower(name), -1) {
		if w != "" {
			out[w] = true
		}
	}
	return out
}

func distinctive(title string) []string {
	var out []string
	for _, w := range split.Split(strings.ToLower(title), -1) {
		if utf8.RuneCountInString(w) < 4 || stop[w] || slices.Contains(out, w) {
			continue
		}
		if _, err := strconv.Atoi(w); err == nil {
			continue
		}
		out = append(out, w)
	}
	return out
}

func stated(title string) int {
	if m := marker.FindStringSubmatch(title); m != nil {
		n, _ := strconv.Atoi(m[2])
		return n
	}
	return 0
}

func Number(name string) (n int, fractional bool) {
	n, fractional, _ = numbered(name)
	return n, fractional
}

func numbered(name string) (n int, fractional bool, kind string) {
	if m := marker.FindStringSubmatch(name); m != nil {
		n, _ = strconv.Atoi(m[2])
		if m[3] != "" {
			if end, _ := strconv.Atoi(m[3]); end > n {
				return 0, false, ""
			}
		}
		if kind = strings.ToLower(m[1]); strings.HasPrefix(kind, "vol") {
			kind = "vol"
		}
		return n, false, kind
	}
	s := square.ReplaceAllString(name, " ")
	s = round.ReplaceAllStringFunc(s, func(group string) string {
		if inner := strings.TrimSpace(group[1 : len(group)-1]); digits.MatchString(inner) {
			return " " + inner + " "
		}
		return " "
	})
	s = layout.ReplaceAllString(s, " ")
	if spanned.MatchString(s) {
		return 0, false, ""
	}
	m := alone.FindStringSubmatch(s)
	if m == nil {
		return 0, false, ""
	}
	n, _ = strconv.Atoi(m[1])
	if m[2] != "" && m[2] != "0" {
		return 0, true, ""
	}
	return n, false, ""
}
