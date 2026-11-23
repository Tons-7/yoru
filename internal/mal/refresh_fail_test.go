package mal

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func refreshFailing(t *testing.T, status int, body string) (*Client, *atomic.Int32, *atomic.Value) {
	t.Helper()
	var calls atomic.Int32
	var auth atomic.Value
	auth.Store("")
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			w.WriteHeader(status)
			io.WriteString(w, body)
			return
		}
		calls.Add(1)
		auth.Store(r.Header.Get("Authorization"))
		io.WriteString(w, `{"id":1,"name":"tony"}`)
	})
	return c, &calls, &auth
}

func TestRefreshHiccupStillSendsWithAValidToken(t *testing.T) {
	c, calls, auth := refreshFailing(t, http.StatusBadGateway, "busy")
	c.SetToken(Token{Access: "stale", Refresh: "ref-1", Expires: time.Now().Add(3 * time.Minute)})
	if _, err := c.Viewer(t.Context()); err != nil {
		t.Fatalf("a refresh hiccup with a token still valid: %v", err)
	}
	if calls.Load() != 1 || auth.Load() != "Bearer stale" {
		t.Fatalf("%d calls with %q, want one with the still-valid token", calls.Load(), auth.Load())
	}
}

func TestRevokedRefreshStopsTheRequest(t *testing.T) {
	c, calls, _ := refreshFailing(t, http.StatusBadRequest, `{"error":"invalid_grant"}`)
	c.SetToken(Token{Access: "stale", Refresh: "ref-1", Expires: time.Now().Add(3 * time.Minute)})
	_, err := c.Viewer(t.Context())
	if err == nil || !Unauthorized(err) {
		t.Fatalf("a revoked refresh token: %v, want an unauthorized error", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("%d API calls after the refresh was refused", calls.Load())
	}
}

func TestExpiredTokenWithFailedRefreshErrors(t *testing.T) {
	c, calls, _ := refreshFailing(t, http.StatusBadGateway, "busy")
	c.SetToken(Token{Access: "stale", Refresh: "ref-1", Expires: time.Now().Add(-time.Minute)})
	if _, err := c.Viewer(t.Context()); err == nil {
		t.Fatal("an expired token whose refresh failed still went through")
	}
	if calls.Load() != 0 {
		t.Fatalf("%d API calls with an expired token", calls.Load())
	}
}
