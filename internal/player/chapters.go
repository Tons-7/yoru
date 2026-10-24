package player

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// The tags matter: the browser player reads lower-case names, as everywhere in the API.
type SkipRange struct {
	Kind  string  `json:"kind"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	// Source: chapters, aniskip, found (shared sound) or marked (the viewer). ID is AniSkip's, for votes.
	Source string `json:"source,omitempty"`
	ID     string `json:"id,omitempty"`
}

// Part is the part of an episode a kind names: a mixed opening is still the opening.
func (s SkipRange) Part() string { return strings.TrimPrefix(strings.ToLower(s.Kind), "mixed-") }

func (s SkipRange) Contains(t float64) bool { return t >= s.Start && t < s.End }

// Merge takes each part from the first list that has it, the lists in order of trust; a list keeps its ranges
// of a part that don't overlap (an A and a B eyecatch, not an opening given as both op and mixed-op).
func Merge(lists ...[]SkipRange) []SkipRange {
	out := []SkipRange{}
	for _, list := range lists {
		trusted := len(out)
		for _, r := range list {
			if !HasPart(out[:trusted], r.Part()) && !overlapsPart(out[trusted:], r) {
				out = append(out, r)
			}
		}
	}
	return out
}

func overlapsPart(ranges []SkipRange, r SkipRange) bool {
	for _, o := range ranges {
		if o.Part() == r.Part() && r.Start < o.End && o.Start < r.End {
			return true
		}
	}
	return false
}

func HasPart(ranges []SkipRange, part string) bool {
	for _, r := range ranges {
		if r.Part() == part {
			return true
		}
	}
	return false
}

// From keeps the ranges of one source.
func From(ranges []SkipRange, source string) []SkipRange {
	var out []SkipRange
	for _, r := range ranges {
		if r.Source == source {
			out = append(out, r)
		}
	}
	return out
}

// AutoSkip is which parts skip by themselves; chapters named in Settings always do.
type AutoSkip struct{ Opening, Ending, Recap, Preview bool }

// Skipped keeps the ranges that skip by themselves.
func (a AutoSkip) Skipped(ranges []SkipRange) []SkipRange {
	var out []SkipRange
	for _, r := range ranges {
		switch r.Part() {
		case "op":
			if a.Opening {
				out = append(out, r)
			}
		case "ed":
			if a.Ending {
				out = append(out, r)
			}
		case "recap":
			if a.Recap {
				out = append(out, r)
			}
		case "preview", "eyecatch":
			if a.Preview {
				out = append(out, r)
			}
		case "skip":
			out = append(out, r)
		}
	}
	return out
}

// SkipNames reads playback.skip_chapters, a JSON list as Settings saves it: chapter names to always skip.
func SkipNames(pref string) []string {
	var names, out []string
	json.Unmarshal([]byte(pref), &names)
	for _, name := range names {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// ChapterKind is a chapter's skip kind: as the probe named it, else "skip" when its title holds one of names.
func ChapterKind(kind, title string, names []string) string {
	if kind != "" {
		return kind
	}
	t := strings.ToLower(title)
	for _, n := range names {
		if strings.Contains(t, n) {
			return "skip"
		}
	}
	return ""
}

// EndingStart is where the earliest ending among an episode's ranges begins, 0 when none is marked.
func EndingStart(ranges []SkipRange) float64 {
	start := 0.0
	for _, r := range ranges {
		if k := strings.ToLower(r.Kind); (k == "ed" || k == "mixed-ed") && r.Start > 0 && (start == 0 || r.Start < start) {
			start = r.Start
		}
	}
	return start
}

// uosc's chapter_range_patterns match these titles to tint the seekbar, so they're load-bearing.
func chapterTitle(kind string) string {
	switch strings.ToLower(kind) {
	case "op", "mixed-op":
		return "Opening"
	case "ed", "mixed-ed":
		return "Ending"
	case "recap":
		return "Recap"
	case "preview":
		return "Preview"
	case "eyecatch":
		return "Eyecatch"
	default:
		return strings.ToUpper(kind)
	}
}

// WriteChapters emits an ffmetadata file for mpv's --chapters-file. Skip data
// lives outside the video, so chapters have to be supplied alongside it.
func WriteChapters(path string, ranges []SkipRange, duration float64) error {
	var b strings.Builder
	b.WriteString(";FFMETADATA1\n")

	// In time order: the file's own chapters come first, AniSkip's after, and mpv lists them by start.
	ranges = slices.Clone(ranges)
	slices.SortFunc(ranges, func(a, b SkipRange) int { return cmp.Compare(a.Start, b.Start) })
	prev := 0.0
	for _, r := range ranges {
		// Empty, or overlapping the one before: two names for one span would mislabel the rest.
		if r.End <= r.Start || r.Start < prev {
			continue
		}
		if r.Start > prev {
			writeChapter(&b, prev, r.Start, "Episode")
		}
		writeChapter(&b, r.Start, r.End, chapterTitle(r.Kind))
		prev = r.End
	}
	if duration > prev {
		writeChapter(&b, prev, duration, "Episode")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeChapter(b *strings.Builder, start, end float64, title string) {
	fmt.Fprintf(b, "\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=%d\nEND=%d\ntitle=%s\n",
		int64(start*1000), int64(end*1000), title)
}
