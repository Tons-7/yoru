package player

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

type SkipRange struct {
	Kind   string  `json:"kind"`
	Start  float64 `json:"start"`
	End    float64 `json:"end"`
	Source string  `json:"source,omitempty"`
	ID     string  `json:"id,omitempty"`
}

func (s SkipRange) Part() string { return strings.TrimPrefix(strings.ToLower(s.Kind), "mixed-") }

func (s SkipRange) Contains(t float64) bool { return t >= s.Start && t < s.End }

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

func From(ranges []SkipRange, source string) []SkipRange {
	var out []SkipRange
	for _, r := range ranges {
		if r.Source == source {
			out = append(out, r)
		}
	}
	return out
}

type AutoSkip struct{ Opening, Ending, Recap, Preview bool }

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

func EndingStart(ranges []SkipRange) float64 {
	start := 0.0
	for _, r := range ranges {
		if k := strings.ToLower(r.Kind); (k == "ed" || k == "mixed-ed") && r.Start > 0 && (start == 0 || r.Start < start) {
			start = r.Start
		}
	}
	return start
}

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

func WriteChapters(path string, ranges []SkipRange, duration float64) error {
	var b strings.Builder
	b.WriteString(";FFMETADATA1\n")

	ranges = slices.Clone(ranges)
	slices.SortFunc(ranges, func(a, b SkipRange) int { return cmp.Compare(a.Start, b.Start) })
	prev := 0.0
	for _, r := range ranges {
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
