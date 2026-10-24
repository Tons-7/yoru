package library

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/metadata"
	"yoru/internal/parse"
	"yoru/internal/score"
)

// TRaSH's lists are mirrored whole and graded into the ranking; a list that fails to load keeps the last one.
func TestGroupTiersMirrorIntoTheRanking(t *testing.T) {
	failing := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing && strings.Contains(r.URL.Path, "lq") {
			http.Error(w, "gone", http.StatusBadGateway)
			return
		}
		group := map[string]string{"/anime-web-tier-01.json": "TopGroup", "/anime-lq-groups.json": "MiniGroup"}[r.URL.Path]
		if group == "" {
			fmt.Fprint(w, `{"specifications":[{"name":"WEB","implementation":"SourceSpecification","fields":{"value":1}}]}`)
			return
		}
		fmt.Fprintf(w, `{"specifications":[{"name":%q,"implementation":"ReleaseTitleSpecification","fields":{"value":"\\b(%s)\\b"}}]}`, group, group)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { score.SetGroupTiers(nil) })

	meta := metadata.New()
	meta.SetURL("trash", srv.URL+"/")
	e := NewEnricher(prefetchStore(t), meta, slog.New(slog.DiscardHandler))
	ctx := context.Background()
	if n, err := e.GroupTiers(ctx, true); err != nil || n != 2 {
		t.Fatalf("mirrored %d, %v; want the two groups", n, err)
	}
	top := score.Candidate{Torrent: indexer.Torrent{Title: "[TopGroup] Show - 01 (WEB 1080p).mkv", Seeders: 9, SeedersKnown: true, Size: 1 << 30},
		Release: parse.Parse("[TopGroup] Show - 01 (WEB 1080p).mkv")}
	if r := score.Rank([]score.Candidate{top}, score.DefaultPreferences())[0]; !slices.Contains(r.Reasons, "WEB tier 1 group on TRaSH Guides") {
		t.Fatalf("reasons %v; the tier wasn't applied", r.Reasons)
	}

	failing = true
	if _, err := e.GroupTiers(ctx, true); err == nil {
		t.Fatal("a failed list read as a success")
	}
	if r := score.Rank([]score.Candidate{top}, score.DefaultPreferences())[0]; !slices.Contains(r.Reasons, "WEB tier 1 group on TRaSH Guides") {
		t.Fatal("a failed refresh dropped the grades already saved")
	}
}
