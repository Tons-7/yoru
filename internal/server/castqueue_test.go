package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"yoru/internal/config"
	"yoru/internal/library"
)

// With Auto next on the TV gets what plays after, no more than Still watching lets play unasked, each a link
// for the episode with the shown subtitles' language; with it off, nothing past the episode cast.
func TestCastQueueFollowsAutoNext(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	ctx := context.Background()
	seedShow(t, h, 7, 12)
	expires := time.Now().Add(time.Hour)

	if q := h.server.castQueue(ctx, 7, 1, "http://tv", expires, "eng"); len(q) != 0 {
		t.Errorf("Auto next off: queued %d", len(q))
	}
	for key, value := range map[string]string{"playback.autonext": "true", "playback.still_watching": "3"} {
		if err := h.store.SetAnimePref(ctx, 7, key, value); err != nil {
			t.Fatal(err)
		}
	}
	q := h.server.castQueue(ctx, 7, 1, "http://tv", expires, "eng")
	if len(q) != 3 || q[0].Key != "7-2" || q[2].Key != "7-4" {
		t.Fatalf("queue = %+v, want episodes 2-4", q)
	}
	grant, found := strings.CutPrefix(q[0].URL, "http://tv/c/")
	g, ok := h.server.openGrant(strings.TrimSuffix(grant, "/playlist.m3u8"), time.Now())
	if !found || !ok {
		t.Fatalf("the link doesn't open: %s", q[0].URL)
	}
	if anime, episode, lang, ok := parseCastEpisode(g.Source); !ok || anime != 7 || episode != 2 || lang != "eng" {
		t.Errorf("link names %d %d %q (%v)", anime, episode, lang, ok)
	}
	if len(q[0].Subtitles) != 1 || !strings.HasSuffix(q[0].Subtitles[0].URL, "/sub-auto.vtt") || q[0].Subtitle != "Episode 2" {
		t.Errorf("item = %+v", q[0])
	}
}

// A queued episode opens on the TV's first ask, as a page would open it: playlist, init, subtitles in the
// cast one's language, and a segment, which notes where the TV has got to.
func TestTheTVOpensAQueuedEpisodeItself(t *testing.T) {
	h, _ := streamHarness(t)
	h.server.playback = library.NewPlayback(h.store, nil, nil, nil, t.TempDir(), slog.New(slog.DiscardHandler))
	root := "/c/" + h.server.signGrant(grant{Source: castEpisodePrefix + "1-1:eng", Expires: time.Now().Add(time.Hour).Unix()})

	code, _, playlist := h.fetch(t, "GET", root+"/playlist.m3u8")
	if code != http.StatusOK || !bytes.HasPrefix(playlist, []byte("#EXTM3U")) {
		t.Fatalf("the queued episode's playlist answered %d: %.120s", code, playlist)
	}
	if code, _, init := h.fetch(t, "GET", root+"/init.mp4"); code != http.StatusOK || !bytes.Contains(init[:min(len(init), 64)], []byte("ftyp")) {
		t.Errorf("init answered %d", code)
	}
	// By its first cue's time: this file's short ASS format leaves WebVTT's text empty, on every track route.
	if code, _, vtt := h.fetch(t, "GET", root+"/sub-auto.vtt"); code != http.StatusOK || !bytes.Contains(vtt, []byte("00:01.000 --> 00:05.000")) {
		t.Errorf("its English subtitles answered %d: %.120s", code, vtt)
	}
	if code, _, seg := h.fetch(t, "GET", root+"/0.mp4"); code != http.StatusOK || !bytes.Contains(seg[:min(len(seg), 256)], []byte("moof")) {
		t.Errorf("segment 0 answered %d", code)
	}
	h.server.tvMu.Lock()
	_, noted := h.server.tvSeen["1-1"]
	h.server.tvMu.Unlock()
	if !noted {
		t.Error("the TV's ask wasn't noted for its progress")
	}
	// Asked again, the same session: opened once.
	if code, _, _ := h.fetch(t, "GET", root+"/playlist.m3u8"); code != http.StatusOK || len(h.server.tvOpens) != 1 {
		t.Errorf("asked again: %d, %d opens", code, len(h.server.tvOpens))
	}
	// The language tagged another way still finds it.
	short := "/c/" + h.server.signGrant(grant{Source: castEpisodePrefix + "1-1:en", Expires: time.Now().Add(time.Hour).Unix()})
	if code, _, vtt := h.fetch(t, "GET", short+"/sub-auto.vtt"); code != http.StatusOK || !bytes.Contains(vtt, []byte("00:01.000")) {
		t.Errorf(`"en" asked of an "eng" track answered %d: %.80s`, code, vtt)
	}
	// Another language than the episode has: an empty track rather than the wrong one, or one that fails.
	other := "/c/" + h.server.signGrant(grant{Source: castEpisodePrefix + "1-1:fre", Expires: time.Now().Add(time.Hour).Unix()})
	if code, _, vtt := h.fetch(t, "GET", other+"/sub-auto.vtt"); code != http.StatusOK || string(vtt) != "WEBVTT\n" {
		t.Errorf("French asked of an English-only episode answered %d: %q, want an empty track", code, vtt)
	}
}

func TestSameLanguage(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"eng", "en", true}, {"en-US", "ENG", true}, {"jpn", "ja", true}, {"fre", "fra", true}, {"eng", "jpn", false},
		{"", "", false}, {"und", "eng", false}, {"xx", "xx", true}, {"xx", "eng", false}} {
		if got := sameLanguage(c.a, c.b); got != c.want {
			t.Errorf("sameLanguage(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// The TV's place moves from its second ask, no oftener than every 10 s and never faster than the clock; back
// to an earlier place it counts on from there; and a page's recent report stands instead.
func TestTheTVsPlaceIsEstimatedFromWhatItReads(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	s, now := h.server, time.Now()
	if _, save := s.tvEstimate("7-2", 40, now); save {
		t.Error("the first ask saved: it is only where the TV started")
	}
	if _, save := s.tvEstimate("7-2", 46, now.Add(5*time.Second)); save {
		t.Error("saved again within 10 s")
	}
	if played, save := s.tvEstimate("7-2", 100, now.Add(12*time.Second)); !save || played != 12 {
		t.Errorf("after 12 s: played %v, save %v; want 12 s played (a seek forward isn't watching)", played, save)
	}
	if _, save := s.tvEstimate("7-2", 90, now.Add(30*time.Second)); save {
		t.Error("a seek back saved at once")
	}
	if played, save := s.tvEstimate("7-2", 105, now.Add(42*time.Second)); !save || played != 12 {
		t.Errorf("after a seek back: played %v, save %v; want it counted on from there", played, save)
	}
	// The same episode again, the same evening.
	s.tvEstimate("7-2", 0, now.Add(60*time.Second))
	if played, save := s.tvEstimate("7-2", 15, now.Add(75*time.Second)); !save || played != 15 {
		t.Errorf("watched again: played %v, save %v", played, save)
	}
	s.pageReported["7-2"] = now.Add(80 * time.Second)
	if _, save := s.tvEstimate("7-2", 130, now.Add(95*time.Second)); save {
		t.Error("the estimate overwrote a page's exact report")
	}
}
