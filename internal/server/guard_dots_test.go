package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDotPathsNeverPassTheRoomException(t *testing.T) {
	s := &Server{}
	reached := false
	h := s.guard(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	for _, target := range []string{
		"/api/together/../library",
		"/api/together/x/../../library",
		"/together/x/../../api/library",
		"/together/%2e%2e/api/library",
		"/api/together/%2E%2E/library",
	} {
		reached = false
		req := httptest.NewRequest(http.MethodGet, "http://yoru.test/", nil)
		parsed, err := url.ParseRequestURI(target)
		if err != nil {
			t.Fatal(err)
		}
		req.URL = parsed
		req.RemoteAddr = "192.168.1.50:5555"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if reached {
			t.Errorf("%s reached the app without a token", target)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", target, rec.Code)
		}
	}
}
