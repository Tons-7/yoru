package httpTracker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func announceTo(t *testing.T, path string, handler http.HandlerFunc) (AnnounceResponse, error) {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL + path)
	require.NoError(t, err)
	cl := NewClient(u, NewClientOpts{})
	t.Cleanup(func() { cl.Close() })
	return cl.Announce(context.Background(), AnnounceRequest{Left: -1}, AnnounceOpt{})
}

func TestHTTPAnnounce(t *testing.T) {
	resp, err := announceTo(t, "/announce", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "1", r.URL.Query().Get("compact"))
		w.Write([]byte("d8:completei3e10:incompletei4e8:intervali1800e5:peers12:\x01\x02\x03\x04\x00\x50\x05\x06\x07\x08\x00\x516:peers618:\x20\x01\x0d\xb8\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01\x1f\x90e"))
	})
	require.NoError(t, err)
	assert.Len(t, resp.Peers, 3)
	assert.EqualValues(t, 3, resp.Seeders)
	assert.EqualValues(t, 4, resp.Leechers)

	_, err = announceTo(t, "/announce", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("d14:failure reason7:go awaye"))
	})
	assert.ErrorContains(t, err, "go away")

	_, err = announceTo(t, "/announce", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(strings.Repeat("x", 10000)))
	})
	require.Error(t, err)
	assert.Less(t, len(err.Error()), 400)

	_, err = announceTo(t, "/announce", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", maxResponseBytes+10)))
	})
	assert.ErrorContains(t, err, "over")
}

func TestLocalTrackerNeedsAnnouncePath(t *testing.T) {
	called := false
	_, err := announceTo(t, "/api/library/export", func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	assert.Error(t, err)
	assert.False(t, called)
}

func TestLocalAddresses(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "::1", "10.1.2.3", "192.168.1.1", "169.254.0.1", "0.0.0.0", "::ffff:192.168.1.1"} {
		assert.True(t, localAddr(netip.MustParseAddr(s)), s)
	}
	for _, s := range []string{"1.1.1.1", "2001:4860::1"} {
		assert.False(t, localAddr(netip.MustParseAddr(s)), s)
	}
	assert.True(t, localHost("LocalHost"))
	assert.False(t, localHost("tracker.example.org"))
}
