package bt

import (
	"yoru/internal/bt/webseed"
)

// Deprecated: The names doesn't reflect the actual behaviour. You can only add sources. Use
// AddSources instead.
func (t *Torrent) UseSources(sources []string) {
	t.AddSources(sources)
}

// Deprecated: Webseed requests are capped Client-wide, per host or by a cost function on the URL.
func WebSeedTorrentMaxRequests(maxRequests int) AddWebSeedsOpt {
	return func(c *webseed.Client) {
		c.MaxRequests = maxRequests
	}
}
