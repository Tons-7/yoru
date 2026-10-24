package player

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteChaptersFillsGaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chapters.ini")
	err := WriteChapters(path, []SkipRange{
		{Kind: "op", Start: 128, End: 218},
		{Kind: "ed", Start: 1343, End: 1433},
	}, 1440)
	if err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)

	if !strings.HasPrefix(got, ";FFMETADATA1") {
		t.Fatalf("missing ffmetadata header:\n%s", got)
	}
	// uosc tints the seekbar by matching these titles.
	for _, want := range []string{"title=Opening", "title=Ending", "title=Episode"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if n := strings.Count(got, "[CHAPTER]"); n != 5 {
		t.Errorf("%d chapters, want 5 (pre-OP, OP, body, ED, tail)", n)
	}
	if !strings.Contains(got, "START=128000") || !strings.Contains(got, "END=218000") {
		t.Errorf("opening not written in milliseconds:\n%s", got)
	}
}

// The file's ending precedes AniSkip's opening in the list; written so, the body read as the opening.
func TestWriteChaptersInTimeOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chapters.ini")
	if err := WriteChapters(path, []SkipRange{
		{Kind: "ed", Start: 1343, End: 1433},
		{Kind: "op", Start: 128, End: 218},
		{Kind: "recap", Start: 200, End: 260},
	}, 1440); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	got := string(body)
	op, ed := strings.Index(got, "title=Opening"), strings.Index(got, "title=Ending")
	if op < 0 || ed < 0 || op > ed {
		t.Fatalf("opening and ending out of order:\n%s", got)
	}
	if strings.Contains(got, "title=Recap") {
		t.Errorf("a range overlapping the opening was written too:\n%s", got)
	}
	if n := strings.Count(got, "[CHAPTER]"); n != 5 {
		t.Errorf("%d chapters, want 5:\n%s", n, got)
	}
}

func TestWriteChaptersEdgeCases(t *testing.T) {
	dir := t.TempDir()

	t.Run("no ranges", func(t *testing.T) {
		path := filepath.Join(dir, "a.ini")
		if err := WriteChapters(path, nil, 1440); err != nil {
			t.Fatal(err)
		}
		body, _ := os.ReadFile(path)
		if n := strings.Count(string(body), "[CHAPTER]"); n != 1 {
			t.Errorf("%d chapters, want a single episode chapter", n)
		}
	})

	t.Run("zero length range is skipped", func(t *testing.T) {
		path := filepath.Join(dir, "b.ini")
		WriteChapters(path, []SkipRange{{Kind: "op", Start: 100, End: 100}}, 1440)
		body, _ := os.ReadFile(path)
		if strings.Contains(string(body), "title=Opening") {
			t.Error("wrote a zero-length chapter")
		}
	})

	t.Run("opening at zero has no gap chapter", func(t *testing.T) {
		path := filepath.Join(dir, "c.ini")
		WriteChapters(path, []SkipRange{{Kind: "op", Start: 0, End: 90}}, 1440)
		body, _ := os.ReadFile(path)
		if n := strings.Count(string(body), "[CHAPTER]"); n != 2 {
			t.Errorf("%d chapters, want opening plus body", n)
		}
	})
}

func TestChapterTitle(t *testing.T) {
	tests := map[string]string{
		"op": "Opening", "mixed-op": "Opening",
		"ed": "Ending", "mixed-ed": "Ending",
		"recap": "Recap",
	}
	for kind, want := range tests {
		if got := chapterTitle(kind); got != want {
			t.Errorf("chapterTitle(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestSkipRangeContains(t *testing.T) {
	r := SkipRange{Start: 128, End: 218}

	cases := map[float64]bool{127.9: false, 128: true, 200: true, 217.9: true, 218: false, 300: false}
	for at, want := range cases {
		if got := r.Contains(at); got != want {
			t.Errorf("Contains(%.1f) = %v, want %v", at, got, want)
		}
	}
}

// A trusted source's part wins, and one source keeps every range of a part: an A and a B eyecatch.
func TestMergeKeepsEveryRangeOfTheSourceItTakes(t *testing.T) {
	marked := []SkipRange{{Kind: "op", Start: 80, End: 170, Source: "marked"}}
	chapters := []SkipRange{
		{Kind: "op", Start: 85, End: 175, Source: "chapters"},
		{Kind: "eyecatch", Start: 600, End: 605, Source: "chapters"},
		{Kind: "eyecatch", Start: 610, End: 615, Source: "chapters"},
	}
	aniskip := []SkipRange{
		{Kind: "eyecatch", Start: 700, End: 705, Source: "aniskip"},
		// One ending given twice: kept once.
		{Kind: "ed", Start: 1300, End: 1390, Source: "aniskip"},
		{Kind: "mixed-ed", Start: 1302, End: 1392, Source: "aniskip"},
	}
	got := Merge(marked, chapters, aniskip)
	if len(got) != 4 || got[0].Source != "marked" || got[1].Start != 600 || got[2].Start != 610 || got[3].Start != 1300 {
		t.Errorf("merged %+v, want the marked opening, both chapter eyecatches and one ending", got)
	}
}
