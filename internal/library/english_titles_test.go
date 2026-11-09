package library

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"yoru/internal/metadata"
	"yoru/internal/store"
)

func TestEnglishTitlesFillAnAiringShowAndStopPending(t *testing.T) {
	st := openStore(t)
	ctx := context.Background()
	a := entryOf(1, "TV", "Seitokai ni mo Ana wa Aru", "", "2026-10-04", 3, 24)
	malID, status := 61578, "RELEASING"
	a.MalID, a.Status = &malID, &status
	if _, err := st.ImportList(ctx, []store.Anime{a}, nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}

	aired := time.Now().Add(-48 * time.Hour).UTC()
	if _, err := st.SaveEpisodes(ctx, 1, []metadata.Episode{
		{Number: 1, TitleJA: "かいちょーは梅くんに貰われました", AirDate: aired.Unix()},
		{Number: 2, AirDate: aired.Add(7 * 24 * time.Hour).Unix()},
	}); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"data":[{"mal_id":1,"title":"Ume Takes Responsibility","aired":"%sT00:00:00+00:00"}],
			"pagination":{"has_next_page":false}}`, aired.Format("2006-01-02"))
	}))
	t.Cleanup(srv.Close)
	meta := metadata.New()
	meta.SetURL("tenrai", srv.URL)
	e := NewEnricher(st, meta, slog.New(slog.DiscardHandler))

	if e.TitlesPending(ctx, 1) {
		t.Fatal("pending with nothing fetching")
	}
	e.mu.Lock()
	e.fetching = map[int]struct{}{malID: {}}
	e.mu.Unlock()
	if !e.TitlesPending(ctx, 1) {
		t.Fatal("an aired episode without an English title should be pending while the listing is fetched")
	}

	e.englishTitles(ctx, 1, func() (metadata.Listing, error) { return meta.Listing(ctx, malID) })

	if e.TitlesPending(ctx, 1) {
		t.Fatal("still pending after the fill")
	}
	eps, err := st.Episodes(ctx, 1)
	if err != nil || len(eps) != 2 || eps[0].TitleEN == nil || *eps[0].TitleEN != "Ume Takes Responsibility" {
		t.Fatalf("episodes = %+v, err %v", eps, err)
	}
}
