package bt

import (
	"testing"
	"time"

	g "github.com/anacrolix/generics"

	"yoru/internal/bt/tracker"
)

// Every per-tracker lookup starts from the minimum record with the tracker's url set; an announce of that
// tracker ordered before it is skipped, and the index's own check panics (a zero When sorted last once did).
func TestNextAnnounceMinRecordIsTheLeast(t *testing.T) {
	const url trackerAnnouncerKey = "udp://tracker.test:80"
	least := nextAnnounceMinRecord()
	least.url = url
	now := time.Now()
	for _, when := range []time.Time{{}, now.Add(-time.Minute), now.Add(time.Minute)} {
		for _, active := range []bool{false, true} {
			for _, torrent := range []g.Option[nextAnnounceTorrentInput]{
				g.None[nextAnnounceTorrentInput](), g.Some(nextAnnounceTorrentInput{NeedData: true, WantPeers: true}),
			} {
				r := nextAnnounceRecord{}
				r.url = url
				r.When = when
				r.AnnounceEvent = tracker.None
				r.overdue = !when.IsZero() && !when.After(now)
				r.active = active
				r.torrent = torrent
				if announceIndexCompare(least, r) > 0 {
					t.Errorf("when %v active %v torrent %v: sorted before the minimum record", when, active, torrent.Ok)
				}
			}
		}
	}
}

// Among announces not yet due, one with nothing left to do (a zero When) goes last, never at the head.
func TestZeroWhenSortsAfterAPendingAnnounce(t *testing.T) {
	pending := nextAnnounceInput{nextAnnounceStateInput: nextAnnounceStateInput{When: time.Now().Add(time.Minute)}}
	done := nextAnnounceInput{}
	if compareNextAnnounce(done, pending) <= 0 {
		t.Fatal("a zero When sorted at or before a pending announce")
	}
}
