package score

import (
	"testing"
	"time"
)

func TestTenBitH264IsTakenWhenNothingElseCanBe(t *testing.T) {
	prefs := DefaultPreferences()
	hi10 := seeded("[Kametsu] Princess Principal - Picture Dramas (BD 1080p Hi10 FLAC) [Dual-Audio]", 1<<30, 8)
	dead := seeded("[supai] Princess Principal Picture Dramas [BD 1080p AAC]", 385<<20, 0)

	ranked := Rank([]Candidate{dead, hi10}, prefs)
	if !ranked[0].AutoPick || ranked[0].Torrent.Title != hi10.Torrent.Title {
		t.Fatalf("top %q auto=%v blocked=%q; want the 10-bit release as the last resort", ranked[0].Torrent.Title, ranked[0].AutoPick, ranked[0].Blocked)
	}
	if ranked[1].AutoPick {
		t.Error("a dead swarm was lifted too")
	}

	plain := seeded("[Other] Princess Principal Picture Drama - 01 [1080p].mkv", 200<<20, 3)
	ranked = Rank([]Candidate{hi10, plain}, prefs)
	for _, r := range ranked {
		if r.Torrent.Title == hi10.Torrent.Title && r.AutoPick {
			t.Error("10-bit H.264 was taken while a hardware-decodable release was there")
		}
	}

	prefs.AvoidGroups = []string{"Kametsu"}
	if ranked = Rank([]Candidate{hi10}, prefs); ranked[0].AutoPick {
		t.Error("an avoided group came back as the last resort")
	}
}

func TestAFreshTenBitUploadWaitsForANormalRelease(t *testing.T) {
	hi10 := seeded("[Group] Show - 05 (1080p Hi10P AAC).mkv", 1<<30, 30)
	hi10.Torrent.Published = time.Now().Add(-20 * time.Minute)
	if r := Rank([]Candidate{hi10}, DefaultPreferences())[0]; r.AutoPick || r.Fallback {
		t.Error("a minutes-old 10-bit upload was taken before the usual releases could land")
	}
	hi10.Torrent.Published = time.Now().Add(-72 * time.Hour)
	if r := Rank([]Candidate{hi10}, DefaultPreferences())[0]; !r.AutoPick {
		t.Errorf("a days-old 10-bit release, the only one, stayed blocked: %q", r.Blocked)
	}
}

func TestTenBitIsKeptInReserveBehindAPickableRelease(t *testing.T) {
	hi10 := seeded("[Kametsu] Show (BD 1080p Hi10 FLAC)", 1<<30, 8)
	plain := seeded("[Other] Show - 01 [1080p].mkv", 200<<20, 3)
	ranked := Rank([]Candidate{hi10, plain}, DefaultPreferences())
	more := Fallbacks(ranked)
	if len(more) != 1 || more[0].Torrent.Title != hi10.Torrent.Title || !more[0].AutoPick {
		t.Fatalf("fallbacks %+v; want the 10-bit release ready for when the other fails", more)
	}
	for _, r := range ranked {
		if r.Torrent.Title == hi10.Torrent.Title && r.AutoPick {
			t.Error("the reserve was picked ahead of the normal release")
		}
	}
}
