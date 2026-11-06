package library

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/parse"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

const silentWitchReport = 62422

func silentWitchStore(t *testing.T) *store.Store {
	t.Helper()
	st := openStore(t)
	ctx := context.Background()
	special := entryOf(silentWitchReport, "SPECIAL", "Silent Witch: Chinmoku no Majo no Kakushigoto - Sennyuu Kiroku", "", "2025-09-20", 1, 24)
	if _, err := st.ImportList(ctx, []store.Anime{special}, nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	var titles []store.CorpusTitle
	for _, text := range []string{
		"Secrets of the Silent Witch: Undercover Report",
		"Secrets of the Silent Witch Episode 9.5",
		"Secrets of the Silent Witch Recap",
		"Silent Witch: Chinmoku no Majo no Kakushigoto Episode 9.5",
	} {
		titles = append(titles, store.CorpusTitle{Text: text, Kind: "synonym", Lang: "en"})
	}
	if _, err := st.SaveCorpus(ctx, []store.CorpusEntry{{AniListID: silentWitchReport, Kind: "SPECIAL", Episodes: 1, Titles: titles}}); err != nil {
		t.Fatal(err)
	}
	return st
}

func silentWitchReleases() []indexer.Torrent {
	return []indexer.Torrent{
		sized("sp01", "[SubsPlease] Silent Witch - Chinmoku no Majo no Kakushigoto - 01 (1080p) [8C8A2840].mkv", 90, 1.4),
		sized("varyg01", "Secrets of the Silent Witch S01E01 1080p CR WEB-DL AAC2.0 H 264-VARYG (Silent Witch: Chinmoku no Majo no Kakushigoto, Multi-Subs)", 80, 1.4),
		sized("report", "Secrets of the Silent Witch S01E07 The Report 1080p CR WEB-DL AAC2.0 H 264-VARYG (Silent Witch: Chinmoku no Majo no Kakushigoto, Multi-Subs)", 70, 1.4),
		sized("special", "Secrets of the Silent Witch S01E9 5 Undercover Report 1080p CR WEB-DL AAC2.0 H 264-VARYG (Silent Witch: Chinmoku no Majo no Kakushigoto, Multi-Subs)", 7, 1.4),
	}
}

func TestSpecialIsNotItsSeriesFirstEpisode(t *testing.T) {
	got, _ := findFilm(t, silentWitchStore(t), silentWitchReport, silentWitchReleases())

	have := kept(got)
	for _, hash := range []string{"sp01", "varyg01", "report"} {
		if have[hash] {
			t.Errorf("%s was offered for the special", byHash(t, got, hash).Torrent.Title)
		}
	}
	if !have["special"] {
		t.Fatalf("the special's own release was dropped: kept %v", have)
	}
	if got.Best == nil || got.Best.Torrent.InfoHash != "special" {
		t.Errorf("best = %+v, want the release named Undercover Report", got.Best)
	}
}

func TestAPastedMagnetIsListedFirst(t *testing.T) {
	st := silentWitchStore(t)
	idx := &askedIndexer{results: silentWitchReleases()}
	f := NewFinder(st, idx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.Ask(silentWitchReport, 1, "", &indexer.Torrent{
		Title: "Pasted magnet 624c5996", InfoHash: "624c5996ce245a670cd052bc2d53d92b11891974", Indexer: Pasted,
	})
	got, err := f.Find(context.Background(), Request{AnimeID: silentWitchReport, Episode: 1, Prefs: score.DefaultPreferences()})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Results) == 0 || got.Results[0].Torrent.Indexer != Pasted {
		t.Fatalf("first = %+v, want the pasted magnet", got.Results)
	}
	if got.Best != nil && got.Best.Torrent.Indexer == Pasted {
		t.Errorf("the pasted magnet became the automatic pick")
	}
}

func judasFiles(withSpecial bool) []torrent.File {
	var files []torrent.File
	for ep := 1; ep <= 13; ep++ {
		files = append(files, torrent.File{Name: fmt.Sprintf("[Judas] Silent Witch - S01E%02d.mkv", ep), Length: 300 << 20})
	}
	if withSpecial {
		files = append(files, torrent.File{Name: "[Judas] Silent Witch - S01SP01.mkv", Length: 300 << 20})
	}
	return files
}

func TestASpecialPlaysItsFileFromASeasonBatch(t *testing.T) {
	batch := score.Result{Candidate: score.Candidate{
		Release:       parse.Parse("[Judas] Silent Witch Chinmoku no Majo no Kakushigoto (Season 01) [1080p][HEVC x265 10bit][Dual-Audio][Multi-Subs] (Batch)"),
		TotalEpisodes: 1, Episode: 1, Numbers: []int{1},
	}}
	if !batch.Release.Batch {
		t.Fatal("the release does not parse as a batch")
	}
	f, _, ok := pickFile(judasFiles(true), batch)
	if !ok || !strings.Contains(f.Name, "SP01") {
		t.Errorf("picked %q, want the batch's special", f.Name)
	}

	series := batch
	series.TotalEpisodes = 13
	if f, _, ok := pickFile(judasFiles(true), series); !ok || !strings.Contains(f.Name, "S01E01") {
		t.Errorf("the series' episode 1 picked %q from the same batch", f.Name)
	}
}

func TestSubtitleWordsAreWhatOnlyTheSpecialsNamesAdd(t *testing.T) {
	got := subtitleWords([]string{
		"Silent Witch: Chinmoku no Majo no Kakushigoto - Sennyuu Kiroku",
		"Secrets of the Silent Witch: Undercover Report",
		"Secrets of the Silent Witch Recap",
	})
	want := map[string]bool{"kiroku sennyuu": true, "report undercover": true}
	if len(got) != len(want) {
		t.Fatalf("subtitles = %v", got)
	}
	for _, words := range got {
		key := words[0]
		for _, w := range words[1:] {
			key += " " + w
		}
		if !want[key] {
			t.Errorf("unexpected subtitle %v in %v", words, got)
		}
	}
}
