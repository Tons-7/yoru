package parse

import (
	"regexp"
	"strings"
)

var subtitleMarkers = []struct {
	lang string
	re   *regexp.Regexp
}{
	{"en", regexp.MustCompile(`(?i)\b(?:eng(?:lish)?)[\s._-]*(?:sub(?:s|bed|title)?s?)\b` +
		`|\bsub(?:s|title)?s?[\s._-]*\(?\s*eng?\b` +
		`|\[eng?(?:lish)?\]` +
		`|\bmulti[\s._-]*(?:sub|subtitle)s?\b|\bmultiple[\s._-]*subtitles?\b`)},

	{"zh", regexp.MustCompile(`简体|简繁|繁体|繁體|简日|繁日|中文|字幕组|双语|简中|繁中` +
		`|(?i)\b(?:chs|cht|big5|gb_?jp|jpsc|jptc|chinese)\b`)},

	{"es", regexp.MustCompile(`(?i)\b(?:spa(?:nish)?|castellano|espa[nñ]ol|latino)[\s._-]*sub|\[esp?\]`)},
	{"pt", regexp.MustCompile(`(?i)\blegendado\b|\bpt-?br\b|\bportugu[eê]s\b`)},
	{"ru", regexp.MustCompile(`(?i)\brus(?:sian)?\b|русск|\[ru\]`)},
	{"ar", regexp.MustCompile(`(?i)\barabic\b|\bmutamer\b|مترجم`)},
	{"id", regexp.MustCompile(`(?i)\bindo(?:nesian?)?[\s._-]*sub\b`)},
	{"vi", regexp.MustCompile(`(?i)\bviet(?:sub|namese)\b`)},
	{"th", regexp.MustCompile(`(?i)\bthai[\s._-]*sub\b`)},
	{"ko", regexp.MustCompile(`(?i)\bkor(?:ean)?[\s._-]*sub\b|한국어`)},
	{"fr", regexp.MustCompile(`(?i)\b(?:vostfr|french|fre)[\s._-]*sub\b|\bvostfr\b`)},
	{"de", regexp.MustCompile(`(?i)\bger(?:man)?[\s._-]*sub\b|\bdeutsch\b`)},
	{"it", regexp.MustCompile(`(?i)\bita(?:lian)?[\s._-]*sub\b`)},
}

var hardSubbed = regexp.MustCompile(`内嵌|硬字幕|(?i)\bhard[\s._-]?sub(?:bed|s)?\b`)

var russianDub = regexp.MustCompile(`(?i)\brus(?:sian)?[\s._-]*dub(?:bed|s)?\b`)

func detectSubtitles(name string) ([]string, bool) {
	var out []string
	marked := russianDub.ReplaceAllString(name, " ")
	for _, m := range subtitleMarkers {
		if m.re.MatchString(marked) {
			out = append(out, m.lang)
		}
	}
	return out, hardSubbed.MatchString(name)
}

func HasSubtitleLanguage(r Release, langs []string) bool {
	for _, want := range langs {
		for _, got := range r.Subtitles {
			if strings.EqualFold(want, got) {
				return true
			}
		}
	}
	return false
}
