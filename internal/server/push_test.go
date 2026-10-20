package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"testing"

	"yoru/internal/config"
	"yoru/internal/store"
)

// Only the browsers' push services are accepted: a subscription is an address Yoru posts to.
func TestPushSubscriptionsOnlyToPushServices(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	keys := map[string]string{
		"p256dh": base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...)),
		"auth":   base64.RawURLEncoding.EncodeToString(make([]byte, 16)),
	}
	for endpoint, want := range map[string]int{
		"https://fcm.googleapis.com/fcm/send/abc":                200,
		"https://updates.push.services.mozilla.com/wpush/v2/abc": 200,
		"https://web.push.apple.com/abc":                         200,
		"http://fcm.googleapis.com/fcm/send/abc":                 400,
		"https://192.168.1.1/admin":                              400,
		"https://evil.example/fcm.googleapis.com":                400,
		"https://fcm.googleapis.com:8443/x":                      400,
	} {
		res, _ := sendJSON(t, h, "POST", "/api/push/subscribe", map[string]any{"endpoint": endpoint, "keys": keys})
		if res.StatusCode != want {
			t.Errorf("%s: HTTP %d, want %d", endpoint, res.StatusCode, want)
		}
	}
	short := map[string]string{"p256dh": "AAAA", "auth": keys["auth"]}
	if res, _ := sendJSON(t, h, "POST", "/api/push/subscribe", map[string]any{"endpoint": "https://fcm.googleapis.com/x", "keys": short}); res.StatusCode != 400 {
		t.Errorf("a malformed key: HTTP %d, want 400", res.StatusCode)
	}

	subs, _ := h.store.PushSubscriptions(context.Background())
	if len(subs) != 3 || subs[0].Origin == "" {
		t.Fatalf("stored %+v; want the three push services, each with its origin", subs)
	}
	sendJSON(t, h, "POST", "/api/push/unsubscribe", map[string]any{"endpoint": "https://web.push.apple.com/abc"})
	if subs, _ := h.store.PushSubscriptions(context.Background()); len(subs) != 2 {
		t.Errorf("after unsubscribing: %d left, want 2", len(subs))
	}
}

// A device's alerts end with its access: declined, removed, or all paired ones when pairing is reset.
func TestPushStopsWithTheDevicesAccess(t *testing.T) {
	h := approvalHarness(t, approvalOnce)
	keys := `"keys":{"p256dh":"` + base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...)) +
		`","auth":"` + base64.RawURLEncoding.EncodeToString(make([]byte, 16)) + `"}`
	endpoint := func(name string) string { return "https://fcm.googleapis.com/fcm/send/" + name }
	left := func() (names []string) {
		subs, _ := h.store.PushSubscriptions(context.Background())
		for _, sub := range subs {
			names = append(names, strings.TrimPrefix(sub.Endpoint, endpoint("")))
		}
		slices.Sort(names)
		return names
	}

	var ids []string
	phones := map[string]*device{}
	for i, name := range []string{"declined", "removed", "kept"} {
		d := newDevice(t, h)
		d.addr = fmt.Sprintf("192.168.1.%d:5555", 60+i)
		d.get("/?token=s3cret", true)
		for _, dev := range h.devices(t) {
			if id := dev["id"].(string); !slices.Contains(ids, id) {
				ids = append(ids, id)
				h.host(t, "POST", "/api/access/devices/"+id, `{"status":"approved"}`)
			}
		}
		if res := d.do("POST", "/api/push/subscribe", `{"endpoint":"`+endpoint(name)+`",`+keys+`}`, false); res.StatusCode != 200 {
			t.Fatalf("%s subscribing: HTTP %d", name, res.StatusCode)
		}
		phones[name] = d
	}
	if rec := h.host(t, "POST", "/api/push/subscribe", `{"endpoint":"`+endpoint("host")+`",`+keys+`}`); rec.Code != 200 {
		t.Fatalf("the host subscribing: HTTP %d %s", rec.Code, rec.Body)
	}

	h.host(t, "POST", "/api/access/devices/"+ids[0], `{"status":"denied"}`)
	h.host(t, "DELETE", "/api/access/devices/"+ids[1], "")
	if got := left(); !slices.Equal(got, []string{"host", "kept"}) {
		t.Fatalf("after declining one and removing another: %v, want host and kept", got)
	}
	h.host(t, "POST", "/api/access/rotate", "")
	if got := left(); !slices.Equal(got, []string{"host"}) {
		t.Errorf("after resetting pairing: %v, want only the host machine's", got)
	}
}

// A release alert names the show and opens the episode, tagged as the open tab's own alert is.
func TestReleaseAlert(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	seedShow(t, h, 7, 12)
	anime, episode := 7, 3
	alert, ok := ReleaseAlert(context.Background(), h.store, store.Notification{
		ID: 42, Kind: store.NotifyRelease, AnimeID: &anime, Episode: &episode, Title: "Episode 3 is available",
	})
	if !ok || alert.Title == "" || alert.Title == "Yoru" || alert.Body != "Episode 3 is available" ||
		alert.Tag != "yoru-42" || alert.Path != "/watch/7/3" {
		t.Errorf("alert = %+v, %v", alert, ok)
	}
	if _, ok := ReleaseAlert(context.Background(), h.store, store.Notification{Kind: store.NotifyUpdate}); ok {
		t.Error("an update notice was pushed to phones")
	}
}
