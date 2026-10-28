package server

import (
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"yoru/internal/config"
	"yoru/internal/library"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

// The callback installs the new token only once the account is known and saved: a failure halfway
// leaves the client as it was, not carrying a token Settings says is not connected.
func TestAniListCallbackInstallsTheTokenOnlyOnceSaved(t *testing.T) {
	viewerFails := true
	h := newHarness(t, config.Config{Addr: "127.0.0.1:4321"}, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			io.WriteString(w, `{"access_token":"fresh","expires_in":3600}`)
			return
		}
		if viewerFails {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `{"errors":[{"message":"temporarily disabled","status":403}]}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			t.Errorf("viewer asked with %q, want the new token", r.Header.Get("Authorization"))
		}
		io.WriteString(w, `{"data":{"Viewer":{"id":7,"name":"tony","mediaListOptions":{"scoreFormat":"POINT_100"}}}}`)
	})
	ctx := t.Context()
	h.store.SetSetting(ctx, "anilist.client_id", "1")

	callback := func() int {
		h.store.SetSetting(ctx, "anilist.state", "s")
		res, _ := h.do(t, "GET", "/callback?state=s&code=c")
		return res.StatusCode
	}

	if code := callback(); code != http.StatusBadGateway {
		t.Fatalf("status = %d, want the viewer failure", code)
	}
	if h.server.anilist.Authenticated() {
		t.Fatal("a half-finished login left its token on the live client")
	}
	if tok, _ := h.store.Setting(ctx, "anilist.token"); tok != "" {
		t.Fatalf("stored token = %q", tok)
	}

	viewerFails = false
	if code := callback(); code != http.StatusFound {
		t.Fatalf("status = %d", code)
	}
	if !h.server.anilist.Authenticated() {
		t.Fatal("a finished login should install the token")
	}
	if name, _ := h.store.Setting(ctx, "anilist.user_name"); name != "tony" {
		t.Fatalf("user = %q", name)
	}
}

// A token pasted from AniList's pin page connects in one step, whole address and all, with the expiry the
// token carries; one AniList refuses, or text that isn't one, leaves the login as it was.
func TestPastedAniListTokenConnects(t *testing.T) {
	h := newHarness(t, config.Config{}, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+jwt {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"errors":[{"message":"Invalid token","status":401}]}`)
			return
		}
		io.WriteString(w, `{"data":{"Viewer":{"id":7,"name":"tony","mediaListOptions":{"scoreFormat":"POINT_10"}}}}`)
	})
	ctx := t.Context()

	for _, bad := range []string{`{"token":"not one at all"}`, `{"token":"nope"}`} {
		if code, _ := h.call(t, "POST", "/api/auth/token", bad); code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want refused", bad, code)
		}
	}
	if h.server.anilist.Authenticated() {
		t.Fatal("a refused token was installed")
	}

	pasted := `{"token":"https://anilist.co/api/v2/oauth/pin#access_token=` + jwt + `&token_type=Bearer&expires_in=31536000"}`
	if code, body := h.call(t, "POST", "/api/auth/token", pasted); code != http.StatusOK || body["user"] != "tony" {
		t.Fatalf("status %d %v", code, body)
	}
	if !h.server.anilist.Authenticated() {
		t.Fatal("the pasted token wasn't installed")
	}
	if at, _ := h.store.Setting(ctx, "anilist.expires_at"); at != "4102444800" {
		t.Errorf("expires_at = %s, want the token's own exp", at)
	}
}

// jwt is a token whose claims say it expires at the start of 2100.
var jwt = "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":4102444800}`)) + ".sig"

// The reaper taking a session nobody closed releases the episode the way a close does: unpinned, so the
// cache may take it, and the queue's hold is given back.
func TestIdleSessionReleasesTheEpisode(t *testing.T) {
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"torrents":[]}`)
	}))
	t.Cleanup(engine.Close)
	h := newHarness(t, config.Config{Addr: "127.0.0.1:4321"}, nil, func(d *Deps) {
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		d.Playback = library.NewPlayback(d.Store, nil, torrent.NewClient(engine.URL), nil, t.TempDir(), log)
	})
	ctx := t.Context()
	if err := h.store.RecordTorrent(ctx, store.TorrentRecord{
		InfoHash: "abc", EngineID: 1, Name: "x", AnimeID: 1, EpKey: "5", FilePath: "x.mkv", TotalSize: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.store.PinCache(ctx, "abc", 0, true); err != nil {
		t.Fatal(err)
	}

	h.server.SessionIdle("1-5")
	entries, _ := h.store.CacheEntries(ctx)
	if len(entries) != 1 || entries[0].Pinned {
		t.Fatalf("entries = %+v, want the episode unpinned", entries)
	}
}
