package library

import (
	"encoding/json"
	"slices"
	"strconv"

	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

type scorePrefs = score.Preferences

// resolutions, highest first: what "up to" orders the ladder by.
var resolutions = []string{"2160p", "1440p", "1080p", "720p", "576p", "540p", "480p", "360p"}

// upTo puts the tiers at or under limit first, best first, then any of no listed resolution, then those above,
// nearest first, so nothing is lost.
func upTo(limit string, ladder []score.Tier) []score.Tier {
	at := slices.Index(resolutions, limit)
	var under, other, over []score.Tier
	for _, t := range ladder {
		if !slices.Contains(resolutions, t.Resolution) {
			other = append(other, t)
		}
	}
	for i, res := range resolutions {
		var tiers []score.Tier
		for _, t := range ladder {
			if t.Resolution == res {
				tiers = append(tiers, t)
			}
		}
		if len(tiers) == 0 {
			tiers = []score.Tier{score.ParseTier(res)}
		}
		if i >= at {
			under = append(under, tiers...)
		} else {
			over = append(tiers, over...)
		}
	}
	return slices.Concat(under, other, over)
}

func defaultScorePrefs() scorePrefs { return score.DefaultPreferences() }

// prefsFromSettings turns resolved settings into scoring preferences. Settings
// are pre-resolved, so a per-show override is applied like a global default.
func prefsFromSettings(p store.Prefs) scorePrefs {
	prefs := score.DefaultPreferences()

	var tiers []string
	if err := json.Unmarshal([]byte(p.String("quality.ladder")), &tiers); err == nil && len(tiers) > 0 {
		prefs.Ladder = make([]score.Tier, 0, len(tiers))
		for _, t := range tiers {
			prefs.Ladder = append(prefs.Ladder, score.ParseTier(t))
		}
	}
	if limit := p.String("quality.prefer"); limit == "720p" || limit == "480p" {
		prefs.Ladder = upTo(limit, prefs.Ladder)
	}
	// 0 is no limit, chosen in Settings; unset keeps the default.
	if s := p.String("quality.max_auto_bytes"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil && n >= 0 {
			prefs.MaxAutoBytes = n
		}
	}
	prefs.AllowHi10P = p.Bool("quality.allow_hi10p")

	for key, into := range map[string]*[]string{
		"release.prefer_groups": &prefs.PreferGroups, "release.avoid_groups": &prefs.AvoidGroups,
		"release.exclude_terms": &prefs.ExcludeTerms,
	} {
		var list []string
		if json.Unmarshal([]byte(p.String(key)), &list) == nil {
			*into = list
		}
	}
	prefs.AvoidLQ = p.Bool("release.avoid_lq")
	if a := p.String("audio.prefer"); a != "" {
		prefs.Audio = a
	}
	if l := p.String("subtitle.languages"); l != "" {
		var langs []string
		if json.Unmarshal([]byte(l), &langs) == nil {
			prefs.SubLanguages = langs
		}
	}
	// Never unattended: waiting is how a subbed release, hours after the raw, gets picked.
	prefs.AllowRaw = false
	return prefs
}

// Preferences is the entry point for callers that have an anime id.
func Preferences(p store.Prefs) score.Preferences { return prefsFromSettings(p) }

// TorrentLimits is the engine's download cap, upload choice and network from Settings.
func TorrentLimits(p store.Prefs) torrent.Limits {
	return torrent.Limits{
		DownloadBytes: max(0, p.Int64("downloads.limit_bytes")),
		Upload:        p.String("downloads.upload"),
		Adapter:       p.String("downloads.adapter"),
	}
}
