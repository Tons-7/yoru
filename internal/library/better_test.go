package library

import (
	"context"
	"fmt"
	"testing"

	"yoru/internal/metadata"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

func keepEpisode(t *testing.T, st *store.Store, ep int, hash, name string) {
	t.Helper()
	ctx := context.Background()
	if err := st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: hash, EngineID: ep, Name: name,
		AnimeID: 1, EpKey: fmt.Sprint(ep), FilePath: name, TotalSize: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	st.SetCacheBytes(ctx, hash, 0, 1<<30, true)
	if _, err := st.KeepDownload(ctx, hash, true); err != nil {
		t.Fatal(err)
	}
}

func keepWeekly(t *testing.T, st *store.Store, episodes int) {
	t.Helper()
	for ep := 1; ep <= episodes; ep++ {
		keepEpisode(t, st, ep, fmt.Sprintf("%040d", ep), fmt.Sprintf("[SubsPlease] Show - %02d (1080p).mkv", ep))
	}
}

func seadex(t *testing.T, st *store.Store, releases ...metadata.SeaDexRelease) {
	t.Helper()
	if _, err := st.SaveSeaDex(context.Background(), releases); err != nil {
		t.Fatal(err)
	}
}

func best(hash, group string, dual bool, files ...string) metadata.SeaDexRelease {
	r := metadata.SeaDexRelease{AniListID: 1, InfoHash: hash, ReleaseGroup: group, Tracker: "Nyaa", IsBest: true, DualAudio: dual}
	for _, f := range files {
		r.Files = append(r.Files, metadata.SeaDexFile{Name: f, Length: 2 << 30})
	}
	return r
}

const (
	mehul = "bdbdbdbdbdbdbdbdbdbdbdbdbdbdbdbdbdbdbdbd"
	dual  = "dadadadadadadadadadadadadadadadadadadada"
)

func TestABetterCopyIsOfferedForKeptEpisodes(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	keepWeekly(t, st, 2)
	seadex(t, st,
		best(mehul, "Mehul", false, "Show.S01E01.1080p.Blu-ray.FLAC.x264-Mehul.mkv", "Show.S01E02.1080p.Blu-ray.FLAC.x264-Mehul.mkv"),
		best("7272727272727272727272727272727272727272", "Small", false, "[Small] Show - 01 (720p).mkv", "[Small] Show - 02 (720p).mkv"),
		metadata.SeaDexRelease{AniListID: 1, ReleaseGroup: "Private", Tracker: "AB", IsBest: true},
	)

	offers, err := BetterCopies(ctx, st, nil, 0)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers %+v, %v; want one", offers, err)
	}
	o := offers[0]
	if o.InfoHash != mehul || len(o.Episodes) != 2 || o.Resolution != "1080p" || o.Size != 4<<30 {
		t.Fatalf("offer %+v; want the 1080p Blu-ray covering both episodes", o)
	}

	if n, err := ReplaceWithBetter(ctx, st, nil, 1, mehul); err != nil || n != 2 {
		t.Fatalf("replace queued %d, %v", n, err)
	}
	queued, _ := st.QueuedDownloads(ctx)
	if len(queued) != 2 || queued[0].InfoHash != mehul || queued[1].InfoHash != mehul {
		t.Fatalf("queued %+v, want both episodes from the Blu-ray", queued)
	}
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 0 {
		t.Errorf("the offer came back while its replacement waits in the queue: %+v", offers)
	}
}

func TestTurningDownAnOfferKeepsItAway(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	keepWeekly(t, st, 2)
	seadex(t, st,
		best(mehul, "Mehul", false, "Show.S01E01.1080p.Blu-ray.x264-Mehul.mkv", "Show.S01E02.1080p.Blu-ray.x264-Mehul.mkv"),
		best(dual, "Dual", true, "[Dual] Show - 01 (BD 1080p) [Dual Audio].mkv", "[Dual] Show - 02 (BD 1080p) [Dual Audio].mkv"),
	)

	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 1 || offers[0].InfoHash != mehul {
		t.Fatalf("offers %+v; with Japanese audio preferred, want the Japanese-only best", offers)
	}
	if err := DismissBetter(ctx, st, nil, 1, mehul); err != nil {
		t.Fatal(err)
	}
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 1 || offers[0].InfoHash != dual {
		t.Fatalf("offers %+v; want the dual audio one next", offers)
	}
	if err := DismissBetter(ctx, st, nil, 1, dual); err != nil {
		t.Fatal(err)
	}
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 0 {
		t.Errorf("turning down a second offer brought the first back: %+v", offers)
	}
}

