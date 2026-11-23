package server

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/config"
)

func TestAnOpenSeriesPageMovesOnAfterAnEpisodeAirs(t *testing.T) {
	var flipped atomic.Bool
	var asked atomic.Int32
	nextAt := time.Now().Add(7 * 24 * time.Hour).Unix()
	h := newHarness(t, config.Config{}, func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		episode, at := 9, time.Now().Add(-time.Minute).Unix()
		if flipped.Load() {
			episode, at = 10, nextAt
		}
		fmt.Fprintf(w, `{"data":{"Page":{"media":[{"id":6,"status":"RELEASING","title":{"romaji":"Show"},
			"nextAiringEpisode":{"episode":%d,"airingAt":%d,"timeUntilAiring":1},"studios":{"nodes":[]}}]}}}`, episode, at)
	})
	ctx := context.Background()
	status, title := "RELEASING", "Show"
	aired := int(time.Now().Add(-time.Minute).Unix())
	if _, err := h.server.importer.Save(ctx, []anilist.Media{{ID: 6, Status: &status, Title: anilist.Title{Romaji: &title},
		NextAiring: &anilist.Airing{Episode: 9, AiringAt: aired}}}); err != nil {
		t.Fatal(err)
	}

	flipped.Store(true)
	res, body := h.do(t, "GET", "/api/episodes?id=6")
	if res.StatusCode != http.StatusOK || body["airingDue"] != true {
		t.Fatalf("right after airing: %d %v", res.StatusCode, body["airingDue"])
	}
	if res.Header.Get(headerRefreshing) == "" {
		t.Error("the page was not told a refresh is running")
	}

	deadline := time.Now().Add(5 * time.Second)
	for h.store.AiringDue(ctx, 6) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if h.store.AiringDue(ctx, 6) {
		t.Fatalf("the show never moved on after %d asks", asked.Load())
	}
	_, body = h.do(t, "GET", "/api/episodes?id=6")
	if body["airingDue"] != false {
		t.Errorf("still due: %v", body["airingDue"])
	}
	items, _ := body["items"].([]any)
	found := false
	for _, item := range items {
		row := item.(map[string]any)
		if row["number"] == float64(10) && row["airDate"] == float64(nextAt) {
			found = true
		}
	}
	if !found {
		t.Errorf("episode 10 is not listed with its air time: %v", items)
	}
}
