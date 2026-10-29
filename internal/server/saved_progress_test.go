package server

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"yoru/internal/config"
)

// One late report of a whole episode counts; a place older than Yoru's own is ignored.
func TestSavedProgressCountsWhatWasWatchedAway(t *testing.T) {
	h := watchHarness(t)
	now := time.Now().UnixMilli()
	code, body := h.call(t, "POST", "/api/saved/progress",
		fmt.Sprintf(`{"items":[{"animeId":1,"episode":1,"position":1390,"duration":1400,"played":1380,"at":%d}]}`, now))
	if code != http.StatusOK || len(body["watched"].([]any)) != 1 {
		t.Fatalf("a whole episode watched away: %d %v", code, body)
	}

	report2 := `{"animeId":1,"episode":2,"position":600,"duration":1400,"played":100}`
	if code, _ := h.call(t, "POST", "/api/progress", report2); code != http.StatusOK {
		t.Fatal(code)
	}
	old := time.Now().Add(-time.Hour).UnixMilli()
	h.call(t, "POST", "/api/saved/progress",
		fmt.Sprintf(`{"items":[{"animeId":1,"episode":2,"position":30,"duration":1400,"played":30,"at":%d}]}`, old))
	if at, err := h.store.ResumeAt(context.Background(), 1, "2"); err != nil || at != 600 {
		t.Errorf("episode 2 resumes at %v (%v), want 600 kept", at, err)
	}
}

func TestSavedReadyWaitsForAWholeFile(t *testing.T) {
	h := watchHarness(t)
	if code, body := h.call(t, "GET", "/api/saved/ready?anime=1&episode=3", ""); code != http.StatusOK ||
		body["ready"] != false || body["downloading"] != false {
		t.Errorf("nothing on the PC: %d %v", code, body)
	}
}

func TestAppFilesListTheBuiltApp(t *testing.T) {
	h := newHarness(t, config.Config{}, nil)
	code, body := h.call(t, "GET", "/api/app/files", "")
	if code != http.StatusOK {
		t.Fatalf("answered %d %v", code, body)
	}
	var files []string
	for _, f := range body["files"].([]any) {
		files = append(files, f.(string))
	}
	if body["version"] == "" || !slices.Contains(files, "/") || slices.Contains(files, "/sw.js") || slices.Contains(files, "/index.html") ||
		!slices.ContainsFunc(files, func(f string) bool { return strings.HasPrefix(f, "/assets/") }) {
		t.Errorf("files %v version %v", files, body["version"])
	}
}
