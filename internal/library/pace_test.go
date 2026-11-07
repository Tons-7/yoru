package library

import (
	"context"
	"testing"
	"time"

	"yoru/internal/indexer"
	"yoru/internal/score"
)

func crawlBar(t *testing.T) {
	t.Helper()
	factor, sample, patience, peers := paceFactor, paceSample, slowPatience, fewPeers
	paceFactor, paceSample, slowPatience, fewPeers = 100, 100*time.Millisecond, 2*time.Second, 10
	t.Cleanup(func() { paceFactor, paceSample, slowPatience, fewPeers = factor, sample, patience, peers })
}

func TestAFasterReleasePlaysOverOneThatCannotKeepUp(t *testing.T) {
	quickWarm(t)
	crawlBar(t)
	engine := newFakeEngine()
	engine.trickle[goodHash] = 200 * time.Millisecond

	p := newPlayback(t, engine, []indexer.Torrent{
		release(goodHash, "[Crawl] Sousou no Frieren - 01 [1080p].mkv", 900),
		release(otherHash, "[Quick] Sousou no Frieren - 01 [1080p].mkv", 50),
	})
	start := time.Now()
	rel, _, _, _, _, err := p.attach(context.Background(), PlayRequest{
		AnimeID: 1, Episode: 1, Season: 1, Prefs: score.DefaultPreferences(),
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if rel.Torrent.InfoHash != otherHash {
		t.Fatalf("picked %s, want the release that keeps up", rel.Torrent.InfoHash)
	}
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Errorf("took %s; a ready faster release should not wait out the patience", took)
	}
}

func TestAFastReleaseWaitsForABetterOneStillStarting(t *testing.T) {
	quickWarm(t)
	crawlBar(t)
	stall := headStall
	headStall = 10 * time.Second
	t.Cleanup(func() { headStall = stall })
	engine := newFakeEngine()
	engine.trickle[goodHash] = 200 * time.Millisecond
	engine.stuck[otherHash] = true

	p := newPlayback(t, engine, []indexer.Torrent{
		release(goodHash, "[Crawl] Sousou no Frieren - 01 [1080p BluRay].mkv", 900),
		release(otherHash, "[Second] Sousou no Frieren - 01 [1080p BluRay].mkv", 400),
		release(thirdHash, "[Third] Sousou no Frieren - 01 [1080p].mkv", 30),
	})
	found, _ := p.finder.Find(context.Background(), Request{AnimeID: 1, Episode: 1, Season: 1, Prefs: score.DefaultPreferences()})
	if len(found.Results) != 3 || found.Results[2].Torrent.InfoHash != thirdHash {
		t.Fatalf("the third release should rank last: %v", found.Results)
	}
	start := time.Now()
	rel, _, _, _, _, err := p.attach(context.Background(), PlayRequest{
		AnimeID: 1, Episode: 1, Season: 1, Prefs: score.DefaultPreferences(),
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if took := time.Since(start); took < slowPatience/2 {
		t.Fatalf("took %s: the third-ranked release won while the second was still starting", took)
	}
	if rel.Torrent.InfoHash != thirdHash {
		t.Fatalf("picked %s; after the wait the faster one should play over the slow one", rel.Torrent.Title)
	}
}

func TestASlowReleaseStillPlaysWhenNothingIsFaster(t *testing.T) {
	quickWarm(t)
	crawlBar(t)
	engine := newFakeEngine()
	engine.trickle[goodHash] = 200 * time.Millisecond

	p := newPlayback(t, engine, []indexer.Torrent{
		release(goodHash, "[Crawl] Sousou no Frieren - 01 [1080p].mkv", 900),
	})
	start := time.Now()
	rel, _, _, _, _, err := p.attach(context.Background(), PlayRequest{
		AnimeID: 1, Episode: 1, Season: 1, Prefs: score.DefaultPreferences(),
	})
	if err != nil {
		t.Fatalf("the only release was refused for being slow: %v", err)
	}
	if rel.Torrent.InfoHash != goodHash {
		t.Fatalf("picked %s", rel.Torrent.InfoHash)
	}
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Errorf("took %s; with nothing else racing there is nothing to wait for", took)
	}
}

func TestASlowReleaseWaitsBrieflyThenPlaysWhenTheOthersFail(t *testing.T) {
	quickWarm(t)
	crawlBar(t)
	engine := newFakeEngine()
	engine.trickle[goodHash] = 200 * time.Millisecond
	engine.stuck[otherHash] = true

	p := newPlayback(t, engine, []indexer.Torrent{
		release(goodHash, "[Crawl] Sousou no Frieren - 01 [1080p].mkv", 900),
		release(otherHash, "[Stuck] Sousou no Frieren - 01 [1080p].mkv", 50),
	})
	rel, _, _, _, _, err := p.attach(context.Background(), PlayRequest{
		AnimeID: 1, Episode: 1, Season: 1, Prefs: score.DefaultPreferences(),
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if rel.Torrent.InfoHash != goodHash {
		t.Fatalf("picked %s, want the slow one that at least delivers", rel.Torrent.InfoHash)
	}
}

func TestASlowReleaseWithManyPeersIsTheConnectionNotTheSwarm(t *testing.T) {
	quickWarm(t)
	crawlBar(t)
	fewPeers = 4
	engine := newFakeEngine()
	engine.trickle[goodHash] = 200 * time.Millisecond

	p := newPlayback(t, engine, []indexer.Torrent{
		release(goodHash, "[Crawl] Sousou no Frieren - 01 [1080p].mkv", 900),
		release(otherHash, "[Quick] Sousou no Frieren - 01 [1080p].mkv", 50),
	})
	rel, _, _, _, _, err := p.attach(context.Background(), PlayRequest{
		AnimeID: 1, Episode: 1, Season: 1, Prefs: score.DefaultPreferences(),
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if rel.Torrent.InfoHash != goodHash {
		t.Fatalf("picked %s; a release with 4 peers connected is held back by the line, not its swarm", rel.Torrent.InfoHash)
	}
}

func TestAHandPickedSlowReleaseIsNotSecondGuessed(t *testing.T) {
	quickWarm(t)
	crawlBar(t)
	engine := newFakeEngine()
	engine.trickle[goodHash] = 200 * time.Millisecond

	p := newPlayback(t, engine, []indexer.Torrent{
		release(goodHash, "[Crawl] Sousou no Frieren - 01 [1080p].mkv", 900),
		release(otherHash, "[Quick] Sousou no Frieren - 01 [1080p].mkv", 50),
	})
	rel, _, _, _, _, err := p.attach(context.Background(), PlayRequest{
		AnimeID: 1, Episode: 1, Season: 1, Prefs: score.DefaultPreferences(), InfoHash: goodHash,
	})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if rel.Torrent.InfoHash != goodHash {
		t.Fatalf("picked %s over the one chosen by hand", rel.Torrent.InfoHash)
	}
}
