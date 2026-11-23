package parse

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type Release struct {
	Raw        string
	Title      string
	Group      string
	Season     int
	SeasonEnd  int
	Special    bool
	Part       int
	Episode    int
	EpisodeEnd int
	Version    int
	Resolution string
	Source     string
	Codec      string
	BitDepth   int
	Audio      []string
	DualAudio  bool
	Subtitles  []string
	HardSub    bool
	Batch      bool
	CRC32      string
	Extension  string
}

var (
	bracketed     = regexp.MustCompile(`[\[\(]([^\]\)]*)[\]\)]`)
	leadGroup     = regexp.MustCompile(`^\[([^\]]+)\]`)
	sceneGroup    = regexp.MustCompile(`-([A-Za-z0-9]+)$`)
	crc32Re       = regexp.MustCompile(`(?i)^[0-9A-F]{8}$`)
	resolution    = regexp.MustCompile(`(?i)(?:\b|_|BD|WEB|DVD|HDTV|Rip)(\d{3,4})[pi]\b|\b(\d{3,4})x(\d{3,4})\b`)
	seasonEp      = regexp.MustCompile(`(?i)\bS(\d{1,2})\s*E(\d{1,4})(?:v(\d))?\b`)
	seasonEpSpan  = regexp.MustCompile(`(?i)\bS(\d{1,2})\s*E(\d{1,4})\s*[-~]\s*(?:S\d{1,2})?E?(\d{1,4})\b`)
	bracketEp     = regexp.MustCompile(`\[(\d{1,4})(?:v(\d))?\]`)
	dashEp        = regexp.MustCompile(`(?:^|\s)-\s*(\d{1,4})(?:v(\d))?(?:\s|$)`)
	rangeEp       = regexp.MustCompile(`(?:^|[\s\(\[])(\d{1,4})\s*[-~]\s*(\d{1,4})(?:[\s\)\]]|$)`)
	markedEp      = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])E(?:P|PISODE)?\.?\s*(\d{1,4})(?:v(\d))?\b`)
	listedEp      = regexp.MustCompile(`(?i)\bE(\d{1,4})(?:[\s,]+E(\d{1,4}))+\b`)
	markedSpan    = regexp.MustCompile(`(?i)(?:^|[\s._\[(])EP?\.?\s*(\d{1,4})\s*[-~]\s*(?:EP?\.?\s*)?(\d{1,4})\b`)
	seasonVersion = regexp.MustCompile(`(?i)\b(season\s*\d{1,2})v(\d)\b`)
	seasonOnly    = regexp.MustCompile(`(?i)\b(?:S(\d{1,2})|Season\s*(\d{1,2}))\b|\b(\d{1,2})(?:st|nd|rd|th)\s*Season\b|\bS(\d{1,2})SP\d{1,4}(?:\s*-\s*\d{1,4})?\b`)
	partOnly      = regexp.MustCompile(`(?i)\b(?:part|cour)\s*(\d{1,2}|IV|I{1,3})\b`)
	bitDepthRe    = regexp.MustCompile(`(?i)\b(8|10)\s*-?\s*bits?\b|\bhi(10)p?\b`)
	extRe         = regexp.MustCompile(`(?i)\.(mkv|mp4|avi|ts|m2ts|webm)$`)

	metaTokens = `\d{3,4}[pi]|\d{3,4}x\d{3,4}|BD|BDRip|BDMV|Blu-?ray|Remux|WEB-?DL|WEB-?Rip|WEB|HDTV|TVRip|DVD|DVDRip|CR|HIDIVE|AMZN|NF|Baha|Funi|x26[45]|H\.?26[45]|HEVC|AVC|AV1|VP9|MPEG-?2|AAC|FLAC|Opus|E?-?AC-?3|DDP?|DTS|TrueHD|Dual|Multi|10-?bits?|8-?bits?|Hi10P|Complete|Batch`
	metaStart  = regexp.MustCompile(`(?i)[\s._]+\b(` + metaTokens + `)\b`)

	bareEp = regexp.MustCompile(`(?i)(?:^|[\s._])(?:(\d{2,3}|[1-9]\d{3})(?:v(\d))?[\s._]+|(\d{2}|[1-9]\d{3})(?:v(\d))?[\s._]*[\[(])(` + metaTokens + `)\b`)

	crossEp    = regexp.MustCompile(`(?i)(?:^|[\s._-])(\d{1,2})x(\d{2,3})(?:v(\d))?(?:[\s._\[(-]|$)`)
	ownVersion = regexp.MustCompile(`(?i)\[v(\d)\]`)
	ultraHD    = regexp.MustCompile(`(?i)\b(?:4K|UHD)\b`)

	notGroup = regexp.MustCompile(`(?i)^(?:(?:19|20)\d{2}|S\d{1,2}E\d{1,4}(?:\s*-\s*(?:S\d{1,2})?E?\d{1,4})?|[a-z]{2,3}(?:/[a-z]{2,3})+)$`)

	dotSeparated = regexp.MustCompile(`[._]`)

	endAfterEp = regexp.MustCompile(`(?i)((?:^|\s)-\s*\d{1,4}(?:v\d)?)\s+(?:end|final)\b`)

	spacedCodec = regexp.MustCompile(`(?i)\b[hx]\s*\.?\s*26[45]\b`)
	seasonsSpan = regexp.MustCompile(`(?i)\bSeasons\s*(\d{1,2})\s*[-~+&]\s*(\d{1,2})\b|\bSeason\s*(\d{1,2})(?:[-~]|\s*[+&]\s*)(\d{1,2})\b`)
	seasonPair  = regexp.MustCompile(`(?i)\bS(\d{1,2})\s*[-+&]\s*S(\d{1,2})\b`)
	seasonTail  = regexp.MustCompile(`(?i)(?:\bS\d{1,2}|\bseason\s*\d{1,2})$`)
)

type named struct {
	re   *regexp.Regexp
	name string
}

func table(entries ...string) []named {
	out := make([]named, 0, len(entries)/2)
	for i := 0; i+1 < len(entries); i += 2 {
		out = append(out, named{regexp.MustCompile(entries[i]), entries[i+1]})
	}
	return out
}

var sources = table(
	`bd\s*remux|blu-?ray\s*remux|remux`, "BDRemux",
	`\bbdrip\b|\bbd(?:_?\d{3,4}[pi])?\b|blu-?ray|\bbdmv\b`, "BD",
	`\bdvdrip\b|\bdvd\b`, "DVD",
	`web-?dl|web-?rip|\bweb(?:_?\d{3,4}[pi])?\b|\bcr\b|hidive|\bamzn\b|\bnf\b|\bbaha\b|funi`, "WEB",
	`\bhdtv\b|\btvrip\b|\bbs11\b|\bbs\d{1,2}\b|\btx\b`, "TV",
)

var dualAudio = regexp.MustCompile(`(?i)\b(?:dual[\s-]?audio|multi[\s-]?audio|dual)\b`)

var codecs = table(
	`\bav1\b`, "AV1",
	`x\s*265|h\.?\s*265|hevc`, "HEVC",
	`x\s*264|h\.?\s*264|\bavc\b`, "H264",
	`\bmpeg-?2\b`, "MPEG2",
	`\bvp9\b`, "VP9",
)

var audioCodecs = table(
	`\be-?ac-?3\b|\bddp\b|dolby\s*digital\s*plus`, "EAC3",
	`\bac-?3\b`, "AC3",
	`\btruehd\b`, "TrueHD",
	`\bdts(?:-hd)?\b`, "DTS",
	`\bflac(?:\d(?:\.\d)?)?\b`, "FLAC",
	`\bopus(?:\d(?:\.\d)?)?\b`, "Opus",
	`\baac(?:\d(?:\.\d)?)?\b`, "AAC",
)

func Parse(name string) Release {
	r := Release{Raw: name}
	work := strings.TrimSpace(name)

	if m := extRe.FindStringSubmatch(work); m != nil {
		r.Extension = strings.ToLower(m[1])
		work = extRe.ReplaceAllString(work, "")
	}
	if !strings.Contains(work, " ") && strings.Count(work, "_") >= 2 {
		work = strings.ReplaceAll(work, "_", " ")
	}

	r.Group = detectGroup(work)
	work = seasonVersion.ReplaceAllString(work, "$1 v$2")

	for _, tok := range bracketed.FindAllStringSubmatch(work, -1) {
		field := strings.TrimSpace(tok[1])
		if crc32Re.MatchString(field) && !isResolution(field) {
			r.CRC32 = strings.ToUpper(field)
		}
	}

	lower := strings.ToLower(work)
	r.Source = firstMatch(lower, sources)
	r.Codec = firstMatch(lower, codecs)
	r.DualAudio = dualAudio.MatchString(lower)
	r.Batch = isBatch(lower)
	r.Subtitles, r.HardSub = detectSubtitles(name)

	for _, a := range audioCodecs {
		if a.re.MatchString(lower) {
			r.Audio = append(r.Audio, a.name)
		}
	}

	if m := resolution.FindStringSubmatch(work); m != nil {
		switch {
		case m[1] != "":
			r.Resolution = m[1] + "p"
		case m[3] != "":
			r.Resolution = m[3] + "p"
		}
	} else if ultraHD.MatchString(work) {
		r.Resolution = "2160p"
	}

	if m := bitDepthRe.FindStringSubmatch(lower); m != nil {
		switch {
		case m[2] != "":
			r.BitDepth = 10
			if r.Codec == "" {
				r.Codec = "H264"
			}
		case m[1] != "":
			r.BitDepth, _ = strconv.Atoi(m[1])
		}
	}

	r.Part = partNumber(beforeMarker(work))
	numbered := spacedCodec.ReplaceAllString(work, " ")
	if r.Part > 0 {
		numbered = partOnly.ReplaceAllString(numbered, " ")
	}
	seasonLo, seasonHi := 0, 0
	for _, re := range []*regexp.Regexp{seasonsSpan, seasonPair} {
		if m := re.FindStringSubmatch(numbered); m != nil {
			if m[1] == "" && len(m) > 4 {
				m = []string{m[0], m[3], m[4]}
			}
			lo, _ := strconv.Atoi(m[1])
			hi, _ := strconv.Atoi(m[2])
			if hi > lo && lo > 0 {
				seasonLo, seasonHi = lo, hi
				numbered = re.ReplaceAllString(numbered, " ")
				break
			}
		}
	}
	r.Season, r.Episode, r.EpisodeEnd, r.Version = numbering(numbered, r.Batch)
	if m := ownVersion.FindStringSubmatch(work); m != nil && r.Version == 0 {
		r.Version, _ = strconv.Atoi(m[1])
	}
	if seasonHi > 0 {
		r.Season, r.SeasonEnd, r.Batch = seasonLo, seasonHi, true
	}
	if m := seasonEp.FindStringSubmatch(numbered); m != nil && r.EpisodeEnd == 0 {
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		r.Special = s == 0 || e == 0
	}

	if r.Season > 0 && r.Episode == 0 && !r.Special {
		r.Batch = true
	}
	if r.EpisodeEnd > r.Episode {
		r.Batch = true
	}

	r.Title = title(work, r)
	return r
}

func detectGroup(work string) string {
	if m := leadGroup.FindStringSubmatch(work); m != nil {
		return strings.TrimSpace(m[1])
	}

	stripped := strings.TrimSpace(bracketed.ReplaceAllString(work, " "))
	if m := sceneGroup.FindStringSubmatch(stripped); m != nil && !strings.EqualFold(m[1], "DL") {
		return m[1]
	}

	tokens := bracketed.FindAllStringSubmatch(work, -1)
	for _, token := range slices.Backward(tokens) {
		field := strings.TrimSpace(token[1])
		if field == "" || len(field) > 20 || strings.ContainsAny(field, " .") {
			continue
		}
		if crc32Re.MatchString(field) || isResolution(field) || metaStart.MatchString(" "+field) || notGroup.MatchString(field) {
			continue
		}
		return field
	}
	return ""
}

func isResolution(s string) bool {
	return resolution.MatchString(s)
}

func isDotSeparated(s string) bool {
	return strings.Count(s, ".")+strings.Count(s, "_") > strings.Count(s, " ")
}

func firstMatch(lower string, table []named) string {
	for _, e := range table {
		if e.re.MatchString(lower) {
			return e.name
		}
	}
	return ""
}

var batchWord = regexp.MustCompile(`\b(?:batch|complete)\b`)

func isBatch(lower string) bool {
	if batchWord.MatchString(lower) {
		return true
	}
	for _, marker := range []string{"bd box", "season 1-", "s1-"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

var romanParts = map[string]int{"I": 1, "II": 2, "III": 3, "IV": 4}

var recapWord = regexp.MustCompile(`(?i)\brecap\b`)

func PartOf(title string) int { return partNumber(title) }

func SeasonOf(title string) int {
	season, _, _, _ := numbering(title, false)
	return season
}

var (
	trailingNumeral = regexp.MustCompile(`(?i)\s+(II|III|IV|V|VI|VII|VIII|IX|X|[2-9])[!?.]*$`)
	romanSeasons    = map[string]int{"II": 2, "III": 3, "IV": 4, "V": 5, "VI": 6, "VII": 7, "VIII": 8, "IX": 9, "X": 10}
)

func NumeralSeason(title string) (season int, roman bool) {
	for _, sep := range []string{":", " - "} {
		if i := strings.Index(title, sep); i > 0 {
			title = title[:i]
		}
	}
	title = strings.TrimSpace(title)
	m := trailingNumeral.FindStringSubmatch(title)
	if m == nil {
		return 0, false
	}
	if before := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(title, m[0]))); strings.HasSuffix(before, "part") ||
		strings.HasSuffix(before, "cour") {
		return 0, false
	}
	if n, ok := romanSeasons[strings.ToUpper(m[1])]; ok {
		return n, true
	}
	n, _ := strconv.Atoi(m[1])
	return n, false
}

func partNumber(work string) int {
	all := partOnly.FindAllStringSubmatch(work, -1)
	if len(all) == 0 || recapWord.MatchString(work) {
		return 0
	}
	m := all[len(all)-1]
	if n, err := strconv.Atoi(m[1]); err == nil {
		return n
	}
	return romanParts[strings.ToUpper(m[1])]
}

func numbering(work string, batch bool) (season, episode, end, version int) {
	if m := seasonEpSpan.FindStringSubmatch(work); m != nil && !bareSpacedEnd(m[0]) {
		season, _ = strconv.Atoi(m[1])
		episode, _ = strconv.Atoi(m[2])
		end, _ = strconv.Atoi(m[3])
		if end > episode {
			return season, episode, end, 0
		}
	}
	if m := seasonEp.FindStringSubmatch(work); m != nil {
		season, _ = strconv.Atoi(m[1])
		episode, _ = strconv.Atoi(m[2])
		version, _ = strconv.Atoi(m[3])
		return season, episode, 0, version
	}

	if m := crossEp.FindStringSubmatch(work); m != nil {
		season, _ = strconv.Atoi(m[1])
		episode, _ = strconv.Atoi(m[2])
		version, _ = strconv.Atoi(m[3])
		return season, episode, 0, version
	}

	if m := seasonOnly.FindStringSubmatch(work); m != nil {
		for _, g := range m[1:] {
			if g != "" {
				season, _ = strconv.Atoi(g)
				break
			}
		}
	}

	if m := rangeEp.FindStringSubmatchIndex(work); m != nil {
		lo, _ := strconv.Atoi(work[m[2]:m[3]])
		hi, _ := strconv.Atoi(work[m[4]:m[5]])
		span := work[m[0]:m[1]]
		separated := strings.Contains(span, " - ") || strings.Contains(span, " ~ ")
		lead := strings.TrimRight(work[:m[2]], " ")
		opens := lead == "" || strings.ContainsAny(lead[len(lead)-1:], "-~([")
		worded := separated && !batch && startsWithWord(work[m[5]:])
		if hi > lo && (batch || !separated || opens) && !worded && !(isYear(lo) && isYear(hi)) {
			return season, lo, hi, 0
		}
	}
	for _, re := range []*regexp.Regexp{markedSpan, listedEp} {
		if m := re.FindStringSubmatch(work); m != nil {
			lo, _ := strconv.Atoi(m[1])
			hi, _ := strconv.Atoi(m[2])
			if hi > lo {
				return season, lo, hi, 0
			}
		}
	}

	m := dashEp.FindStringSubmatch(work)
	if m == nil {
		m = markedEp.FindStringSubmatch(work)
	}
	if m == nil && !batch {
		if b := bareEpisode(work); b != nil {
			m = []string{work[b[0]:b[1]], work[b[2]:b[3]], ""}
			if b[4] >= 0 {
				m[2] = work[b[4]:b[5]]
			}
		}
	}
	if m == nil {
		m = bracketEpisode(work)
	}
	if m != nil {
		episode, _ = strconv.Atoi(m[1])
		if m[2] != "" {
			version, _ = strconv.Atoi(m[2])
		}
	}
	return season, episode, end, version
}

func isYear(n int) bool { return n >= 1900 && n <= 2100 }

func bareSpacedEnd(span string) bool {
	for _, sep := range []string{" - ", " ~ "} {
		if _, after, ok := strings.Cut(span, sep); ok {
			tail := strings.TrimSpace(after)
			return tail != "" && tail[0] != 'E' && tail[0] != 'e' && tail[0] != 'S' && tail[0] != 's'
		}
	}
	return false
}

func startsWithWord(s string) bool {
	s = strings.TrimLeft(s, " ")
	for _, r := range s {
		return unicode.IsLetter(r)
	}
	return false
}

func bareEpisode(work string) []int {
	for _, m := range bareEp.FindAllStringSubmatchIndex(work, -1) {
		if m[2] < 0 {
			m = []int{m[0], m[1], m[6], m[7], m[8], m[9]}
		} else {
			m = m[:6]
		}
		if !hasWordBefore(work, m[2]) {
			continue
		}
		if n, _ := strconv.Atoi(work[m[2]:m[3]]); m[3]-m[2] == 4 && n >= 1900 && n <= 2100 {
			continue
		}
		if m[0] > 0 && work[m[0]-1] >= '0' && work[m[0]-1] <= '9' && !seasonTail.MatchString(work[:m[0]]) {
			continue
		}
		if m[0] > 0 && work[m[0]] == '.' && strings.ContainsRune("hHxX", rune(work[m[0]-1])) {
			continue
		}
		return m
	}
	return nil
}

func bracketEpisode(work string) []string {
	for _, m := range bracketEp.FindAllStringSubmatch(work, -1) {
		if n, _ := strconv.Atoi(m[1]); len(m[1]) == 4 && n >= 1900 && n <= 2100 {
			continue
		}
		return m
	}
	return nil
}

func hasWordBefore(work string, idx int) bool {
	head := work[:idx]
	if m := leadGroup.FindString(head); m != "" {
		head = head[len(m):]
	}
	return strings.ContainsFunc(head, unicode.IsLetter)
}

func trailingName(work, group string) (int, string) {
	cut := -1
	for _, m := range bracketed.FindAllStringSubmatchIndex(work, -1) {
		field := strings.TrimSpace(work[m[2]:m[3]])
		if isResolution(field) || metaStart.MatchString(" "+field) {
			cut = m[1]
		}
	}
	if cut < 0 {
		return -1, ""
	}
	tail := strings.TrimSpace(bracketed.ReplaceAllString(work[cut:], " "))
	if strings.HasPrefix(tail, "|") || strings.HasPrefix(tail, "/") {
		return -1, ""
	}
	tail = strings.TrimSpace(strings.TrimLeft(tail, "-~ "))
	if !strings.ContainsFunc(tail, unicode.IsLetter) || metaStart.MatchString(" "+tail) ||
		strings.HasPrefix(tail, ".") || strings.EqualFold(tail, group) {
		return -1, ""
	}
	return cut, strings.Join(strings.Fields(tail), " ")
}

func title(work string, r Release) string {
	work = spacedCodec.ReplaceAllString(work, " ")
	alt := ""
	if r.Episode == 0 || r.EpisodeEnd > 0 {
		if cut, tail := trailingName(work, r.Group); cut >= 0 {
			work, alt = work[:cut], tail
		}
	}
	out := releaseTitle(work, r)
	if alt != "" {
		alt = releaseTitle(alt, Release{Season: r.Season, Batch: r.Batch})
	}
	if alt != "" && out != "" && !strings.EqualFold(alt, out) {
		out += " | " + alt
	}
	return out
}

func releaseTitle(work string, r Release) string {
	if r.SeasonEnd > 0 {
		work = seasonsSpan.ReplaceAllString(seasonPair.ReplaceAllString(work, " "), " ")
	}
	marked := dashEp.MatchString(work) || markedEp.MatchString(work)
	if b := bareEpisode(work); r.Episode > 0 && b != nil && !marked {
		end := b[3]
		if b[4] >= 0 {
			end = b[5]
		}
		work = work[:b[0]] + work[end:]
	}
	s := bracketed.ReplaceAllString(work, " ")

	if r.Group != "" {
		s = strings.TrimSuffix(strings.TrimSpace(s), "-"+r.Group)
	}
	s = strings.ReplaceAll(s, "_", " ")
	if isDotSeparated(s) {
		s = dotSeparated.ReplaceAllString(s, " ")
	}
	if r.Episode > 0 || r.Batch || r.Special {
		if r.EpisodeEnd > 0 {
			s = seasonEpSpan.ReplaceAllString(s, " ")
		}
		if r.Episode > 0 && r.EpisodeEnd == 0 {
			s = beforeEpisode(s)
		}
		s = seasonEp.ReplaceAllString(s, " ")
		if r.EpisodeEnd > 0 {
			s = markedSpan.ReplaceAllString(s, " ")
			s = rangeEp.ReplaceAllString(s, " ")
			s = listedEp.ReplaceAllString(s, " ")
		}
		s = endAfterEp.ReplaceAllString(s, "$1 ")
		s = dashEp.ReplaceAllString(s, " ")
		s = markedEp.ReplaceAllString(s, " ")
	}
	if r.Season > 0 {
		s = seasonOnly.ReplaceAllString(s, " ")
	}

	if loc := metaStart.FindStringIndex(s); loc != nil {
		s = s[:loc[0]]
	}

	out := strings.Trim(strings.Join(dropLeftovers(strings.Fields(s)), " "), " -_~,+")
	if out == "" {
		out = bracketedTitle(work, r)
	}
	return out
}

var leftover = regexp.MustCompile(`(?i)^(?:\{?v\d\}?|\+|ep\.?)$`)

func dropLeftovers(words []string) []string {
	out := words[:0:0]
	for i := 0; i < len(words); i++ {
		w := words[i]
		if w == "+" && i+1 < len(words) && packExtra.MatchString(words[i+1]) {
			i++
			continue
		}
		if leftover.MatchString(w) {
			continue
		}
		out = append(out, w)
	}
	return out
}

var packExtra = regexp.MustCompile(`(?i)^(?:ova|ovas|oad|specials?|sp|movies?|extras?|soundtracks?|osts?)$`)

func beforeMarker(s string) string {
	for _, re := range []*regexp.Regexp{seasonEp, dashEp, markedEp} {
		if loc := re.FindStringIndex(s); loc != nil && strings.ContainsFunc(s[:loc[0]], unicode.IsLetter) {
			return s[:loc[0]]
		}
	}
	return s
}

var altName = regexp.MustCompile(`\s+[|/]\s+`)

func beforeEpisode(s string) string {
	for _, re := range []*regexp.Regexp{seasonEp, dashEp, markedEp} {
		loc := re.FindStringIndex(s)
		if loc == nil {
			continue
		}
		if !strings.ContainsFunc(s[:loc[0]], unicode.IsLetter) {
			return s
		}
		if alt := altName.FindStringIndex(s[loc[1]:]); alt != nil {
			return s[:loc[1]] + s[loc[1]+alt[0]:]
		}
		return s[:loc[1]]
	}
	return s
}

func bracketedTitle(work string, r Release) string {
	for i, tok := range bracketed.FindAllStringSubmatch(work, -1) {
		field := strings.TrimSpace(tok[1])
		if i == 0 && field == r.Group || !strings.ContainsFunc(field, unicode.IsLetter) {
			continue
		}
		if crc32Re.MatchString(field) || isResolution(field) || metaStart.MatchString(" "+field) {
			continue
		}
		if langs, _ := detectSubtitles("[" + field + "]"); len(langs) > 0 {
			continue
		}
		return field
	}
	return ""
}
