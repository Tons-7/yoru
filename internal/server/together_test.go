package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"yoru/internal/config"
)

// A room as createTogether makes it, without the HTTPS a real one needs.
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

func TestWatchTogetherNeedsHTTPS(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	code, body := h.call(t, "POST", "/api/together", `{"animeId":7,"episode":1,"infoHash":"h"}`)
	if code != http.StatusConflict || body["needs"] != "https" {
		t.Fatalf("no HTTPS: %d %v, want it to say what's missing", code, body)
	}
}

// Two left blank, or the same name twice: numbered, so the feed tells them apart.
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

// The room's rules: asked before someone joins when it says so; a play held until everyone's ready; a slow
// connection holds it; pause, chat and reactions reach everyone; a stranger's key or token gets nothing.
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

// Only the host starts without everyone; the host leaving hands the room on; seats left a while are cleared,
// so a room isn't full of people who closed their tab.
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
