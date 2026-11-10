package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"yoru/internal/config"
)

func openRoom(t *testing.T, h *harness) (*togetherRoom, *togetherMember) {
	t.Helper()
	room := &togetherRoom{code: "abc123", secret: "key", created: time.Now(), wait: true,
		state: togetherState{AnimeID: 7, Episode: 1, InfoHash: "hash", At: nowMillis()}}
	host := &togetherMember{ID: "host", Name: "Tons", Host: true, Admitted: true, Connected: true, token: "ht",
		events: make(chan []byte, 256)}
	room.members = []*togetherMember{host}
	h.server.rooms[room.code] = room
	return room, host
}

func act(t *testing.T, h *harness, member, token, body string) (int, map[string]any) {
	t.Helper()
	return h.call(t, "POST", "/api/together/abc123/act?k=key",
		fmt.Sprintf(`{"member":%q,"token":%q,%s}`, member, token, body))
}

func TestWatchTogetherNeedsTheTunnel(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	code, body := h.call(t, "POST", "/api/together", `{"animeId":7,"episode":1,"infoHash":"h"}`)
	if code != http.StatusConflict || body["needs"] != "cloudflared" {
		t.Fatalf("no tunnel: %d %v, want it to say what's missing", code, body)
	}
}

func TestRoomPortServesOnlyTheRoom(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	openRoom(t, h)
	base, err := h.server.roomAddress()
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{
		"/api/together/abc123?k=key": http.StatusOK,
		"/together/abc123?k=key":     http.StatusOK,
		"/api/library":               http.StatusNotFound,
		"/api/settings":              http.StatusNotFound,
		"/":                          http.StatusNotFound,
	} {
		res, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("%s answered %d, want %d", path, res.StatusCode, want)
		}
	}

	ws, err := websocket.Dial(strings.Replace(base, "http", "ws", 1)+"/api/together/abc123/socket?k=key&m=host&t=ht", "",
		"https://friend.example")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.SetDeadline(time.Now().Add(5 * time.Second))
	var msg string
	if err := websocket.Message.Receive(ws, &msg); err != nil || !strings.Contains(msg, `"type":"room"`) {
		t.Fatalf("first message %q, %v; want the room", msg, err)
	}

	if _, err := websocket.Dial(strings.Replace(base, "http", "ws", 1)+"/api/together/abc123/socket?k=wrong&m=host&t=ht", "",
		"https://friend.example"); err == nil {
		t.Fatal("a wrong key opened the room's socket")
	}
}

