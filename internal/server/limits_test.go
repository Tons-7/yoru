package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"yoru/internal/config"
	"yoru/internal/torrent"
)

// limitsEngine records what the engine is handed.
type limitsEngine struct{ got []torrent.Limits }

func (e *limitsEngine) Ensure(context.Context) error { return nil }
func (e *limitsEngine) SetLimits(l torrent.Limits)   { e.got = append(e.got, l) }
func (e *limitsEngine) Network() torrent.Network {
	return torrent.Network{Adapter: "ProtonVPN", Address: "10.2.0.2"}
}

// The network torrents keep to is the host's to choose, and to see: a paired phone gets neither.
func TestTorrentNetworkIsTheHosts(t *testing.T) {
	eng := &limitsEngine{}
	h := newHarness(t, config.Config{}, nil, func(d *Deps) {
		d.Engine = torrent.NewClient("http://127.0.0.1:1").WithEngine(eng)
	})
	h.server.token = "s3cret"
	phone := http.Header{"Authorization": {"Bearer s3cret"}}

	if res := h.from(t, "192.168.1.50:5555", "/api/network", phone); res.StatusCode != http.StatusForbidden {
		t.Errorf("a phone listed this machine's networks: HTTP %d", res.StatusCode)
	}
	raw, _ := json.Marshal(map[string]any{"key": "downloads.adapter", "value": "ProtonVPN"})
	req := httptest.NewRequest("POST", "/api/prefs", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer s3cret")
	req.Host, req.RemoteAddr = "192.168.1.10:4321", "192.168.1.50:5555"
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || len(eng.got) != 0 {
		t.Errorf("a phone chose the network: HTTP %d, engine got %+v", rec.Code, eng.got)
	}

	res := h.from(t, "127.0.0.1:5555", "/api/network", nil)
	var body struct {
		Adapters []netAdapter    `json:"adapters"`
		Network  torrent.Network `json:"network"`
	}
	json.NewDecoder(res.Body).Decode(&body)
	if res.StatusCode != http.StatusOK || body.Network.Address != "10.2.0.2" {
		t.Errorf("the host's view: HTTP %d %+v", res.StatusCode, body)
	}
	h.postJSON(t, "/api/prefs", map[string]any{"key": "downloads.adapter", "value": "ProtonVPN"})
	if len(eng.got) != 1 || eng.got[0].Adapter != "ProtonVPN" {
		t.Errorf("the host's choice reached the engine as %+v", eng.got)
	}
}

// Saving the download cap or the upload choice reaches the engine at once; a bad value is refused.
func TestLimitsReachTheEngine(t *testing.T) {
	eng := &limitsEngine{}
	h := newHarness(t, config.Config{}, nil, func(d *Deps) {
		d.Engine = torrent.NewClient("http://127.0.0.1:1").WithEngine(eng)
	})
	h.postJSON(t, "/api/prefs", map[string]any{"key": "downloads.limit_bytes", "value": "1048576"})
	h.postJSON(t, "/api/prefs", map[string]any{"key": "downloads.upload", "value": "never"})
	for _, bad := range [][2]string{{"downloads.upload", "always"}, {"downloads.limit_bytes", "fast"}} {
		if res, _ := h.postJSON(t, "/api/prefs", map[string]any{"key": bad[0], "value": bad[1]}); res.StatusCode != 400 {
			t.Errorf("%s = %q: HTTP %d, want 400", bad[0], bad[1], res.StatusCode)
		}
	}
	want := []torrent.Limits{
		{DownloadBytes: 1 << 20, Upload: torrent.UploadWhileDownloading},
		{DownloadBytes: 1 << 20, Upload: torrent.UploadNever},
	}
	if !slices.Equal(eng.got, want) {
		t.Errorf("engine got %+v, want %+v", eng.got, want)
	}
}
