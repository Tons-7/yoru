package score

import (
	"regexp"
	"strings"
	"sync/atomic"

	"yoru/internal/parse"
)

// GroupTier is one release group on TRaSH Guides' anime lists: a BD or WEB tier, 1 the best, or low quality.
type GroupTier struct {
	Kind  string // "bd", "web" or "lq"
	Level int
	Name  string
	match *regexp.Regexp
}

// NewGroupTier compiles TRaSH's pattern case-blind, as Sonarr reads it; one RE2 can't parse matches the name.
func NewGroupTier(kind string, level int, name, pattern string) GroupTier {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		re = regexp.MustCompile(`(?i)(^|[^a-z0-9])` + regexp.QuoteMeta(name) + `($|[^a-z0-9])`)
	}
	return GroupTier{Kind: kind, Level: level, Name: name, match: re}
}

var groupTiers atomic.Pointer[[]GroupTier]

// SetGroupTiers swaps in the grades every ranking reads.
func SetGroupTiers(tiers []GroupTier) { groupTiers.Store(&tiers) }

const (
	// A BD tier-1 group is worth 48 over an unlisted one, a low-quality one 50 under: 98 at most, under the 100
	// between ladder tiers, so a grade orders groups and never a source or a resolution.
	groupTierStep = 6.0
	bdTiers       = 8
	webTiers      = 6
	lowQuality    = 50.0
)

// tiersOf is the grade TRaSH gives the release's group for its source, and whether the group is low quality.
func tiersOf(title string, rel parse.Release) (grade *GroupTier, lq bool) {
	tiers := groupTiers.Load()
	if tiers == nil {
		return nil, false
	}
	disc := rel.Source == "BD" || rel.Source == "BDRemux" || rel.Source == "DVD"
	for i := range *tiers {
		t := &(*tiers)[i]
		if !t.match.MatchString(title) {
			continue
		}
		switch {
		case t.Kind == "lq":
			lq = true
		case t.Kind == "bd" && disc, t.Kind == "web" && !disc:
			if grade == nil || t.Level < grade.Level {
				grade = t
			}
		}
	}
	return grade, lq
}

// lowQualityGroup: the release's group is on TRaSH's low-quality list.
func lowQualityGroup(c Candidate) bool {
	_, lq := tiersOf(c.Torrent.Title, c.Release)
	return lq
}

func (r *Result) applyTiers(c Candidate) {
	grade, lq := tiersOf(c.Torrent.Title, c.Release)
	if grade != nil {
		top := bdTiers
		if grade.Kind == "web" {
			top = webTiers
		}
		r.Score += float64(top+1-grade.Level) * groupTierStep
		r.addf("%s tier %d group on TRaSH Guides", strings.ToUpper(grade.Kind), grade.Level)
	}
	if lq {
		r.Score -= lowQuality
		r.add("on TRaSH Guides' low-quality group list")
	}
}
