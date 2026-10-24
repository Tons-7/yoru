package server

import (
	"context"
	"net/http/httptest"
	"testing"

	"yoru/internal/config"
)

// Another device playing keeps the computer awake until its reports stop, unless Settings say otherwise.
func TestAnotherDeviceWatchingKeepsTheComputerAwake(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	ctx := context.Background()
	if why := h.server.awakeReason(ctx); why != "" {
		t.Fatalf("awake with nothing going on: %q", why)
	}

	phone := httptest.NewRequest("POST", "/api/progress", nil)
	phone.RemoteAddr = "192.168.1.50:5555"
	h.server.noteWatching(phone, 1, 3, false)
	if why := h.server.awakeReason(ctx); why != "another device is watching" {
		t.Errorf("a phone playing: %q", why)
	}
	if err := h.store.SetSetting(ctx, "power.awake_watching", "false"); err != nil {
		t.Fatal(err)
	}
	if why := h.server.awakeReason(ctx); why != "" {
		t.Errorf("turned off, yet awake: %q", why)
	}
	h.store.SetSetting(ctx, "power.awake_watching", "true")
	h.server.noteWatching(phone, 1, 3, true)
	if why := h.server.awakeReason(ctx); why != "" {
		t.Errorf("the phone stopped, yet awake: %q", why)
	}

	// A TV reading a cast stream counts while it reads.
	done := h.server.serving()
	if why := h.server.awakeReason(ctx); why != "another device is watching" {
		t.Errorf("a TV casting: %q", why)
	}
	done()
}
