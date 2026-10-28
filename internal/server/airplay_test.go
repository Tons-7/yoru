package server

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"yoru/internal/transcode"
)

// An opened episode names its AirPlay address, which an Apple TV follows without the page's cookie: a master
// playlist (picture and English track), the track's playlist, and its WebVTT mapped to 0.
func TestAirPlayGetsThePictureAndTheSubtitles(t *testing.T) {
	h, source := streamHarness(t)
	code, opened := h.call(t, "POST", "/api/stream/open?id=1&episode=1&source="+source, "")
	link, _ := opened["airplay"].(string)
	at := strings.Index(link, "/c/")
	if code != http.StatusOK || at < 0 || !strings.HasSuffix(link, "/master.m3u8") {
		t.Fatalf("open answered %d with AirPlay %q", code, link)
	}
	root := strings.TrimSuffix(link[at:], "master.m3u8")

	// The English track picked on the page.
	code, _, master := h.fetch(t, "GET", root+"master.m3u8?sub=3")
	for _, want := range []string{`TYPE=SUBTITLES,GROUP-ID="subs"`, `LANGUAGE="eng",DEFAULT=YES`, `URI="subs-3.m3u8"`,
		`SUBTITLES="subs"`, "\nplaylist.m3u8\n"} {
		if code != http.StatusOK || !bytes.Contains(master, []byte(want)) {
			t.Fatalf("master answered %d without %s:\n%s", code, want, master)
		}
	}
	code, _, subs := h.fetch(t, "GET", root+"subs-3.m3u8")
	if code != http.StatusOK || !bytes.Contains(subs, []byte("\nsubs-3-0.vtt\n")) || !bytes.HasSuffix(subs, []byte("#EXT-X-ENDLIST\n")) {
		t.Fatalf("the track's playlist answered %d:\n%s", code, subs)
	}
	code, _, vtt := h.fetch(t, "GET", root+"subs-3-0.vtt")
	if code != http.StatusOK || !bytes.HasPrefix(vtt, []byte("WEBVTT\nX-TIMESTAMP-MAP=MPEGTS:0,LOCAL:00:00:00.000\n")) ||
		!bytes.Contains(vtt, []byte("00:01.000 --> 00:05.000")) {
		t.Fatalf("the track answered %d: %q", code, vtt)
	}
	if code, _, playlist := h.fetch(t, "GET", root+"playlist.m3u8"); code != http.StatusOK || !bytes.HasPrefix(playlist, []byte("#EXTM3U")) {
		t.Fatalf("the picture's playlist answered %d", code)
	}
	// The Chromecast's own track keeps no map.
	if code, _, vtt := h.fetch(t, "GET", root+"sub-3.vtt"); code != http.StatusOK || bytes.Contains(vtt, []byte("X-TIMESTAMP-MAP")) {
		t.Errorf("the Chromecast's track answered %d: %q", code, vtt)
	}
	// Subtitles off on the page: none shown on the TV, nor picked for it.
	if _, _, master := h.fetch(t, "GET", root+"master.m3u8?sub=off"); !bytes.Contains(master, []byte("DEFAULT=NO,AUTOSELECT=NO")) {
		t.Errorf("with subtitles off:\n%s", master)
	}
}

// A minute a time, each piece holding the cues showing in it, one across its edge in both.
func TestAirPlaySubtitlesComeAMinuteAtATime(t *testing.T) {
	vtt := []byte("WEBVTT\n\n00:10.000 --> 00:12.000\nfirst\n\n00:59.000 --> 01:02.000\nacross\n\n01:30.000 --> 01:31.000\nsecond\n")
	first, second := string(airplayCues(vtt, 0)), string(airplayCues(vtt, 1))
	if !strings.HasPrefix(first, "WEBVTT\nX-TIMESTAMP-MAP=MPEGTS:0,LOCAL:00:00:00.000\n") ||
		!strings.Contains(first, "first") || !strings.Contains(first, "across") || strings.Contains(first, "second") {
		t.Errorf("minute one:\n%s", first)
	}
	if strings.Contains(second, "first") || !strings.Contains(second, "across") || !strings.Contains(second, "second") {
		t.Errorf("minute two:\n%s", second)
	}
	if got := airplaySubtitles(&transcode.Session{Info: &transcode.MediaInfo{Duration: 130}}, 3); !strings.Contains(got,
		"#EXTINF:10.000,\nsubs-3-2.vtt\n#EXT-X-ENDLIST") {
		t.Errorf("a 130 s episode's pieces:\n%s", got)
	}
}

// The page's pick is the TV's; with none, the file's own default, never simply the first.
func TestAirPlayFollowsThePagesSubtitles(t *testing.T) {
	session := &transcode.Session{Info: &transcode.MediaInfo{Subtitles: []transcode.Stream{
		{Index: 2, Codec: "ass", Language: "eng"}, {Index: 3, Codec: "ass", Language: "spa"},
	}}}
	shown := func(choice string) []string {
		var on []string
		for line := range strings.SplitSeq(airplayMaster(session, choice), "\n") {
			if strings.Contains(line, "DEFAULT=YES") {
				on = append(on, line[strings.Index(line, "URI="):])
			}
		}
		return on
	}
	if on := shown("3"); len(on) != 1 || on[0] != `URI="subs-3.m3u8"` {
		t.Errorf("Spanish picked: %v", on)
	}
	if on := shown(""); len(on) != 0 {
		t.Errorf("no pick and no default in the file: %v, want none forced", on)
	}
}
