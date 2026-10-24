package library

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"yoru/internal/metadata"
	"yoru/internal/player"
)

// Off, nothing goes to AniSkip; on, a time it lacks goes once with an anonymous id, and a vote once per time.
func TestSkipTimesAreSharedOnceAndOnlyWhenOn(t *testing.T) {
	var mu sync.Mutex
	var got []map[string]any
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		got, paths = append(got, body), append(paths, r.URL.Path)
		mu.Unlock()
		w.Write([]byte(`{"message":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	meta := metadata.New()
	meta.SetURL("aniskip-base", srv.URL+"/v2/skip-times")
	st := skipStore(t)
	e := NewEnricher(st, meta, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	op := player.SkipRange{Kind: "op", Start: 87, End: 176.9, Source: "chapters"}

	if err := e.ShareSkip(ctx, 7, 3, op, 1420); err == nil || len(got) != 0 {
		t.Fatalf("shared with sharing off: %v, %v", err, got)
	}
	st.SetSetting(ctx, "skips.share", "true")
	for range 2 {
		if err := e.ShareSkip(ctx, 7, 3, op, 1420); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := e.VoteSkip(ctx, "a1b2", false); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 {
		t.Fatalf("sent %d, want one submission and one vote: %v", len(got), paths)
	}
	sub := got[0]
	if paths[0] != "/v2/skip-times/52991/3" || sub["skipType"] != "op" || sub["startTime"] != 87.0 ||
		sub["episodeLength"] != 1420.0 || sub["providerName"] != "Yoru" || len(sub["submitterId"].(string)) != 36 {
		t.Errorf("submission %s %v", paths[0], sub)
	}
	if paths[1] != "/v2/skip-times/vote/a1b2" || got[1]["voteType"] != "downvote" {
		t.Errorf("vote %s %v", paths[1], got[1])
	}
	// The same anonymous id each time.
	if id, _ := st.Setting(ctx, "skips.submitter"); id != sub["submitterId"] {
		t.Errorf("submitter %q, sent %v", id, sub["submitterId"])
	}
}