func TestRoomSocketThroughTheMainPort(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	openRoom(t, h)
	srv := httptest.NewServer(h.handler)
	t.Cleanup(srv.Close)
	ws, err := websocket.Dial(strings.Replace(srv.URL, "http", "ws", 1)+"/api/together/abc123/socket?k=key&m=host&t=ht", "",
		srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.SetDeadline(time.Now().Add(5 * time.Second))
	var msg string
	if err := websocket.Message.Receive(ws, &msg); err != nil || !strings.Contains(msg, `"type":"room"`) {
		t.Fatalf("first message %q, %v; want the room", msg, err)
	}
}

func TestWatchTogetherNamesAreUnique(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	room, _ := openRoom(t, h)
	for _, name := range []string{"", " ", "tons", "Tons"} {
		if code, _ := h.call(t, "POST", "/api/together/abc123/join?k=key", fmt.Sprintf(`{"name":%q}`, name)); code != http.StatusOK {
			t.Fatalf("join %q answered %d", name, code)
		}
	}
	var got []string
	for _, m := range room.members[1:] {
		got = append(got, m.Name)
	}
	if want := "Friend,Friend 2,tons 2,Tons 3"; strings.Join(got, ",") != want {
		t.Errorf("names %v, want %s", got, want)
	}
}

func TestWatchTogetherPicksAndAudio(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	room, _ := openRoom(t, h)
	room.title = "First Show"
	if code, _ := h.call(t, "POST", "/api/together/abc123/join?k=key", `{"name":"Alex"}`); code != http.StatusOK {
		t.Fatalf("join answered %d", code)
	}
	alex := room.members[1]

	change := `"type":"episode","animeId":9,"episode":3,"infoHash":"NEW","title":"Second Show","audio":"dub"`
	if code, _ := act(t, h, alex.ID, alex.token, change); code != http.StatusForbidden || room.state.AnimeID != 7 {
		t.Fatalf("a friend changed the room's show: %d, state %+v", code, room.state)
	}
	if code, _ := act(t, h, alex.ID, alex.token, `"type":"audio","audio":"dub"`); code != http.StatusForbidden {
		t.Errorf("a friend changed the room's audio: %d", code)
	}

	if code, _ := act(t, h, alex.ID, alex.token, `"type":"suggest","animeId":7,"episode":1`); code != http.StatusConflict {
		t.Errorf("suggesting what's playing answered %d", code)
	}
	act(t, h, alex.ID, alex.token, `"type":"suggest","animeId":8,"episode":1,"title":"Dropped Idea"`)
	if code, _ := act(t, h, alex.ID, alex.token, `"type":"suggest","animeId":9,"episode":3,"title":"Second Show"`); code != http.StatusOK {
		t.Fatalf("suggest answered %d", code)
	}
	if len(room.picks) != 1 || room.picks[0].Title != "Second Show" || room.picks[0].By != "Alex" {
		t.Fatalf("picks = %+v, want Alex's latest only", room.picks)
	}

	if code, _ := act(t, h, "host", "ht", change); code != http.StatusOK {
		t.Fatalf("the host's change answered %d", code)
	}
	st := room.state
	if st.AnimeID != 9 || st.Episode != 3 || st.InfoHash != "new" || st.Audio != "dub" || room.title != "Second Show" {
		t.Fatalf("state = %+v, title %q", st, room.title)
	}
	if len(room.picks) != 0 {
		t.Errorf("the played suggestion is still listed: %+v", room.picks)
	}

	act(t, h, "host", "ht", `"type":"audio","audio":"sub"`)
	if room.state.Audio != "sub" {
		t.Errorf("audio = %q after the host picked sub", room.state.Audio)
	}

	act(t, h, alex.ID, alex.token, `"type":"suggest","animeId":10,"episode":1,"title":"Third"`)
	pick := room.picks[0].ID
	if act(t, h, "host", "ht", fmt.Sprintf(`"type":"unsuggest","pick":%d`, pick)); len(room.picks) != 0 {
		t.Errorf("the host couldn't dismiss a suggestion: %+v", room.picks)
	}

	act(t, h, "host", "ht", `"type":"settings","anyone":true`)
	if code, _ := act(t, h, alex.ID, alex.token, `"type":"episode","animeId":9,"episode":4,"infoHash":"next"`); code != http.StatusOK ||
		room.state.Episode != 4 || room.state.Audio != "sub" {
		t.Fatalf("with anyone allowed, a friend's change: %d, state %+v", code, room.state)
	}
}

func TestSomeoneBrowsingDoesNotHoldTheRoom(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	room, _ := openRoom(t, h)
	if code, _ := h.call(t, "POST", "/api/together/abc123/join?k=key", `{"name":"Alex"}`); code != http.StatusOK {
		t.Fatalf("join answered %d", code)
	}
	alex := room.members[1]
	alex.Connected = true

	act(t, h, "host", "ht", `"type":"ready","ready":true`)
	act(t, h, "host", "ht", `"type":"play"`)
	if !room.state.Waiting {
		t.Fatalf("state = %+v, want the play held for Alex", room.state)
	}
	act(t, h, alex.ID, alex.token, `"type":"browsing","browsing":true`)
	if !room.state.Playing || room.state.Waiting || !alex.Browsing {
		t.Fatalf("state = %+v, want playing once Alex is only browsing", room.state)
	}
	if names := room.notReady(); len(names) != 0 {
		t.Errorf("still waited for %v", names)
	}

	act(t, h, "host", "ht", `"type":"pause","position":5`)
	act(t, h, alex.ID, alex.token, `"type":"browsing","browsing":false`)
	act(t, h, "host", "ht", `"type":"play"`)
	if !room.state.Waiting {
		t.Fatalf("state = %+v, want Alex waited for again once back at the player", room.state)
	}
}

func TestAWatchTogetherRoom(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	room, host := openRoom(t, h)
	room.ask = true

	if code, _ := h.call(t, "GET", "/api/together/abc123?k=wrong", ""); code != http.StatusNotFound {
		t.Errorf("a wrong key answered %d", code)
	}
	code, joined := h.call(t, "POST", "/api/together/abc123/join?k=key", `{"name":"Alex"}`)
	if code != http.StatusOK || joined["admitted"] != false {
		t.Fatalf("join with asking on: %d %v", code, joined)
	}
	if knock := <-host.events; !strings.Contains(string(knock), `"knock"`) || !strings.Contains(string(knock), "Alex") {
		t.Errorf("the host wasn't asked: %s", knock)
	}
	alex := room.members[1]
	if code, _ := act(t, h, alex.ID, alex.token, `"type":"play"`); code != http.StatusForbidden {
		t.Errorf("someone not let in yet acted: %d", code)
	}
	if code, _ := act(t, h, "host", "ht", fmt.Sprintf(`"type":"admit","admit":%q,"allow":true`, alex.ID)); code != http.StatusOK || !alex.Admitted {
		t.Fatalf("admit: %d", code)
	}
	alex.Connected = true

	act(t, h, "host", "ht", `"type":"play"`)
	if !room.state.Waiting || room.state.Playing {
		t.Fatalf("state = %+v, want the play held for Alex", room.state)
	}
	act(t, h, "host", "ht", `"type":"ready","ready":true`)
	act(t, h, alex.ID, alex.token, `"type":"ready","ready":true`)
	if !room.state.Playing || room.state.Waiting {
		t.Fatalf("state = %+v, want playing once both are ready", room.state)
	}

	act(t, h, alex.ID, alex.token, `"type":"ready","ready":false`)
	if room.state.Playing || !room.state.Waiting {
		t.Errorf("state = %+v, want the room waiting for Alex's connection", room.state)
	}
	act(t, h, alex.ID, alex.token, `"type":"ready","ready":true`)
	act(t, h, alex.ID, alex.token, `"type":"pause","position":42`)
	if room.state.Playing || room.state.Position != 42 {
		t.Errorf("state = %+v, want paused at 42", room.state)
	}

	for len(host.events) > 0 {
		<-host.events
	}
	act(t, h, alex.ID, alex.token, `"type":"chat","text":"this opening"`)
	if msg := <-host.events; !strings.Contains(string(msg), "this opening") || !strings.Contains(string(msg), "Alex") {
		t.Errorf("chat reached the host as %s", msg)
	}
	if code, _ := act(t, h, alex.ID, alex.token, `"type":"react","text":"🍕"`); code != http.StatusBadRequest {
		t.Errorf("a reaction not on the list answered %d", code)
	}
	if code, _ := act(t, h, alex.ID, alex.token, `"type":"settings","wait":false`); code != http.StatusForbidden {
		t.Errorf("a guest changed the room's settings: %d", code)
	}
	if code, _ := act(t, h, alex.ID, "stolen", `"type":"pause"`); code != http.StatusForbidden {
		t.Errorf("a wrong token acted: %d", code)
	}
}

func TestAWatchTogetherRoomKeepsAHostAndRoom(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	room, _ := openRoom(t, h)
	h.call(t, "POST", "/api/together/abc123/join?k=key", `{"name":"Alex"}`)
	alex := room.members[1]
	if code, _ := act(t, h, alex.ID, alex.token, `"type":"start"`); code != http.StatusForbidden {
		t.Errorf("a guest started the room past waiting: %d", code)
	}
	if code, _ := act(t, h, "host", "ht", `"type":"leave"`); code != http.StatusOK || !alex.Host {
		t.Fatalf("the host left: %d, Alex host %v, want the room handed on", code, alex.Host)
	}

	for range togetherMax - 1 {
		h.call(t, "POST", "/api/together/abc123/join?k=key", `{"name":"Tab"}`)
	}
	for _, m := range room.members[1:] {
		m.gone = time.Now().Add(-3 * time.Minute)
	}
	if code, _ := h.call(t, "POST", "/api/together/abc123/join?k=key", `{"name":"Sam"}`); code != http.StatusOK {
		t.Errorf("seats left 3 minutes still filled the room: %d", code)
	}
}

func TestAWatchTogetherRoomFills(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	openRoom(t, h)
	for i := 1; i < togetherMax; i++ {
		if code, _ := h.call(t, "POST", "/api/together/abc123/join?k=key", `{"name":"Friend"}`); code != http.StatusOK {
			t.Fatalf("join %d: %d", i, code)
		}
	}
	if code, _ := h.call(t, "POST", "/api/together/abc123/join?k=key", `{"name":"One more"}`); code != http.StatusConflict {
		t.Errorf("a full room let one more in: %d", code)
	}
}

func lastView(t *testing.T, m *togetherMember) map[string]bool {
	t.Helper()
	var view map[string]any
	for len(m.events) > 0 {
		var event map[string]any
		if err := json.Unmarshal(<-m.events, &event); err == nil && event["type"] == "room" {
			view = event
		}
	}
	if view == nil {
		t.Fatalf("no member list was sent")
	}
	ready := map[string]bool{}
	for _, raw := range view["members"].([]any) {
		member := raw.(map[string]any)
		ready[member["name"].(string)] = member["ready"].(bool)
	}
	return ready
}

func TestEveryStatusChangeReachesTheRoom(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	room, host := openRoom(t, h)
	if code, _ := h.call(t, "POST", "/api/together/abc123/join?k=key", `{"name":"Alex"}`); code != http.StatusOK {
		t.Fatalf("join answered %d", code)
	}
	alex := room.members[1]
	alex.Connected = true
	act(t, h, "host", "ht", `"type":"ready","ready":true`)
	act(t, h, alex.ID, alex.token, `"type":"ready","ready":true`)
	act(t, h, "host", "ht", `"type":"play"`)
	if !room.state.Playing {
		t.Fatalf("state = %+v, want playing with everyone ready", room.state)
	}
	lastView(t, host)

	act(t, h, alex.ID, alex.token, `"type":"ready","ready":false`)
	if got := lastView(t, host); got["Alex"] || !room.state.Waiting {
		t.Fatalf("Alex buffering: the room was told %v, state %+v", got, room.state)
	}
	act(t, h, alex.ID, alex.token, `"type":"ready","ready":true`)
	if got := lastView(t, host); !got["Alex"] || !room.state.Playing {
		t.Fatalf("Alex ready again: the room was told %v, state %+v", got, room.state)
	}
	act(t, h, "host", "ht", `"type":"pause","position":40`)
	act(t, h, "host", "ht", `"type":"play"`)
	for len(host.events) > 0 {
		if raw := <-host.events; strings.Contains(string(raw), `"type":"room"`) {
			t.Fatalf("a pause and a play with nobody's status changed resent the whole member list: %s", raw)
		}
	}
	act(t, h, "host", "ht", `"type":"seek","position":90`)
	if got := lastView(t, host); got["Alex"] || got["Tons"] {
		t.Fatalf("after a jump the room was told %v, want everyone loading", got)
	}
	act(t, h, "host", "ht", `"type":"audio","audio":"dub"`)
	act(t, h, "host", "ht", `"type":"ready","ready":true`)
	act(t, h, "host", "ht", `"type":"audio","audio":"sub"`)
	if got := lastView(t, host); got["Tons"] {
		t.Fatalf("after an audio switch the room was told %v, want everyone loading", got)
	}
	act(t, h, "host", "ht", `"type":"episode","animeId":7,"episode":2,"infoHash":"two"`)
	lastView(t, host)
	act(t, h, "host", "ht", `"type":"episode","animeId":7,"episode":3,"infoHash":"three"`)
	lastView(t, host)
}

func TestASilentSocketIsMarkedAway(t *testing.T) {
	tick, silence := roomTick, roomSilence
	roomTick, roomSilence = 20*time.Millisecond, 150*time.Millisecond
	t.Cleanup(func() { roomTick, roomSilence = tick, silence })
	h := newHarness(t, config.Config{}, nil)
	room, host := openRoom(t, h)
	host.Connected = false
	srv := httptest.NewServer(h.handler)
	t.Cleanup(srv.Close)
	dial := func() *websocket.Conn {
		ws, err := websocket.Dial(strings.Replace(srv.URL, "http", "ws", 1)+"/api/together/abc123/socket?k=key&m=host&t=ht", "", srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { ws.Close() })
		return ws
	}
	connected := func() bool {
		room.mu.Lock()
		defer room.mu.Unlock()
		return host.Connected
	}
	settle := func(want bool, within time.Duration) bool {
		for end := time.Now().Add(within); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if connected() == want {
				return true
			}
		}
		return false
	}

	old := dial()
	if !settle(true, time.Second) {
		t.Fatal("the socket never counted as connected")
	}
	time.Sleep(3 * roomSilence)
	if !connected() {
		t.Fatal("a socket that never answers was dropped; older apps don't answer ticks and must stay")
	}
	old.Close()
	if !settle(false, time.Second) {
		t.Fatal("a closed socket still counts as connected")
	}

	ws := dial()
	if err := websocket.Message.Send(ws, "1"); err != nil {
		t.Fatal(err)
	}
	if !settle(true, time.Second) {
		t.Fatal("the second socket never counted as connected")
	}
	if !settle(false, 10*roomSilence) {
		t.Fatal("a socket that answered once and then went silent still counts as connected")
	}
}
