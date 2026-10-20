package indexer

import (
	"context"
	"errors"
	"testing"
)

// The list swaps whole: bad entries named and skipped, kept ones keep their cache, adult ones search apart.
func TestSitesSwapWhileRunning(t *testing.T) {
	s := NewSites()
	if Configured(s.General()) || Configured(s.Adult()) {
		t.Fatal("an empty list reports something to search")
	}
	if _, err := s.General().Search(context.Background(), Query{Text: "x"}); !errors.Is(err, ErrNoSites) {
		t.Fatalf("search with no sites: %v, want ErrNoSites", err)
	}

	nyaa := Site{Type: "nyaa", URL: "https://nyaa.example"}
	errs := s.Set([]Site{nyaa, {Type: "torznab", URL: "https://x.example"}, {Type: "nyaa", URL: "https://adult.example", Adult: true}})
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want the unknown type alone", errs)
	}
	if general, adult := s.Counts(); general != 1 || adult != 1 {
		t.Fatalf("counts = %d general, %d adult; want 1 and 1", general, adult)
	}
	if !Configured(s.General()) || !Configured(s.Adult()) {
		t.Fatal("configured sites read as none")
	}

	before := s.cur.Load().cached[nyaa.key()]
	s.Set([]Site{nyaa, {Type: "tokyotosho", URL: "https://tt.example"}})
	if s.cur.Load().cached[nyaa.key()] != before {
		t.Error("a site kept across an edit lost its cache")
	}
	if general, adult := s.Counts(); general != 2 || adult != 0 {
		t.Errorf("after the edit: %d general, %d adult; want 2 and 0", general, adult)
	}
}
