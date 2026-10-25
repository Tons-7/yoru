package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yoru/internal/config"
	"yoru/internal/jimaku"
)

// A key is kept only once Jimaku takes it, and no answer of Yoru's gives it back.
func TestJimakuKeyIsKeptSecret(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "good-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		io.WriteString(w, "[]")
	}))
	defer site.Close()
	h := newHarness(t, config.Config{}, nil)
	h.server.jimaku = jimaku.NewAt(site.URL, site.Client())

	if res, out := h.postJSON(t, "/api/jimaku/key", map[string]string{"key": "bad-key"}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("a key Jimaku refuses: HTTP %d %v", res.StatusCode, out)
	}
	if _, out := h.do(t, "GET", "/api/jimaku"); out["set"] != false {
		t.Errorf("after a refused key: %v, want not set", out)
	}
	if res, out := h.postJSON(t, "/api/jimaku/key", map[string]string{"key": "good-key"}); res.StatusCode != http.StatusOK || out["set"] != true {
		t.Fatalf("a good key: HTTP %d %v", res.StatusCode, out)
	}
	if _, out := h.do(t, "GET", "/api/jimaku"); out["set"] != true {
		t.Errorf("after a good key: %v, want set", out)
	}
	code, header, body := h.fetch(t, "GET", "/api/prefs")
	raw, _ := json.Marshal(header)
	if code != http.StatusOK || !strings.Contains(string(body), "effective") || strings.Contains(string(body)+string(raw), "good-key") {
		t.Errorf("the settings answer: HTTP %d, gives the key back or isn't there: %.200s", code, body)
	}
	if _, out := h.postJSON(t, "/api/jimaku/key", map[string]string{"key": ""}); out["set"] != false {
		t.Errorf("forgetting the key: %v", out)
	}
}
