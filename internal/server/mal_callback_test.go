package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"yoru/internal/config"
	"yoru/internal/mal"
)

// The state is all that stands between any web page and a stranger's MAL account linked here: a wrong or
// missing one is refused before the code is exchanged, and a used one can't be replayed.
func TestMALCallbackChecksTheStateOnce(t *testing.T) {
	var exchanges atomic.Int32
	malSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			exchanges.Add(1)
			io.WriteString(w, `{"access_token":"acc","refresh_token":"ref","expires_in":3600}`)
			return
		}
		io.WriteString(w, `{"id":1,"name":"tony"}`)
	}))
	t.Cleanup(malSrv.Close)
	h := newHarness(t, config.Config{Addr: "127.0.0.1:4321"}, nil, func(d *Deps) {
		d.MAL = mal.New(d.Log, mal.WithAPI(malSrv.URL), mal.WithOAuthBase(malSrv.URL),
			mal.WithCredentials("id", ""), mal.WithoutRateLimit())
	})
	ctx := t.Context()
	callback := func(state string) int {
		res, _ := h.do(t, "GET", "/mal/callback?state="+state+"&code=c")
		return res.StatusCode
	}

	// No login was started here, so there is no state to match.
	if code := callback(""); code != http.StatusBadRequest {
		t.Fatalf("no stored state: HTTP %d, want 400", code)
	}
	h.store.SetSettings(ctx, map[string]string{"mal.state": "s", "mal.verifier.token": "v"})
	if code := callback("wrong"); code != http.StatusBadRequest {
		t.Fatalf("wrong state: HTTP %d, want 400", code)
	}
	if n := exchanges.Load(); n != 0 {
		t.Fatalf("%d code exchanges before a matching state, want none", n)
	}

	if code := callback("s"); code != http.StatusFound {
		t.Fatalf("matching state: HTTP %d, want 302", code)
	}
	if tok, _ := h.store.Setting(ctx, "mal.token"); tok != "acc" {
		t.Fatalf("stored token = %q", tok)
	}
	// The same callback again, replayed: the state went with its first use.
	if code := callback("s"); code != http.StatusBadRequest {
		t.Fatalf("replayed callback: HTTP %d, want 400", code)
	}
	if n := exchanges.Load(); n != 1 {
		t.Fatalf("%d code exchanges, want the one", n)
	}
}
