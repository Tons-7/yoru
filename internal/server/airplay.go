package server

import (
	"cmp"
	"fmt"
	"math"
	"strconv"
	"strings"

	"yoru/internal/transcode"
)

// AirPlay: Safari sends an Apple TV a <source> beside the page's own, fetched by a signed link as Cast does;
// the master playlist adds the text subtitles as WebVTT, since the TV can't see the page's.

// airplayMaster is the picture with each text subtitle track beside it. choice is the page's: a track's
// index, "off", or "" for the file's own default, if it marks one.
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
	// A quoted-string may hold neither a double quote nor a line break.
	clean := strings.NewReplacer(`"`, "'", "\n", " ", "\r", " ")
	for i, sub := range texts {
		// The page's pick is shown; with none, the file's default; with them off there, none at all.
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

// airplayPiece: a minute of subtitles at a time, each asked as the TV nears it, so an episode still
// downloading gets the lines written by then; one file for the whole would be asked once, near empty.
const airplayPiece = 60.0

// airplaySubtitles is one text track's playlist: its pieces, a minute each.
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

// airplayCues is one piece of a WebVTT: the cues showing in it, those across its edges in both, as HLS asks,
// with the cue times mapped as the video's own, from 0.
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

// cueTimes reads a cue's "00:01.000 --> 00:05.000" line.
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

// vttSeconds reads "hh:mm:ss.ttt" or "mm:ss.ttt".
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
