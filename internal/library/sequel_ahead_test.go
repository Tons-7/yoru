package library

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"yoru/internal/indexer"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

func TestDownloadAheadStaysInTheShowUnlessToldToCarryOn(t *testing.T) {
	const season1, season2 = 701, 702
	finale := "dddddddddddddddddddddddddddddddddddddddd"
	opener := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

	for _, carry := range []bool{false, true} {
		t.Run(map[bool]string{false: "switch off", true: "switch on"}[carry], func(t *testing.T) {
			engine := newFakeEngine()
			srv := httptest.NewServer(engine.handler())
			t.Cleanup(srv.Close)

			st := prefetchStore(t)
			ctx := context.Background()
			tv, finished := "TV", "FINISHED"
			two, twelve := 2, 12
			first, second := "Hoshi no Kioku", "Hoshi no Kioku Season 2"
			if _, err := st.ImportList(ctx, []store.Anime{
				{ID: season1, Romaji: first, English: &first, Format: &tv, Status: &finished, Episodes: &two, StartDate: "2024-01-05", Synonyms: "[]", Genres: "[]"},
				{ID: season2, Romaji: second, English: &second, Format: &tv, Status: &finished, Episodes: &twelve, StartDate: "2025-01-05", Synonyms: "[]", Genres: "[]"},
			}, nil, store.ImportMerge); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveRelations(ctx, nil, []store.Relation{{AnimeID: season1, RelatedID: season2, Kind: "SEQUEL"}}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.RebuildFranchises(ctx); err != nil {
				t.Fatal(err)
			}
			if next := st.NextSeason(ctx, season1); next != season2 {
				t.Fatalf("season 1 leads to %d, want season 2", next)
			}
			for key, value := range map[string]string{"cache.prefetch_next": "true", "cache.prefetch_sequel": map[bool]string{false: "false", true: "true"}[carry]} {
				if err := st.SetSetting(ctx, key, value); err != nil {
					t.Fatal(err)
				}
			}

			idx := &askedIndexer{results: []indexer.Torrent{
				release(finale, "[Best] Hoshi no Kioku - 02 [1080p].mkv", 900),
				release(opener, "[Best] Hoshi no Kioku Season 2 - 01 [1080p].mkv", 900),
			}}
			finder := NewFinder(st, idx, discard())
			client := torrent.NewClient(srv.URL)
			pref := NewPrefetcher(st, finder, client, discard())
			play := NewPlayback(st, finder, client, nil, t.TempDir(), discard()).WithPrefetcher(pref)

			if _, err := play.Start(ctx, PlayRequest{AnimeID: season1, Episode: 2, Season: 1, Prefs: score.DefaultPreferences()}); err != nil {
				t.Fatalf("start the finale: %v", err)
			}

			ahead := func() bool {
				rec, ok, _ := st.TorrentForEpisode(ctx, season2, "1")
				return ok && rec.InfoHash == opener
			}
			if carry {
				eventually(t, "season 2's first episode to be downloaded ahead", ahead)
				return
			}
			time.Sleep(2 * time.Second)
			if ahead() {
				t.Error("season 2's first episode was downloaded ahead with the switch off")
			}
			if n := torrentsAdded(engine); n != 1 {
				t.Errorf("added %d torrents, want only the finale", n)
			}
		})
	}
}
