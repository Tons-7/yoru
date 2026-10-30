package store

import (
	"context"
	"slices"
	"testing"
	"time"
)

func libraryIDs(page Page[LibraryItem]) []int {
	ids := []int{}
	for _, it := range page.Items {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestLibraryFavouritesOnlyCombinesWithStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	all := Paging{Page: 1, PerPage: 50}

	if _, err := s.ImportList(ctx, []Anime{{ID: 1, Romaji: "A"}, {ID: 2, Romaji: "B"}, {ID: 3, Romaji: "C"}}, []Entry{
		{ID: 11, AnimeID: 1, Status: new("CURRENT")},
		{ID: 12, AnimeID: 2, Status: new("COMPLETED")},
	}, ImportMerge); err != nil {
		t.Fatal(err)
	}
	yes := true
	for _, id := range []int{1, 3} {
		if err := s.PatchBookmark(ctx, id, BookmarkPatch{Favourite: &yes}); err != nil {
			t.Fatal(err)
		}
	}

	page, err := s.Library(ctx, LibraryFilter{Favourite: true, Sort: "title"}, all)
	if err != nil {
		t.Fatal(err)
	}
	if got := libraryIDs(page); !slices.Equal(got, []int{1, 3}) || page.Total != 2 {
		t.Fatalf("favourites = %v total %d, want the listed one and the unlisted one", got, page.Total)
	}
	if !page.Items[0].Favourite || page.Items[1].Status != nil {
		t.Fatalf("items = %+v", page.Items)
	}

	page, err = s.Library(ctx, LibraryFilter{Favourite: true, Status: "CURRENT"}, all)
	if err != nil {
		t.Fatal(err)
	}
	if got := libraryIDs(page); !slices.Equal(got, []int{1}) || page.Total != 1 {
		t.Fatalf("favourites watching = %v total %d", got, page.Total)
	}

	page, _ = s.Library(ctx, LibraryFilter{Status: "COMPLETED"}, all)
	if len(page.Items) != 1 || page.Items[0].Favourite {
		t.Fatalf("completed = %+v, want the one that is not a favourite", page.Items)
	}

	counts, err := s.LibraryCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts.Favourites != 2 || counts.FavouriteStatuses["CURRENT"] != 1 || counts.FavouriteStatuses["COMPLETED"] != 0 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestLibraryEverySortRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.ImportList(ctx, []Anime{{ID: 1, Romaji: "A"}}, []Entry{{ID: 1, AnimeID: 1, Status: new("CURRENT")}}, ImportMerge); err != nil {
		t.Fatal(err)
	}
	for _, sort := range LibrarySorts {
		for _, fav := range []bool{false, true} {
			if _, err := s.Library(ctx, LibraryFilter{Sort: sort, Favourite: fav}, Paging{Page: 1, PerPage: 10}); err != nil {
				t.Fatalf("sort %q fav %v: %v", sort, fav, err)
			}
		}
	}
}

func TestLibrarySortsByLastWatchedNewEpisodesAndDateAdded(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	all := Paging{Page: 1, PerPage: 50}
	releasing := "RELEASING"
	next := 6

	if _, err := s.ImportList(ctx, []Anime{
		{ID: 1, Romaji: "Behind", Status: &releasing, NextEpisode: &next},
		{ID: 2, Romaji: "Caught up", Status: &releasing, NextEpisode: &next},
		{ID: 3, Romaji: "Finished"},
	}, []Entry{
		{ID: 30, AnimeID: 1, Status: new("CURRENT"), Progress: 3},
		{ID: 10, AnimeID: 2, Status: new("CURRENT"), Progress: 5},
		{ID: 20, AnimeID: 3, Status: new("CURRENT"), Progress: 1},
	}, ImportMerge); err != nil {
		t.Fatal(err)
	}
	playThrough(t, s, 2, "5", 0, 600, 1400)

	page, _ := s.Library(ctx, LibraryFilter{Sort: "watched"}, all)
	if got := libraryIDs(page); got[0] != 2 {
		t.Fatalf("last watched = %v, want the one played first", got)
	}
	if page.Items[0].Resume == nil || page.Items[0].Resume.LastPlayed == 0 {
		t.Fatalf("resume = %+v, want when it was played", page.Items[0].Resume)
	}

	page, _ = s.Library(ctx, LibraryFilter{Sort: "new"}, all)
	if got := libraryIDs(page); got[0] != 1 {
		t.Fatalf("new episodes first = %v, want the airing show with an unwatched episode", got)
	}
	if page.Items[0].Airing == nil || *page.Items[0].Airing != releasing {
		t.Fatalf("airing = %v", page.Items[0].Airing)
	}

	seedAnime(t, s, 4)
	if err := s.SetListStatus(ctx, 4, "PLANNING", -1); err != nil {
		t.Fatal(err)
	}
	page, _ = s.Library(ctx, LibraryFilter{Sort: "added"}, all)
	if got := libraryIDs(page); !slices.Equal(got, []int{4, 1, 3, 2}) {
		t.Fatalf("date added = %v, want the local entry, then entry ids newest first", got)
	}
}

func TestHistoryCountsAnEpisodePastTheThresholdAsFinished(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)

	if _, err := s.SavePlayback(ctx, PlaybackState{AnimeID: 1, EpKey: "1", Position: 1350, Duration: 1400, Played: 30}); err != nil {
		t.Fatal(err)
	}
	play(t, s, 1, "2", 300, 1400)

	page, err := s.History(ctx, Paging{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	byEp := map[int]HistoryEntry{}
	for _, e := range page.Items {
		byEp[e.Episode] = e
	}
	if e := byEp[1]; e.Watched || !e.Finished {
		t.Fatalf("episode 1 = watched %v finished %v, want finished without the tick", e.Watched, e.Finished)
	}
	if byEp[2].Finished {
		t.Fatal("an episode a fifth in is not finished")
	}

	stats, err := s.WatchStats(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Episodes != 1 {
		t.Fatalf("episodes finished = %d, want the one past the threshold", stats.Episodes)
	}
}

func TestHistoryFinishedAndEpisodesFinishedAgree(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []int{1, 2, 3} {
		seedAnime(t, s, id)
	}
	if err := s.SetSetting(ctx, "sync.progress_at", "0.5"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAnimePref(ctx, 2, "sync.progress_at", EndingThreshold); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAnimePref(ctx, 3, "sync.progress_at", "0.7"); err != nil {
		t.Fatal(err)
	}

	type row struct {
		anime    int
		ep       string
		position float64
		duration any
		watched  int
		done     any
		want     bool
	}
	rows := []row{
		{1, "1", 1400, 1440.0, 1, nil, true},
		{1, "2", 800, 1400.0, 0, nil, true},
		{1, "3", 300, 1400.0, 1, nil, false},
		{1, "4", 0, 1400.0, 1, nil, false},
		{1, "5", 10, nil, 1, nil, true},
		{1, "6", 1300, 1400.0, 0, 1290.0, true},
		{2, "1", 900, 1400.0, 0, nil, false},
		{2, "2", 1300, 1400.0, 0, nil, true},
		{3, "1", 900, 1400.0, 0, nil, false},
		{3, "2", 1000, 1400.0, 0, nil, true},
	}
	for i, r := range rows {
		if _, err := s.w.ExecContext(ctx, `
			INSERT INTO playback (anime_id, ep_key, position_s, duration_s, played_s, watched, play_count, last_played_at, done_s)
			VALUES (?, ?, ?, ?, 0, ?, 1, ?, ?)`, r.anime, r.ep, r.position, r.duration, r.watched, 1000+i, r.done); err != nil {
			t.Fatal(err)
		}
	}

	page, err := s.History(ctx, Paging{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	got := map[[2]int]bool{}
	finishedRows := 0
	for _, e := range page.Items {
		got[[2]int{e.AnimeID, e.Episode}] = e.Finished
		if e.Finished || e.Watched {
			finishedRows++
		}
	}
	for _, r := range rows {
		key := [2]int{r.anime, episodeFromKey(r.ep)}
		if f, ok := got[key]; ok && f != r.want || !ok && r.want {
			t.Errorf("anime %d episode %s: finished %v (listed %v), want %v", r.anime, r.ep, f, ok, r.want)
		}
	}

	stats, err := s.WatchStats(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Episodes != finishedRows {
		t.Fatalf("Episodes finished = %d, want %d: every row finished now or ever", stats.Episodes, finishedRows)
	}
}

func TestHistoryLeavesOutEpisodesNeverPlayed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)

	if _, err := s.SavePlayback(ctx, PlaybackState{AnimeID: 1, EpKey: "4", Position: 0, Duration: 1400}); err != nil {
		t.Fatal(err)
	}
	page, err := s.History(ctx, Paging{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.Total != 0 {
		t.Fatalf("history = %+v total %d, want an opened episode left out", page.Items, page.Total)
	}
}

func TestHistoryCarriesTheShowsListProgress(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	eps := 12
	if _, err := s.ImportList(ctx, []Anime{{ID: 1, Romaji: "A", Episodes: &eps}},
		[]Entry{{ID: 1, AnimeID: 1, Status: new("CURRENT"), Progress: 3}}, ImportMerge); err != nil {
		t.Fatal(err)
	}
	playThrough(t, s, 1, "4", 0, 200, 1400)

	page, _ := s.History(ctx, Paging{Page: 1, PerPage: 50})
	if len(page.Items) != 1 {
		t.Fatalf("history = %+v", page.Items)
	}
	e := page.Items[0]
	if e.Progress != 3 || e.Episodes == nil || *e.Episodes != 12 || e.ListStatus == nil || *e.ListStatus != "CURRENT" {
		t.Fatalf("entry = %+v", e)
	}
}
