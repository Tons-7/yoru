package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"yoru/internal/push"
	"yoru/internal/store"
)

// pushHosts are the browsers' push services (Chrome and most others use Google's, then Firefox, Safari, Edge).
// Only these: a subscription names the address Yoru posts to, and a paired device could name any.
var pushHosts = []string{".googleapis.com", ".push.services.mozilla.com", ".push.apple.com", ".notify.windows.com"}

func pushEndpointOK(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" {
		return false
	}
	host := "." + strings.ToLower(u.Hostname())
	for _, suffix := range pushHosts {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

type pushBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

func (s *Server) pushKey(w http.ResponseWriter, r *http.Request) {
	if s.push == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "alerts are not available"})
		return
	}
	send(w, http.StatusOK, map[string]any{"publicKey": s.push.PublicKey()})
}

// pushSubscribe stores this device's subscription, with the address it used, where a tapped alert opens.
func (s *Server) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	var body pushBody
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body) != nil || !pushEndpointOK(body.Endpoint) {
		send(w, http.StatusBadRequest, map[string]any{"error": "not a browser push subscription"})
		return
	}
	p256dh, perr := base64.RawURLEncoding.DecodeString(strings.TrimRight(body.Keys.P256dh, "="))
	auth, aerr := base64.RawURLEncoding.DecodeString(strings.TrimRight(body.Keys.Auth, "="))
	if perr != nil || aerr != nil || len(p256dh) != 65 || len(auth) != 16 {
		send(w, http.StatusBadRequest, map[string]any{"error": "the subscription's keys are malformed"})
		return
	}
	sub := store.PushSubscription{
		Endpoint: body.Endpoint, Origin: requestOrigin(r),
		P256dh: base64.RawURLEncoding.EncodeToString(p256dh), Auth: base64.RawURLEncoding.EncodeToString(auth),
		Paired: !fromHost(r),
	}
	// Who asked, so declining or removing the device stops its alerts too.
	sub.Device = requestDevice(r)
	if err := s.store.SavePushSubscription(r.Context(), sub); err != nil {
		s.fail(w, "save push subscription", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"subscribed": true})
}

func (s *Server) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var body pushBody
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body) != nil || body.Endpoint == "" {
		send(w, http.StatusBadRequest, map[string]any{"error": "endpoint is required"})
		return
	}
	if err := s.store.DeletePushSubscription(r.Context(), body.Endpoint); err != nil {
		s.fail(w, "remove push subscription", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"subscribed": false})
}

// pushTest sends one alert to the asking device only, so "it works" is checked where it matters.
func (s *Server) pushTest(w http.ResponseWriter, r *http.Request) {
	var body pushBody
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body) != nil || s.push == nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "endpoint is required"})
		return
	}
	subs, err := s.store.PushSubscriptions(r.Context())
	if err != nil {
		s.fail(w, "push subscriptions", err)
		return
	}
	for _, sub := range subs {
		if sub.Endpoint != body.Endpoint {
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := s.push.SendTo(ctx, sub, push.Alert{
			Title: "Yoru", Body: "Alerts reach this device, even with Yoru closed.", Tag: "yoru-test", Path: "/",
		}); err != nil {
			send(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		send(w, http.StatusOK, map[string]any{"sent": true})
		return
	}
	send(w, http.StatusNotFound, map[string]any{"error": "this device is not subscribed"})
}

// requestOrigin is the address the request came in on, as the browser sees it.
func requestOrigin(r *http.Request) string {
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// ReleaseAlert is the push for a notification, or false for one that belongs only to the open app.
func ReleaseAlert(ctx context.Context, st *store.Store, n store.Notification) (push.Alert, bool) {
	if n.AnimeID == nil {
		return push.Alert{}, false
	}
	switch n.Kind {
	case store.NotifyRelease, store.NotifyAiring:
	case store.NotifyDelay, store.NotifySeason:
		// About the show, not an episode to play: they open its page, and their title names it.
		unread, _ := st.UnreadCount(ctx)
		a := push.Alert{
			Title: n.Title, Body: n.Body, Tag: fmt.Sprintf("yoru-%d", n.ID),
			Path: fmt.Sprintf("/anime/%d", *n.AnimeID), Image: st.BannerOf(ctx, *n.AnimeID), Badge: unread,
		}
		if n.Kind == store.NotifySeason {
			a.Actions, a.AnimeID = []string{"follow", "plan"}, *n.AnimeID
		}
		return a, true
	default:
		return push.Alert{}, false
	}
	episode := 1
	if n.Episode != nil && *n.Episode > 0 {
		episode = *n.Episode
	}
	title := st.ShownTitle(ctx, *n.AnimeID)
	if title == "" {
		title = "Yoru"
	}
	unread, _ := st.UnreadCount(ctx)
	// The tag and page an open tab's own alert uses, so the two show as one.
	a := push.Alert{
		Title: title, Body: n.Title, Tag: fmt.Sprintf("yoru-%d", n.ID),
		Path:  fmt.Sprintf("/watch/%d/%d", *n.AnimeID, episode),
		Image: st.BannerOf(ctx, *n.AnimeID), Badge: unread,
	}
	// A release can be downloaded or lined up from the alert itself; an airing one has nothing to fetch yet.
	if n.Kind == store.NotifyRelease {
		a.Actions, a.AnimeID, a.Episode = []string{"download", "upnext"}, *n.AnimeID, episode
	}
	return a, true
}
