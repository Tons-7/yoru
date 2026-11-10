package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"yoru/internal/config"
)

const tinyPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func fetchPicture(h *harness, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Host = "127.0.0.1:4321"
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func TestProfileAvatar(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)

	if _, body := h.call(t, "GET", "/api/profile", ""); body["avatar"] != "" {
		t.Fatalf("a fresh install has a picture: %v", body)
	}
	if rec := fetchPicture(h, "/api/profile/avatar"); rec.Code != http.StatusNotFound {
		t.Fatalf("no picture answered %d", rec.Code)
	}

	for name, image := range map[string]string{
		"a page":      "data:text/html;base64,PGh0bWw+",
		"a lie":       "data:image/png;base64,PGh0bWw+PHNjcmlwdD4=",
		"too big":     "data:image/png;base64," + strings.Repeat("A", 60000),
		"not encoded": "data:image/png;base64,***",
	} {
		if code, _ := h.call(t, "PUT", "/api/profile/avatar", fmt.Sprintf(`{"image":%q}`, image)); code != http.StatusBadRequest {
			t.Errorf("%s was taken as a picture: %d", name, code)
		}
	}

	code, body := h.call(t, "PUT", "/api/profile/avatar", fmt.Sprintf(`{"image":%q}`, tinyPNG))
	if code != http.StatusOK || body["avatar"] != "own" || body["version"] == "" {
		t.Fatalf("upload: %d %v", code, body)
	}
	rec := fetchPicture(h, "/api/profile/avatar")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" || rec.Body.Len() == 0 {
		t.Fatalf("the picture answered %d as %q", rec.Code, rec.Header().Get("Content-Type"))
	}

	if code, body := h.call(t, "DELETE", "/api/profile/avatar", ""); code != http.StatusOK || body["avatar"] != "" {
		t.Fatalf("remove: %d %v", code, body)
	}
}

func TestProfileName(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	if _, body := h.call(t, "GET", "/api/profile", ""); body["name"] != "" {
		t.Fatalf("a fresh install has a name: %v", body)
	}
	long := strings.Repeat("n", 80)
	code, body := h.call(t, "PUT", "/api/profile/name", fmt.Sprintf(`{"name":"  Tons\n %s"}`, long))
	if code != http.StatusOK || len([]rune(body["name"].(string))) != 32 || !strings.HasPrefix(body["name"].(string), "Tons ") {
		t.Fatalf("name: %d %v; want it trimmed, on one line, at most 32 letters", code, body)
	}
	if code, body := h.call(t, "PUT", "/api/profile/name", `{"name":"   "}`); code != http.StatusOK || body["name"] != "" {
		t.Fatalf("clearing the name: %d %v", code, body)
	}
}

func TestRoomMembersCarryTheirPicture(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	room, _ := openRoom(t, h)
	if code, _ := h.call(t, "POST", "/api/together/abc123/join?k=key", fmt.Sprintf(`{"name":"Alex","avatar":%q}`, tinyPNG)); code != http.StatusOK {
		t.Fatalf("join answered %d", code)
	}
	if code, _ := h.call(t, "POST", "/api/together/abc123/join?k=key", `{"name":"Sam","avatar":"data:text/html;base64,PGh0bWw+"}`); code != http.StatusOK {
		t.Fatalf("a join with a bad picture answered %d; the picture alone should be dropped", code)
	}
	alex, sam := room.members[1], room.members[2]
	if alex.Avatar == "" || sam.Avatar != "" {
		t.Fatalf("avatars: alex %q, sam %q", alex.Avatar, sam.Avatar)
	}
	rec := fetchPicture(h, "/api/together/abc123/avatar/"+alex.ID+"?k=key")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("a member's picture answered %d as %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := fetchPicture(h, "/api/together/abc123/avatar/"+alex.ID+"?k=wrong"); rec.Code != http.StatusNotFound {
		t.Errorf("a wrong key got a member's picture: %d", rec.Code)
	}
	if rec := fetchPicture(h, "/api/together/abc123/avatar/"+sam.ID+"?k=key"); rec.Code != http.StatusNotFound {
		t.Errorf("a member without a picture answered %d", rec.Code)
	}
}
