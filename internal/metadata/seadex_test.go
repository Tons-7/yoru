package metadata

import (
	"context"
	"io"
	"net/http"
	"slices"
	"testing"
)

func TestSeaDexKeepsNotesComparisonsAndFiles(t *testing.T) {
	c := New()
	c.SetURL("seadex", serve(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"page":1,"perPage":500,"totalItems":1,"totalPages":1,"items":[{"alID":100532,
			"incomplete":true,"notes":"Mehul is CsS+Restyled\n","comparison":"https://slow.pics/c/0iBPQRr7, javascript:alert(1) ,https://www.diffchecker.com/9nrmWTwB/",
			"expand":{"trs":[{"infoHash":"7a6e38b4b036a71c101583eb4847f93ea92a5856","releaseGroup":"Mehul","tracker":"Nyaa",
			"isBest":true,"files":[{"name":"Amanchu.S02E01.1080p.Blu-ray-Mehul.mkv","length":939320882}]}]}}]}`)
	}))
	got, err := c.SeaDex(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	r := got[0]
	if r.Notes != "Mehul is CsS+Restyled" || !r.Incomplete {
		t.Errorf("notes %q, incomplete %v", r.Notes, r.Incomplete)
	}
	if !slices.Equal(r.Comparison, []string{"https://slow.pics/c/0iBPQRr7", "https://www.diffchecker.com/9nrmWTwB/"}) {
		t.Errorf("comparison = %v, want the two web pages alone", r.Comparison)
	}
	if len(r.Files) != 1 || r.Files[0].Length != 939320882 {
		t.Errorf("files = %+v", r.Files)
	}
}

func TestSeaDexReadsEveryPageAndDropsPlaceholders(t *testing.T) {
	c := New()
	var pages []string
	c.SetURL("seadex", serve(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		if page == "1" {
			io.WriteString(w, `{"page":1,"totalPages":2,"items":[{"alID":1,"expand":{"trs":[
				{"infoHash":"<redacted>","releaseGroup":"Grp","tags":"a,b"},
				{"infoHash":"<redacted>"}]}}]}`)
			return
		}
		io.WriteString(w, `{"page":2,"totalPages":2,"items":[{"alID":2,"expand":{"trs":[
			{"infoHash":"7a6e38b4b036a71c101583eb4847f93ea92a5856","releaseGroup":"Other"}]}}]}`)
	}))
	got, err := c.SeaDex(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v; want 2 releases", got, err)
	}
	if got[0].InfoHash != "" || got[0].ReleaseGroup != "Grp" || !slices.Equal(got[0].Tags, []string{"a", "b"}) {
		t.Errorf("private-tracker release = %+v, want no hash, its group and tags a,b", got[0])
	}
	if got[1].AniListID != 2 || got[1].InfoHash == "" {
		t.Errorf("page 2 release = %+v", got[1])
	}
	if !slices.Equal(pages, []string{"1", "2"}) {
		t.Errorf("asked pages %v, want 1 and 2", pages)
	}
}
