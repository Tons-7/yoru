package server

import (
	"cmp"
	"fmt"
	"math"
	"strconv"
	"strings"

	"yoru/internal/transcode"
)

func airplayMaster(session *transcode.Session, choice string) string {
	var b strings.Builder
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	var texts []transcode.Stream
	chosen := -1
	for _, sub := range session.Info.Subtitles {
		if !transcode.TextSubtitle(sub.Codec) {
			continue
		}
		if choice == strconv.Itoa(sub.Index) || choice == "" && sub.Default && chosen < 0 {
			chosen = len(texts)
		}
		texts = append(texts, sub)
	}
	clean := strings.NewReplacer(`"`, "'", "\n", " ", "\r", " ")
	for i, sub := range texts {
		def, auto := "NO", "YES"
		if i == chosen {
			def = "YES"
		} else if choice != "" {
			auto = "NO"
		}
		name := clean.Replace(cmp.Or(sub.Title, sub.Language, "Subtitles "+strconv.Itoa(i+1)))
		lang := ""
		if sub.Language != "" {
			lang = `,LANGUAGE="` + clean.Replace(sub.Language) + `"`
		}
		fmt.Fprintf(&b, `#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="%s"%s,DEFAULT=%s,AUTOSELECT=%s,URI="subs-%d.m3u8"`+"\n",
			name, lang, def, auto, sub.Index)
	}
	group := ""
	if len(texts) > 0 {
		group = `,SUBTITLES="subs"`
	}
	rate := session.Info.Bitrate
	if rate <= 0 {
		rate = 8_000_000
	}
	fmt.Fprintf(&b, "#EXT-X-STREAM-INF:BANDWIDTH=%d%s\nplaylist.m3u8\n", rate, group)
	return b.String()
}

const airplayPiece = 60.0

func airplaySubtitles(session *transcode.Session, track int) string {
	d := session.Info.Duration
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n",
		int(airplayPiece))
	for i := range max(1, int(math.Ceil(d/airplayPiece))) {
		fmt.Fprintf(&b, "#EXTINF:%.3f,\nsubs-%d-%d.vtt\n", max(0, min(airplayPiece, d-float64(i)*airplayPiece)), track, i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func airplayCues(vtt []byte, piece int) []byte {
	from, to := float64(piece)*airplayPiece, float64(piece+1)*airplayPiece
	blocks := strings.Split(strings.ReplaceAll(string(vtt), "\r\n", "\n"), "\n\n")
	var b strings.Builder
	head, _, _ := strings.Cut(blocks[0], "\n")
	b.WriteString(head)
	b.WriteString("\nX-TIMESTAMP-MAP=MPEGTS:0,LOCAL:00:00:00.000\n")
	for _, block := range blocks[1:] {
		start, end, ok := cueTimes(block)
		if ok && start < to && end > from {
			b.WriteString("\n")
			b.WriteString(strings.Trim(block, "\n"))
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func cueTimes(block string) (start, end float64, ok bool) {
	for line := range strings.SplitSeq(block, "\n") {
		left, right, found := strings.Cut(line, "-->")
		if !found {
			continue
		}
		right, _, _ = strings.Cut(strings.TrimSpace(right), " ")
		start, okStart := vttSeconds(strings.TrimSpace(left))
		end, okEnd := vttSeconds(right)
		return start, end, okStart && okEnd
	}
	return 0, 0, false
}

func vttSeconds(s string) (float64, bool) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var total float64
	for _, p := range parts {
		n, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0, false
		}
		total = total*60 + n
	}
	return total, true
}
