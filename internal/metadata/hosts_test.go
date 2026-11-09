package metadata

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAnimeNotFoundIsNotRetried(t *testing.T) {
	asked := 0
	c := New()
	c.SetURL("tenrai-anime", serve(t, func(w http.ResponseWriter, r *http.Request) { asked++; http.NotFound(w, r) }))

	if _, err := c.Anime(context.Background(), 5); err == nil {
		t.Fatal("want not found")
	}
	if asked != 1 {
		t.Fatalf("%d requests; a missing record is not worth retrying", asked)
	}
}

const fullJSON = `{"data":{"mal_id":1,"theme":{"openings":["1: \"Yuusha\" by YOASOBI (eps 1-16)"]},"trailer":{"url":"t"}}}`

const staffJSON = `{"data":[{"person":{"name":"Keiichirou Saitou"},"positions":["Director"]}]}`

func TestExtraFlagsAMissingStaffHalf(t *testing.T) {
	c := New()
	c.SetURL("tenrai-full", serve(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, fullJSON) }))
	c.SetURL("tenrai-staff", serve(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "busy", http.StatusBadGateway) }))

	got, err := c.Extra(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Openings) != 1 || !got.StaffMissing {
		t.Fatalf("got %+v; want the themes, flagged as missing its staff", got)
	}
}

func TestExtraReadsThemesAndStaff(t *testing.T) {
	c := New()
	c.SetURL("tenrai-full", serve(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, fullJSON) }))
	c.SetURL("tenrai-staff", serve(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, staffJSON) }))

	got, err := c.Extra(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Trailer != "t" || len(got.Staff) != 1 || got.StaffMissing {
		t.Fatalf("got %+v; want the themes and staff", got)
	}
}
