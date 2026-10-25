package jimaku

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Searched by AniList id with the key, an episode's files listed without archives, fetched from Jimaku only.
func TestFilesAndFetch(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "k3y" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/entries/search":
			if r.URL.Query().Get("anilist_id") != "154587" {
				t.Errorf("search %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode([]map[string]any{{"id": 9}})
		case "/api/entries/9/files":
			if r.URL.Query().Get("episode") != "3" {
				t.Errorf("files %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode([]map[string]any{
				{"name": "Show - 03.ja.ass", "url": srv.URL + "/entry/9/download/03.ass", "size": 10},
				{"name": "Show S1.zip", "url": srv.URL + "/entry/9/download/s1.zip", "size": 99},
			})
		case "/entry/9/download/03.ass":
			w.Write([]byte("[Script Info]"))
		}
	}))
	defer srv.Close()
	c := &Client{http: srv.Client(), base: srv.URL}

	files, err := c.Files(context.Background(), "k3y", 154587, 3)
	if err != nil || len(files) != 1 || files[0].Name != "Show - 03.ja.ass" {
		t.Fatalf("files = %+v, %v; want the one .ass", files, err)
	}
	body, err := c.Fetch(context.Background(), "k3y", files[0].URL)
	if err != nil || string(body) != "[Script Info]" {
		t.Errorf("fetch = %q, %v", body, err)
	}
	if _, err := c.Fetch(context.Background(), "k3y", "https://elsewhere.example/x.ass"); err == nil {
		t.Error("a file from elsewhere was fetched")
	}
	if err := c.Check(context.Background(), "wrong"); !errors.Is(err, ErrKey) {
		t.Errorf("a wrong key: %v, want ErrKey", err)
	}
}
