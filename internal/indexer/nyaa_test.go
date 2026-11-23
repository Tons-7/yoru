package indexer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testNyaa(t *testing.T, h http.HandlerFunc) *Nyaa {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewNyaa(srv.URL, WithNyaaDelay(time.Nanosecond))
}

func TestNyaaWaitsAfterBeingToldToSlowDown(t *testing.T) {
	calls := 0
	n := testNyaa(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	for range 3 {
		if _, err := n.Search(context.Background(), Query{Text: "frieren"}); err == nil || !strings.HasPrefix(err.Error(), "HTTP 429") {
			t.Fatalf("err = %v, want a 429", err)
		}
	}
	if calls != 1 {
		t.Errorf("asked the site %d times while it said to wait", calls)
	}
}

func TestNyaaSaysWhenAPageIsNotAListing(t *testing.T) {
	n := testNyaa(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><body><h1>Checking your browser</h1></body></html>"))
	})
	if _, err := n.Search(context.Background(), Query{Text: "frieren"}); err == nil {
		t.Fatal("an interstitial page read as no results")
	}
}

func TestNyaaSearchErrors(t *testing.T) {
	t.Run("http error", func(t *testing.T) {
		n := testNyaa(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		})
		if _, err := n.Search(context.Background(), Query{Text: "x"}); err == nil {
			t.Fatal("expected an error")
		}
	})

	t.Run("empty query", func(t *testing.T) {
		calls := 0
		n := testNyaa(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
		if _, err := n.Search(context.Background(), Query{Text: "  "}); err == nil || calls != 0 {
			t.Fatalf("err = %v after %d calls, want a refusal without asking the site", err, calls)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		n := testNyaa(t, func(w http.ResponseWriter, r *http.Request) {})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := n.Search(ctx, Query{Text: "x"}); err == nil {
			t.Fatal("expected cancellation")
		}
	})
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"1.4 GiB", 1503238553},
		{"512.0 MiB", 536870912},
		{"8.2 GiB", 8804682956},
		{"1 TiB", 1 << 40},
		{"700 MB", 700 * 1000 * 1000},
		{"1.35GB", 1_350_000_000},
		{"512.4MB", 512_400_000},
		{"1,024 MiB", 1 << 30},
		{"", 0},
		{"garbage", 0},
		{"1.4", 0},
		{"1.4 parsecs", 0},
		{"GB", 0},
	}

	for _, tt := range tests {
		if got := ParseSize(tt.in); got != tt.want {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestMagnet(t *testing.T) {
	tr := Torrent{Title: "[Group] Show - 01 [1080p]", InfoHash: "abc123"}
	magnet := tr.Magnet()

	if !strings.HasPrefix(magnet, "magnet:?xt=urn:btih:abc123") {
		t.Fatalf("magnet = %q", magnet)
	}
	if !strings.Contains(magnet, "dn=%5BGroup%5D+Show+-+01+%5B1080p%5D") {
		t.Errorf("display name not escaped: %q", magnet)
	}
	if n := strings.Count(magnet, "&tr="); n != len(trackers) {
		t.Errorf("%d trackers, want %d", n, len(trackers))
	}
	if (Torrent{}).Magnet() != "" {
		t.Error("a torrent with no infohash should produce no magnet")
	}
}

func TestMagnetKeepsTheReleasesOwnTrackers(t *testing.T) {
	own := "udp://tracker.example:1337/announce"
	got := trackersOf(Torrent{InfoHash: "abc", Trackers: []string{own, trackers[1]}}.Magnet())
	if len(got) != 1+len(trackers) || got[0] != own {
		t.Errorf("trackers %v; want the release's own first, the public ones after, none twice", got)
	}
	nyaa := trackersOf(Torrent{InfoHash: "abc", Indexer: "nyaa"}.Magnet())
	if len(nyaa) == 0 || nyaa[0] != nyaaTracker {
		t.Errorf("a Nyaa release's trackers %v; want Nyaa's own first", nyaa)
	}
	if got := trackersOf("magnet:?xt=urn:btih:abc&dn=100%+Ep&tr=" + url.QueryEscape(own)); len(got) != 1 || got[0] != own {
		t.Errorf("trackers %v beside a bad name; want %s", got, own)
	}
}

type fakeSource struct {
	name    string
	results []Torrent
	err     error
}

func (f fakeSource) Name() string { return f.name }
func (f fakeSource) Search(context.Context, Query) ([]Torrent, error) {
	return f.results, f.err
}

func TestMultiDedupesByInfoHash(t *testing.T) {
	m := Multi{Sources: []Source{
		fakeSource{name: "a", results: []Torrent{
			{InfoHash: "aaa", Seeders: 5, SeedersKnown: true},
			{InfoHash: "bbb", Seeders: 1, SeedersKnown: true},
		}},
		fakeSource{name: "b", results: []Torrent{
			{InfoHash: "AAA", Seeders: 50, SeedersKnown: true},
			{InfoHash: "ccc", Seeders: 2, SeedersKnown: true},
		}},
	}}

	got, err := m.Search(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d results, want 3 after dedupe", len(got))
	}
	if got[0].Seeders != 50 {
		t.Errorf("seeders = %d, want the higher count kept", got[0].Seeders)
	}
}

func TestMultiPrefersCountedPeersOverNone(t *testing.T) {
	m := Multi{Sources: []Source{
		fakeSource{name: "tokyotosho", results: []Torrent{
			{InfoHash: "aaa"}, {InfoHash: "ddd"},
		}},
		fakeSource{name: "nyaa", results: []Torrent{
			{InfoHash: "aaa", Seeders: 42, Leechers: 3, SeedersKnown: true},
		}},
	}}

	got, err := m.Search(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want 2", len(got))
	}
	if !got[0].SeedersKnown || got[0].Seeders != 42 || got[0].Leechers != 3 {
		t.Errorf("shared torrent = %+v, want the counted record adopted", got[0])
	}
	if got[1].SeedersKnown || got[1].Seeders != 0 {
		t.Errorf("unshared torrent = %+v, want its count left unknown", got[1])
	}
}

func TestMultiKeepsCountsWhenTheSecondSourceHasNone(t *testing.T) {
	m := Multi{Sources: []Source{
		fakeSource{name: "nyaa", results: []Torrent{
			{InfoHash: "aaa", Seeders: 7, SeedersKnown: true},
		}},
		fakeSource{name: "tokyotosho", results: []Torrent{{InfoHash: "AAA"}}},
	}}

	got, err := m.Search(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Seeders != 7 || !got[0].SeedersKnown {
		t.Fatalf("got %+v, want the counted record kept", got)
	}
}

func TestMultiToleratesFailingSource(t *testing.T) {
	m := Multi{Sources: []Source{
		fakeSource{name: "broken", err: io.ErrUnexpectedEOF},
		fakeSource{name: "ok", results: []Torrent{{InfoHash: "aaa"}}},
	}}

	got, err := m.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("one dead source should not fail the search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results", len(got))
	}
}

func TestMultiEmptyAndFailingIsNotAnError(t *testing.T) {
	m := Multi{Sources: []Source{
		fakeSource{name: "empty"},
		fakeSource{name: "broken", err: io.ErrUnexpectedEOF},
	}}
	if got, err := m.Search(context.Background(), Query{}); err != nil || len(got) != 0 {
		t.Fatalf("got %d results, err %v; want an empty answer", len(got), err)
	}
}

func TestMultiMergesFlags(t *testing.T) {
	m := Multi{Sources: []Source{
		fakeSource{name: "a", results: []Torrent{{InfoHash: "aaa"}}},
		fakeSource{name: "b", results: []Torrent{{InfoHash: "AAA", Trusted: true, Remake: true, Category: "1_2"}}},
	}}
	got, err := m.Search(context.Background(), Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].Trusted || !got[0].Remake || got[0].Category != "1_2" {
		t.Fatalf("got %+v", got)
	}
}

func TestMultiReportsErrorWhenAllFail(t *testing.T) {
	m := Multi{Sources: []Source{
		fakeSource{name: "a", err: io.ErrUnexpectedEOF},
		fakeSource{name: "b", err: io.ErrUnexpectedEOF},
	}}

	if _, err := m.Search(context.Background(), Query{}); err == nil {
		t.Fatal("expected an error when every source fails")
	}
}
