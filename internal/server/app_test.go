package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yoru/internal/config"
)

// Exit Yoru and a second launch's show reach main's callbacks.
func TestAppExitAndShow(t *testing.T) {
	exited, shown := make(chan struct{}), make(chan struct{})
	h := newHarness(t, config.Config{}, nil, func(d *Deps) {
		d.Exit = func() { close(exited) }
		d.ShowWindow = func() { close(shown) }
	})
	if rec := h.host(t, "POST", "/api/app/show", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("show: %d %s", rec.Code, rec.Body)
	}
	if rec := h.host(t, "POST", "/api/app/exit", ""); rec.Code != http.StatusAccepted {
		t.Fatalf("exit: %d %s", rec.Code, rec.Body)
	}
	for name, ch := range map[string]chan struct{}{"show": shown, "exit": exited} {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Errorf("%s never reached its callback", name)
		}
	}
}

// Another device playing shows in what exiting would stop, by name and show, until it pauses; the host's
// own playing isn't another device.
func TestActivityListsOtherDevicesPlaying(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	h.server.token = "s3cret"
	seedShow(t, h, 7, 12)
	phone := newDevice(t, h)
	phone.get("/?token=s3cret", false)
	report := func(d *device, paused bool) {
		t.Helper()
		body := fmt.Sprintf(`{"animeId":7,"episode":3,"position":60,"duration":1400,"played":10,"paused":%v}`, paused)
		var code int
		if d == nil {
			code = h.host(t, "POST", "/api/progress", body).Code
		} else {
			code = d.do("POST", "/api/progress", body, false).StatusCode
		}
		if code != http.StatusOK {
			t.Fatalf("progress: HTTP %d", code)
		}
	}
	activity := func() []map[string]any {
		t.Helper()
		var out struct {
			Watching []map[string]any `json:"watching"`
		}
		json.Unmarshal(h.host(t, "GET", "/api/app/activity", "").Body.Bytes(), &out)
		return out.Watching
	}

	report(nil, false)
	if got := activity(); len(got) != 0 {
		t.Fatalf("the host's own playing counted: %v", got)
	}
	report(phone, false)
	got := activity()
	if len(got) != 1 || got[0]["device"] != "Chrome on Android" || got[0]["title"] == "" || got[0]["episode"] != float64(3) {
		t.Fatalf("activity = %v, want the phone on episode 3", got)
	}
	report(phone, true)
	if got := activity(); len(got) != 0 {
		t.Errorf("a paused phone still counts: %v", got)
	}

	// Two devices behind one router share its address: each is its own, and one pausing leaves the other.
	tablet := newDevice(t, h)
	tablet.ua = "Mozilla/5.0 (Linux; Android 14; SM-X710) AppleWebKit/537.36 Firefox/128.0"
	tablet.get("/?token=s3cret", false)
	report(phone, false)
	report(tablet, false)
	if got := activity(); len(got) != 2 {
		t.Fatalf("activity = %v, want both devices at one address", got)
	}
	report(phone, true)
	if got := activity(); len(got) != 1 {
		t.Errorf("one device pausing took the other with it: %v", got)
	}
}

// The log to copy or send is the last run's then this one's, named, and saved as a file when asked.
func TestLogIsTheLastRunAndThisOne(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "yoru.log.1"), []byte("msg=before\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "yoru.log"), []byte("msg=now"), 0o644)
	h := newHarness(t, config.Config{}.WithDataDir(dir), nil)

	rec := h.host(t, "GET", "/api/log", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Previous run") || !strings.Contains(body, "This run") ||
		strings.Index(body, "msg=before") > strings.Index(body, "msg=now") || !strings.HasSuffix(body, "msg=now\n") {
		t.Fatalf("log: %d %q", rec.Code, body)
	}
	if rec.Header().Get("Content-Disposition") != "" {
		t.Error("a copy was sent as a download")
	}
	rec = h.host(t, "GET", "/api/log?download=1", "")
	if d := rec.Header().Get("Content-Disposition"); !strings.Contains(d, "attachment") || !strings.Contains(d, ".txt") {
		t.Errorf("a save got %q", d)
	}
}
