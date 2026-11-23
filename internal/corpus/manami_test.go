package corpus

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestManamiReadsEveryRecordKind(t *testing.T) {
	lines := []string{
		`{"sources":["https://anilist.co/anime/154587","https://myanimelist.net/anime/52991"],"title":"Sousou no Frieren","type":"TV","episodes":28,"picture":"https://img/f.jpg","animeSeason":{"season":"FALL","year":2023},"synonyms":["Frieren"]}`,
		`{"sources":["https://myanimelist.net/anime/5114"],"title":"Fullmetal Alchemist: Brotherhood","type":"TV","episodes":64,"animeSeason":{"season":"SPRING","year":2009},"synonyms":[]}`,
		`{"sources":["https://kitsu.app/anime/1"],"title":"Neither","type":"TV"}`,
		`{not json`,
		``,
	}
	var raw bytes.Buffer
	enc, _ := zstd.NewWriter(&raw)
	for _, l := range lines {
		enc.Write([]byte(l + "\n"))
	}
	enc.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(raw.Bytes()) }))
	t.Cleanup(srv.Close)

	f := NewFetcher()
	f.SetURL("manami", srv.URL)
	var got []Entry
	kept, err := f.Manami(context.Background(), func(e Entry) error {
		got = append(got, e)
		return nil
	})
	if err != nil || kept != 2 || len(got) != 2 {
		t.Fatalf("kept %d (%d yielded), %v; want 2", kept, len(got), err)
	}
	a, b := got[0], got[1]
	if a.AniListID != 154587 || a.MalID != 52991 || a.Year != 2023 || a.Season != "FALL" || a.Picture != "https://img/f.jpg" ||
		len(a.Titles) != 2 || a.Titles[1].Kind != "synonym" || a.Titles[1].Text != "Frieren" {
		t.Errorf("first record = %+v", a)
	}
	if b.AniListID != -5114 || b.MalID != 5114 {
		t.Errorf("a MAL-only record = id %d, mal %d; want -5114 and 5114", b.AniListID, b.MalID)
	}

	stop := errors.New("stop")
	if n, err := f.Manami(context.Background(), func(Entry) error { return stop }); !errors.Is(err, stop) || n != 0 {
		t.Errorf("a failing yield gave %d, %v; want it to stop the scan with that error", n, err)
	}
}