func TestADubWatcherIsOfferedTheDualAudioBest(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	keepWeekly(t, st, 2)
	if err := st.SetAnimePref(ctx, 1, "audio.prefer", "dub"); err != nil {
		t.Fatal(err)
	}
	seadex(t, st,
		best(mehul, "Mehul", false, "Show.S01E01.1080p.Blu-ray.x264-Mehul.mkv", "Show.S01E02.1080p.Blu-ray.x264-Mehul.mkv"),
		best(dual, "Dual", true, "[Dual] Show - 01 (BD 1080p) [Dual Audio].mkv", "[Dual] Show - 02 (BD 1080p) [Dual Audio].mkv"),
	)
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 1 || offers[0].InfoHash != dual {
		t.Fatalf("offers %+v; want the dual audio best", offers)
	}
}

func TestAPackCountingOnThroughTheSeasonsIsOffered(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	keepWeekly(t, st, 2)
	if _, err := st.SaveEpisodes(ctx, 1, []metadata.Episode{{Number: 1, Absolute: 13}, {Number: 2, Absolute: 14}}); err != nil {
		t.Fatal(err)
	}
	seadex(t, st, best("c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0", "Count", false, "[Count] Show - 13 (BD 1080p).mkv", "[Count] Show - 14 (BD 1080p).mkv"))
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 1 || len(offers[0].Episodes) != 2 {
		t.Errorf("offers %+v; want the counted-on pack covering both episodes", offers)
	}
}

func TestAKeptSeaDexBestIsNotOfferedAnotherBest(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	tharja := "7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a"
	keepEpisode(t, st, 1, tharja, "[Tharja] Show - 01 (BD 1080p) [Dual Audio].mkv")
	keepEpisode(t, st, 2, "7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b7b", "[Tharja] Show - 02 (BD 1080p) [Dual Audio].mkv")
	seadex(t, st,
		best(tharja, "Tharja", true, "[Tharja] Show - 01 (BD 1080p) [Dual Audio].mkv", "[Tharja] Show - 02 (BD 1080p) [Dual Audio].mkv"),
		best("3b3b3b3b3b3b3b3b3b3b3b3b3b3b3b3b3b3b3b3b", "MTBB", false, "[MTBB] Show - 01 (BD 1080p).mkv", "[MTBB] Show - 02 (BD 1080p).mkv"),
	)
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 0 {
		t.Errorf("a show already on a SeaDex best was offered a sideways swap: %+v", offers)
	}
}

func TestANewerRevisionOfTheKeptGroupIsOffered(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	keepEpisode(t, st, 1, "7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a", "[Tharja] Show - 01 (BD 1080p).mkv")
	v2 := "7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c7c"
	seadex(t, st, best(v2, "Tharja", false, "[Tharja] Show - 01v2 (BD 1080p).mkv"))
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 1 || offers[0].InfoHash != v2 {
		t.Errorf("offers %+v; want the v2 of the kept group", offers)
	}
}

func TestASharperBestOfTheKeptGroupIsOffered(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	keepEpisode(t, st, 1, "7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a", "[Tharja] Show - 01 (720p).mkv")
	sharp := "7d7d7d7d7d7d7d7d7d7d7d7d7d7d7d7d7d7d7d7d"
	seadex(t, st, best(sharp, "Tharja", false, "[Tharja] Show - 01 (BD 1080p).mkv"))
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 1 || offers[0].InfoHash != sharp {
		t.Errorf("offers %+v; want the group's 1080p over the kept 720p", offers)
	}
}

func TestTheKeptGroupsBlurayIsOfferedOverItsWebCopy(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	keepEpisode(t, st, 1, "7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a", "[Tharja] Show - 01 (WEB 1080p).mkv")
	bd := "7e7e7e7e7e7e7e7e7e7e7e7e7e7e7e7e7e7e7e7e"
	seadex(t, st, best(bd, "Tharja", false, "[Tharja] Show - 01 (BD 1080p).mkv"))
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 1 || offers[0].InfoHash != bd {
		t.Errorf("offers %+v; want the group's Blu-ray over its web copy", offers)
	}
}

