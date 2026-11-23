package score

import (
	"fmt"
	"regexp"
	"strings"
)

type Rejection struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

type spec struct {
	name   string
	tier   int
	reject func(Candidate, Preferences) string
}

const (
	tierUnplayable = 1
	tierWrongThing = 2
	tierHardware   = 3
	tierPolicy     = 4
)

var (
	discImage  = regexp.MustCompile(`(?i)\b(bdmv|dvdiso|bdiso)\b|\.iso\b|\biso\s+image\b`)
	sampleFile = regexp.MustCompile(`(?i)\bsample\b`)
	notVideo   = regexp.MustCompile(`(?i)\.(gxs|xdelta|zip|rar|7z|ass|ssa|srt|sub|idx|mka|flac|mp3|ogg|pdf|txt|nfo|exe|iso)$`)
)

var specs = []spec{
	{
		name: "wrong show", tier: tierWrongThing,
		reject: func(c Candidate, _ Preferences) string {
			if c.WrongShow {
				return "names a different show"
			}
			return ""
		},
	},
	{
		name: "wrong film", tier: tierWrongThing,
		reject: func(c Candidate, _ Preferences) string {
			if c.WrongFilm {
				return "a different film of the series"
			}
			return ""
		},
	},
	{
		name: "seeders", tier: tierUnplayable,
		reject: func(c Candidate, _ Preferences) string {
			if c.Torrent.SeedersKnown && c.Torrent.Seeders == 0 {
				return "no seeders"
			}
			return ""
		},
	},
	{
		name: "disc image", tier: tierUnplayable,
		reject: func(c Candidate, _ Preferences) string {
			if discImage.MatchString(c.Torrent.Title) {
				return "disc image rather than an episode file"
			}
			return ""
		},
	},
	{
		name: "not a video", tier: tierUnplayable,
		reject: func(c Candidate, _ Preferences) string {
			if notVideo.MatchString(strings.TrimSpace(c.Torrent.Title)) {
				return "not a video file"
			}
			return ""
		},
	},
	{
		name: "sample", tier: tierUnplayable,
		reject: func(c Candidate, _ Preferences) string {
			if sampleFile.MatchString(c.Torrent.Title) {
				return "sample clip"
			}
			return ""
		},
	},
	{
		name: "raw", tier: tierWrongThing,
		reject: func(c Candidate, prefs Preferences) string {
			if prefs.AllowRaw {
				return ""
			}
			if IsRaw(c) {
				return "raw capture, no subtitles"
			}
			return ""
		},
	},
	{
		name: "audio", tier: tierWrongThing,
		reject: func(c Candidate, prefs Preferences) string {
			if !audioMismatch(c.Release, prefs.Audio) {
				return ""
			}
			if prefs.Audio == "dub" {
				return "no English dub"
			}
			return "dubbed only, no Japanese audio"
		},
	},
	{
		name: "hi10p", tier: tierHardware,
		reject: func(c Candidate, prefs Preferences) string {
			if !prefs.AllowHi10P && c.Release.Codec == "H264" && c.Release.BitDepth == 10 {
				return "10-bit H.264 has no hardware decode"
			}
			return ""
		},
	},
	{
		name: "avoided", tier: tierPolicy,
		reject: func(c Candidate, prefs Preferences) string {
			for _, g := range prefs.AvoidGroups {
				if g = strings.TrimSpace(g); g != "" && strings.EqualFold(g, c.Release.Group) {
					return "avoided group " + c.Release.Group
				}
			}
			title := strings.ToLower(c.Torrent.Title)
			for _, w := range prefs.ExcludeTerms {
				if w = strings.TrimSpace(w); w != "" && strings.Contains(title, strings.ToLower(w)) {
					return fmt.Sprintf("title has the excluded words %q", w)
				}
			}
			if prefs.AvoidLQ && lowQualityGroup(c) {
				return "low-quality group on TRaSH Guides' list"
			}
			return ""
		},
	},
	{
		name: "size", tier: tierPolicy,
		reject: func(c Candidate, prefs Preferences) string {
			if prefs.MaxAutoBytes > 0 && c.EpisodeBytes() > c.SizeLimit(prefs) {
				return "larger than the automatic size limit"
			}
			return ""
		},
	},
}

func evaluateSpecs(c Candidate, prefs Preferences) []Rejection {
	var out []Rejection
	tier := 0

	for _, s := range specs {
		if len(out) > 0 && s.tier != tier {
			break
		}
		reason := s.reject(c, prefs)
		if reason == "" {
			continue
		}
		if len(out) == 0 {
			tier = s.tier
		}
		out = append(out, Rejection{Rule: s.name, Reason: reason})
	}
	return out
}

func (r Rejection) String() string { return fmt.Sprintf("%s: %s", r.Rule, r.Reason) }

func Unwanted(c Candidate, prefs Preferences) bool {
	return (!prefs.AllowRaw && IsRaw(c)) || audioMismatch(c.Release, prefs.Audio)
}

func IsRaw(c Candidate) bool {
	return c.Release.Extension == "ts" ||
		strings.Contains(strings.ToLower(c.Torrent.Category), "raw")
}

func (r Result) RejectedOnlyAsRaw() bool {
	if len(r.Rejections) == 0 {
		return false
	}
	for _, rejection := range r.Rejections {
		if rejection.Rule != "raw" {
			return false
		}
	}
	return true
}
