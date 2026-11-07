package library

import (
	"context"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/parse"
	"yoru/internal/score"
)

func TestTheFormatChoiceDecidesBetweenAv1AndHevc(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	cand := func(title string, seeders int) score.Candidate {
		return score.Candidate{
			Torrent:       indexer.Torrent{Title: title, Size: 5 << 30, Seeders: seeders, SeedersKnown: true},
			Release:       parse.Parse(title),
			TotalEpisodes: 12,
		}
	}
	av1 := cand("[Sav1or] Princess Principal + Movies + OVA [BD][1080p][AV1][OPUS][Dual Audio]", 41)
	hevc := cand("[AkihitoSubs] Princess Principal + Extras [BD 1920x1080 x265 10Bit AC3]", 11)

	pick := func(choice string) string {
		if err := st.SetSetting(ctx, "quality.codec", choice); err != nil {
			t.Fatal(err)
		}
		p, err := st.Prefs(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		prefs := Preferences(p)
		prefs.HardwareTranscode = true
		return score.Rank([]score.Candidate{hevc, av1}, prefs)[0].Release.Group
	}
	if got := pick("av1"); got != "Sav1or" {
		t.Errorf("AV1 first picked %s", got)
	}
	if got := pick("hevc"); got != "AkihitoSubs" {
		t.Errorf("HEVC first picked %s", got)
	}
	if got := pick("any"); got != "Sav1or" {
		t.Errorf("no preference picked %s; with the codec out of it the bigger swarm should win", got)
	}
}

func TestAv1FirstAddsATierAboveEachCodecTier(t *testing.T) {
	ladder := []score.Tier{score.ParseTier("1080p:BD:HEVC:10"), score.ParseTier("1080p:BD"), score.ParseTier("720p")}
	got := withCodec(ladder, "av1")
	want := []score.Tier{score.ParseTier("1080p:BD:AV1"), score.ParseTier("1080p:BD:HEVC:10"), score.ParseTier("1080p:BD"), score.ParseTier("720p")}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tier %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if any := withCodec(ladder, "any"); len(any) != 2 || any[0] != score.ParseTier("1080p:BD") {
		t.Errorf("no preference gave %v", any)
	}
}
