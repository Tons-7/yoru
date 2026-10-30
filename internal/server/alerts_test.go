package server

import (
	"context"
	"net/http"
	"testing"

	"yoru/internal/store"
)

func TestAlertsListTheShowsThatAlert(t *testing.T) {
	h := watchHarness(t)
	ctx := context.Background()
	if err := h.store.SetNotify(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	code, body := h.call(t, "GET", "/api/alerts", "")
	if code != http.StatusOK {
		t.Fatalf("alerts answered %d %v", code, body)
	}
	shows := body["shows"].([]any)
	if len(shows) != 1 || body["enabled"] != true {
		t.Fatalf("alerts = %v", body)
	}
	show := shows[0].(map[string]any)
	if show["animeId"] != float64(1) || show["title"] != "Show" || show["because"] != "followed" {
		t.Errorf("show = %v", show)
	}

	if err := h.store.SetNotify(ctx, 1, false); err != nil {
		t.Fatal(err)
	}
	if _, body := h.call(t, "GET", "/api/alerts", ""); len(body["shows"].([]any)) != 0 {
		t.Errorf("a show turned off still alerts: %v", body)
	}
}

func TestNotificationsNameTheirShow(t *testing.T) {
	h := watchHarness(t)
	anime, episode := 1, 3
	if _, err := h.store.AddNotification(context.Background(), store.Notification{
		Kind: store.NotifyRelease, AnimeID: &anime, Episode: &episode, Title: "Episode 3 is available",
	}); err != nil {
		t.Fatal(err)
	}
	_, body := h.call(t, "GET", "/api/notifications", "")
	item := body["items"].([]any)[0].(map[string]any)
	if item["show"] != "Show" || item["title"] != "Episode 3 is available" || item["episode"] != float64(3) {
		t.Errorf("notification = %v", item)
	}
}
