package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yoru/internal/config"
	"yoru/internal/transcode"
)

// A drawn-in copy's id names its episode, so resume, queue holds and suspends treat it as that episode.
func TestBurnedSessionIDsNameTheirEpisode(t *testing.T) {
	for id, want := range map[string][2]int{
		"127230-3": {127230, 3}, "127230-3-burn2-0": {127230, 3}, "127230-3-burn2--15": {127230, 3}, "-55-7-burn0-5": {-55, 7},
	} {
		if a, e, ok := parseSessionID(id); !ok || a != want[0] || e != want[1] {
			t.Errorf("%s: %d %d %v, want %v", id, a, e, ok, want)
		}
	}
}

// What a TV fetches through its link, from a real session: the playlist, init, a segment and WebVTT subtitles.
func TestCastPlaysASessionThroughItsLink(t *testing.T) {
	h, source := streamHarness(t)
	// Before the link is signed: the grant key includes the token.
	h.server.token = "s3cret"
	code, opened := h.call(t, "POST", "/api/stream/open?id=1&episode=1&source="+source, "")
	if code != http.StatusOK {
		t.Fatalf("open answered %d: %v", code, opened)
	}
	code, link := h.call(t, "POST", "/api/stream/"+opened["id"].(string)+"/cast", "")
	if code != http.StatusOK {
		t.Fatalf("cast link: %d %v", code, link)
	}
	playlist := link["url"].(string)
	root := strings.TrimSuffix(strings.TrimPrefix(playlist, "http://127.0.0.1:4321"), "playlist.m3u8")
	subs, _ := link["subtitles"].([]any)
	if len(subs) != 1 {
		t.Fatalf("subtitles %v, want the one text track", subs)
	}

	tv := func(path string) (int, []byte) {
		req := httptest.NewRequest("GET", path, nil)
		req.Host, req.RemoteAddr = "192.168.1.10:4321", "192.168.1.60:5555"
		req.Header.Set("Origin", "https://www.gstatic.com")
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}
	for _, file := range []string{"playlist.m3u8", "init.mp4", "0.mp4"} {
		if code, body := tv(root + file); code != http.StatusOK || len(body) == 0 {
			t.Errorf("%s: %d, %d bytes", file, code, len(body))
		}
	}
	vtt := strings.TrimPrefix(subs[0].(map[string]any)["url"].(string), "http://127.0.0.1:4321")
	// The fixture's cut-down event format gives ffmpeg no text to carry; a release's standard one does.
	if code, body := tv(vtt); code != http.StatusOK || !strings.HasPrefix(string(body), "WEBVTT") || !strings.Contains(string(body), "00:01.000 --> 00:05.000") {
		t.Errorf("subtitles: %d %q", code, body)
	}
}

// A full-frame white box from 1 s to 5 s: drawn in, a frame at 2 s is almost all white.
const boxASS = `[Script Info]
ScriptType: v4.00+
PlayResX: 320
PlayResY: 240

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,0,0,7,0,0,0,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:05.00,Default,,0,0,0,,{\pos(0,0)\p1}m 0 0 l 320 0 320 240 0 240{\p0}
`

// Drawn in: the TV gets a session of its own whose picture carries the subtitles, and no text track beside.
func TestCastWithSubtitlesDrawnIn(t *testing.T) {
	h, source := streamHarnessWith(t, boxASS)
	code, opened := h.call(t, "POST", "/api/stream/open?id=1&episode=1&source="+source, "")
	if code != http.StatusOK {
		t.Fatalf("open answered %d: %v", code, opened)
	}
	index := opened["subtitles"].([]any)[0].(map[string]any)["index"]
	code, link := h.call(t, "POST", fmt.Sprintf("/api/stream/%s/cast?burn=%v&delay=0", opened["id"], index), "")
	if code != http.StatusOK || link["burned"] != true || len(link["subtitles"].([]any)) != 0 {
		t.Fatalf("cast link: %d %v; want the subtitles drawn in and no text track", code, link)
	}
	root := strings.TrimSuffix(strings.TrimPrefix(link["url"].(string), "http://127.0.0.1:4321"), "playlist.m3u8")
	// Its encoder stopped before the temp folder goes, which Windows won't delete with files open.
	t.Cleanup(func() { h.server.streams.Close(fmt.Sprintf("%s-burn%v-0", opened["id"], index)) })

	var video []byte
	for _, file := range []string{"init.mp4", "0.mp4"} {
		code, _, body := h.fetch(t, "GET", root+file)
		if code != http.StatusOK || len(body) == 0 {
			t.Fatalf("%s: %d, %d bytes", file, code, len(body))
		}
		video = append(video, body...)
	}
	clip := filepath.Join(t.TempDir(), "seg.mp4")
	if err := os.WriteFile(clip, video, 0o644); err != nil {
		t.Fatal(err)
	}
	ffmpeg, _ := ffmpegBinaries(t)
	frame, err := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-ss", "2", "-i", clip,
		"-frames:v", "1", "-f", "rawvideo", "-pix_fmt", "gray", "-").Output()
	if err != nil || len(frame) == 0 {
		t.Fatalf("decode a frame: %v", err)
	}
	var sum int
	for _, b := range frame {
		sum += int(b)
	}
	if mean := sum / len(frame); mean < 200 {
		t.Errorf("frame at 2 s averages %d; want the white box drawn in (above 200)", mean)
	}
}

// A TV's link reaches Yoru with no token, opens one session's files only, and answers the receiver's CORS asks.
func TestCastLinks(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	h.server.token = "s3cret"
	h.server.streams = transcode.NewManager("ffmpeg", "ffprobe", t.TempDir(), "libx264", slog.New(slog.DiscardHandler))
	tv := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host, req.RemoteAddr = "192.168.1.10:4321", "192.168.1.60:5555"
		req.Header.Set("Origin", "https://www.gstatic.com")
		rec := httptest.NewRecorder()
		h.handler.ServeHTTP(rec, req)
		return rec
	}
	cast := h.server.signGrant(grant{Source: castPrefix + "1-3", Expires: time.Now().Add(time.Hour).Unix()})
	app := h.server.signGrant(grant{Source: "http://127.0.0.1:3030/torrents/1/stream/0", Expires: time.Now().Add(time.Hour).Unix()})
	gone := h.server.signGrant(grant{Source: castPrefix + "1-3", Expires: time.Now().Add(-time.Minute).Unix()})

	if rec := tv("OPTIONS", "/c/"+cast+"/playlist.m3u8"); rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight: %d, CORS %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
	// Past the grant, the session is looked up: none here, so 404, but never the token's 401.
	if rec := tv("GET", "/c/"+cast+"/playlist.m3u8"); rec.Code != http.StatusNotFound || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("a cast link: %d, want it past the guard to the session lookup", rec.Code)
	}
	for name, grant := range map[string]string{"an app's file link": app, "an expired link": gone, "a made-up link": "abc.def"} {
		if rec := tv("GET", "/c/"+grant+"/playlist.m3u8"); rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, rec.Code)
		}
	}
	if rec := tv("POST", "/c/"+cast+"/playlist.m3u8"); rec.Code == http.StatusOK {
		t.Error("a TV link took a POST")
	}
}