func TestADubWatcherIsOfferedTheKeptGroupsDualAudio(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	keepEpisode(t, st, 1, "7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a", "[Tharja] Show - 01 (BD 1080p).mkv")
	twin := "7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f"
	seadex(t, st, best(twin, "Tharja", true, "[Tharja] Show - 01 (BD 1080p) [Dual Audio].mkv"))
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 0 {
		t.Fatalf("offers %+v; a sub watcher has no use for the dual audio copy", offers)
	}
	if err := st.SetAnimePref(ctx, 1, "audio.prefer", "dub"); err != nil {
		t.Fatal(err)
	}
	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 1 || offers[0].InfoHash != twin {
		t.Errorf("offers %+v; a dub watcher should get the group's dual audio copy", offers)
	}
}

func TestAReleaseSplitAcrossTorrentsIsOneOffer(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	keepWeekly(t, st, 3)
	var releases []metadata.SeaDexRelease
	for ep := 1; ep <= 3; ep++ {
		releases = append(releases, best(fmt.Sprintf("%038d%02d", 7, ep), "Pizza", false, fmt.Sprintf("[Pizza] Show - %02d (BD 1080p).mkv", ep)))
	}
	releases = append(releases, best(fmt.Sprintf("%038d%02d", 7, 4), "Pizza", false, "[Pizza] Show - 04 (BD 1080p).mkv"))
	seadex(t, st, releases...)

	offers, _ := BetterCopies(ctx, st, nil, 0)
	if len(offers) != 1 || len(offers[0].Episodes) != 3 || offers[0].Size != 6<<30 {
		t.Fatalf("offers %+v; want one offer for all three episodes", offers)
	}
	if n, err := ReplaceWithBetter(ctx, st, nil, 1, offers[0].InfoHash); err != nil || n != 3 {
		t.Fatalf("replace queued %d, %v", n, err)
	}
	queued, _ := st.QueuedDownloads(ctx)
	for _, q := range queued {
		if want := fmt.Sprintf("%038d%02d", 7, q.Episode); q.InfoHash != want {
			t.Errorf("episode %d queued from %s, want its own torrent %s", q.Episode, q.InfoHash, want)
		}
	}

	st2 := prefetchStore(t)
	keepWeekly(t, st2, 3)
	seadex(t, st2, releases...)
	if err := DismissBetter(ctx, st2, nil, 1, fmt.Sprintf("%038d%02d", 7, 2)); err != nil {
		t.Fatal(err)
	}
	if offers, _ := BetterCopies(ctx, st2, nil, 0); len(offers) != 0 {
		t.Errorf("turning down the release left part of it offered: %+v", offers)
	}
	keepEpisode(t, st2, 4, fmt.Sprintf("%040d", 4), "[SubsPlease] Show - 04 (1080p).mkv")
	if offers, _ := BetterCopies(ctx, st2, nil, 0); len(offers) != 0 {
		t.Errorf("a turned-down release came back for an episode kept later: %+v", offers)
	}
}

func TestALaterSeasonIsOfferedItsOwnFilesFromAMultiSeasonPack(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	keepEpisode(t, st, 1, fmt.Sprintf("%040d", 1), "[SubsPlease] Show S2 - 01 (1080p).mkv")
	pack := best("5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e", "Big", false,
		"Show/Season 1/Show - S01E01 (BD 1080p).mkv", "Show/Season 2/Show - S02E01 (BD 1080p).mkv")
	pack.Files[1].Length = 3 << 30
	seadex(t, st, pack)

	if offers, _ := BetterCopies(ctx, st, nil, 0); len(offers) != 0 {
		t.Fatalf("offers %+v; without the season the pack is ambiguous", offers)
	}
	season2 := func(context.Context, int) torrent.Scope { return torrent.Scope{Season: 2} }
	offers, _ := BetterCopies(ctx, st, season2, 0)
	if len(offers) != 1 || offers[0].Size != 3<<30 {
		t.Fatalf("offers %+v; want the season 2 file", offers)
	}
}
