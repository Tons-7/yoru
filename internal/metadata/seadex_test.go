package metadata

import (
	"context"
	"io"
	"net/http"
	"slices"
	"testing"
)

// The curator's notes, comparison pages (web addresses only) and each release's files come through.
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
