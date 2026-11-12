package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yoru/internal/config"
)

func TestARestartForAnUpdateKnowsWhatItInterrupts(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	ctx := context.Background()
	if why := h.server.busyWith(ctx); len(why) != 0 {
		t.Fatalf("a fresh Yoru is busy with %q", why)
	}
	if !h.server.autoUpdates(ctx) {
		t.Fatal("automatic updates are off on a fresh Yoru")
	}

	openRoom(t, h)
	if why := strings.Join(h.server.busyWith(ctx), "; "); !strings.Contains(why, "room") {
		t.Errorf("with a watch-together room open: %q", why)
	}
	h.server.togetherMu.Lock()
	delete(h.server.rooms, "abc123")
	h.server.togetherMu.Unlock()

	phone := httptest.NewRequest("POST", "/api/progress", nil)
	phone.RemoteAddr = "192.168.1.50:5555"
	h.server.noteWatching(phone, 1, 3, false)
	if why := strings.Join(h.server.busyWith(ctx), "; "); !strings.Contains(why, "device") {
		t.Errorf("with a phone playing: %q", why)
	}
	h.server.noteWatching(phone, 1, 3, true)
	if why := h.server.busyWith(ctx); len(why) != 0 {
		t.Errorf("with everything stopped: %q", why)
	}

	if code, _ := h.call(t, "POST", "/api/prefs", `{"key":"update.auto","value":"false"}`); code != http.StatusOK {
		t.Fatalf("turning automatic updates off answered %d", code)
	}
	if h.server.autoUpdates(ctx) {
		t.Error("turned off, yet on")
	}
}
