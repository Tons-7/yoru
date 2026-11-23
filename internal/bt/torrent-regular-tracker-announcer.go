package bt

import (
	"net/url"
)

type torrentRegularTrackerAnnouncer struct {
	u                *url.URL
	getAnnounceState func() announceState
}

var _ torrentTrackerAnnouncer = torrentRegularTrackerAnnouncer{}

func (r torrentRegularTrackerAnnouncer) statusLine() string {
	return regularTrackerScraperStatusLine(r.getAnnounceState())
}

func (r torrentRegularTrackerAnnouncer) URL() *url.URL {
	return r.u
}

func (r torrentRegularTrackerAnnouncer) Stop() {
}
