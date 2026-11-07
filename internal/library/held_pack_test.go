package library

import (
	"context"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/score"
	"yoru/internal/store"
)

func TestAnEpisodeComesFromThePackTheShowIsAlreadyDownloading(t *testing.T) {
	quickWarm(t)
	engine := newFakeEngine()
	pack := release(goodHash, "[Sav1or] Sousou no Frieren + Movies + OVA [BD][1080p][Dual Audio]", 43)
	pack.Size = 20 << 30
	p := newPlayback(t, engine, []indexer.Torrent{
		release(otherHash, "[Quick] Sousou no Frieren - 01 [1080p].mkv", 60),
		pack,
	})
	ctx := context.Background()

	found, err := p.finder.Find(ctx, Request{AnimeID: 1, Episode: 1, Season: 1, Prefs: score.DefaultPreferences()})
	if err != nil {
		t.Fatal(err)
	}
	if found.Results[0].Torrent.InfoHash != otherHash {
		t.Fatalf("before anything is held, picked %s", found.Results[0].Torrent.Title)
	}

	if err := p.store.RecordTorrent(ctx, store.TorrentRecord{InfoHash: goodHash, EngineID: 9, Name: pack.Title,
		AnimeID: 1, EpKey: "2", FilePath: "Season 1/Frieren - S01E02.mkv", TotalSize: 1 << 30}); err != nil {
		t.Fatal(err)
	}
	p.store.SetCacheBytes(ctx, goodHash, 0, 1<<30, true)

	found, err = p.finder.Find(ctx, Request{AnimeID: 1, Episode: 1, Season: 1, Prefs: score.DefaultPreferences()})
	if err != nil {
		t.Fatal(err)
	}
	if top := found.Results[0]; top.Torrent.InfoHash != goodHash || !top.Held {
		t.Fatalf("picked %s; want the pack episode 2 already came from", top.Torrent.Title)
	}
}
