package library

import (
	"context"
	"fmt"
	"testing"

	"yoru/internal/metadata"
	"yoru/internal/store"
)

// Kept weekly episodes, then SeaDex's Blu-ray best lands: offered, never at a lower resolution, not after being
// turned down, and Replace queues each covered episode from it.
func TestABetterCopyIsOfferedForKeptEpisodes(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	for ep := 1; ep <= 2; ep++ {
		hash := fmt.Sprintf("%040d", ep)
		if err := st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: hash, EngineID: ep, Name: fmt.Sprintf("[SubsPlease] Show - %02d (1080p).mkv", ep),
			AnimeID: 1, EpKey: fmt.Sprint(ep), TotalSize: 1 << 30}); err != nil {
			t.Fatal(err)
		}
		st.SetCacheBytes(ctx, hash, 0, 1<<30, true)
		if _, err := st.KeepDownload(ctx, hash, true); err != nil {
			t.Fatal(err)
		}
	}
	bd := "bdbdbdbdbdbdbdbdbdbdbdbdbdbdbdbdbdbdbdbd"
	if _, err := st.SaveSeaDex(ctx, []metadata.SeaDexRelease{
		{AniListID: 1, InfoHash: bd, ReleaseGroup: "Mehul", Tracker: "Nyaa", IsBest: true, Files: []metadata.SeaDexFile{
			{Name: "Show.S01E01.1080p.Blu-ray.FLAC.x264-Mehul.mkv", Length: 2 << 30},
			{Name: "Show.S01E02.1080p.Blu-ray.FLAC.x264-Mehul.mkv", Length: 2 << 30},
		}},
		{AniListID: 1, InfoHash: "7272727272727272727272727272727272727272", ReleaseGroup: "Small", Tracker: "Nyaa", IsBest: true,
			Files: []metadata.SeaDexFile{{Name: "[Small] Show - 01 (720p).mkv", Length: 300 << 20}, {Name: "[Small] Show - 02 (720p).mkv", Length: 300 << 20}}},
		{AniListID: 1, InfoHash: "", ReleaseGroup: "Private", Tracker: "AB", IsBest: true},
	}); err != nil {
		t.Fatal(err)
	}

	offers, err := BetterCopies(ctx, st, 0)
	if err != nil || len(offers) != 1 {
		t.Fatalf("offers %+v, %v; want one", offers, err)
	}
	o := offers[0]
	if o.InfoHash != bd || len(o.Episodes) != 2 || o.Resolution != "1080p" || o.Size != 4<<30 {
		t.Fatalf("offer %+v; want the 1080p Blu-ray covering both episodes", o)
	}

	if n, err := ReplaceWithBetter(ctx, st, 1, bd); err != nil || n != 2 {
		t.Fatalf("replace queued %d, %v", n, err)
	}
	queued, _ := st.QueuedDownloads(ctx)
	if len(queued) != 2 || queued[0].InfoHash != bd || queued[1].InfoHash != bd {
		t.Fatalf("queued %+v, want both episodes from the Blu-ray", queued)
	}

	if err := DismissBetter(ctx, st, 1, bd); err != nil {
		t.Fatal(err)
	}
	if offers, _ := BetterCopies(ctx, st, 0); len(offers) != 0 {
		t.Errorf("offered again after being turned down: %+v", offers)
	}

	// SeaDex's second best (say, dual audio) is offered next; turning it down too keeps the first away.
	dual := "dadadadadadadadadadadadadadadadadadadada"
	if _, err := st.SaveSeaDex(ctx, []metadata.SeaDexRelease{
		{AniListID: 1, InfoHash: bd, ReleaseGroup: "Mehul", Tracker: "Nyaa", IsBest: true, Files: []metadata.SeaDexFile{
			{Name: "Show.S01E01.1080p.Blu-ray.FLAC.x264-Mehul.mkv", Length: 2 << 30},
			{Name: "Show.S01E02.1080p.Blu-ray.FLAC.x264-Mehul.mkv", Length: 2 << 30},
		}},
		{AniListID: 1, InfoHash: dual, ReleaseGroup: "Dual", Tracker: "Nyaa", IsBest: true, Files: []metadata.SeaDexFile{
			{Name: "[Dual] Show - 01 (BD 1080p) [Dual Audio].mkv", Length: 3 << 30},
			{Name: "[Dual] Show - 02 (BD 1080p) [Dual Audio].mkv", Length: 3 << 30},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if offers, _ := BetterCopies(ctx, st, 0); len(offers) != 1 || offers[0].InfoHash != dual {
		t.Fatalf("offers %+v; want the dual audio one", offers)
	}
	if err := DismissBetter(ctx, st, 1, dual); err != nil {
		t.Fatal(err)
	}
	if offers, _ := BetterCopies(ctx, st, 0); len(offers) != 0 {
		t.Errorf("turning down a second offer brought the first back: %+v", offers)
	}

	// A pack counting on through the seasons ("Show - 13") holds episodes 1 and 2 by their absolute numbers.
	if _, err := st.SaveEpisodes(ctx, 1, []metadata.Episode{{Number: 1, Absolute: 13}, {Number: 2, Absolute: 14}}); err != nil {
		t.Fatal(err)
	}
	counted := "c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0c0"
	if _, err := st.SaveSeaDex(ctx, []metadata.SeaDexRelease{
		{AniListID: 1, InfoHash: counted, ReleaseGroup: "Count", Tracker: "Nyaa", IsBest: true, Files: []metadata.SeaDexFile{
			{Name: "[Count] Show - 13 (BD 1080p).mkv", Length: 2 << 30},
			{Name: "[Count] Show - 14 (BD 1080p).mkv", Length: 2 << 30},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if offers, _ := BetterCopies(ctx, st, 0); len(offers) != 1 || len(offers[0].Episodes) != 2 {
		t.Errorf("offers %+v; want the counted-on pack covering both episodes", offers)
	}
}
