package bt

import (
	"net/netip"

	g "github.com/anacrolix/generics"
)

// Holepunch stats only: a long-running client fails most dials, so each set starts over at a cap.
const holepunchSetMax = 4096

func setAdd[K comparable](m *map[K]struct{}, elem K) {
	g.MakeMapIfNil(m)
	if len(*m) >= holepunchSetMax {
		clear(*m)
	}
	(*m)[elem] = struct{}{}
}

type clientHolepunchAddrSets struct {
	undialableWithoutHolepunch                            map[netip.AddrPort]struct{}
	undialableWithoutHolepunchDialedAfterHolepunchConnect map[netip.AddrPort]struct{}
	dialableOnlyAfterHolepunch                            map[netip.AddrPort]struct{}
	dialedSuccessfullyAfterHolepunchConnect               map[netip.AddrPort]struct{}
	probablyOnlyConnectedDueToHolepunch                   map[netip.AddrPort]struct{}
	accepted                                              map[netip.AddrPort]struct{}
}

type ClientStats struct {
	TorrentStats

	// Dials under way, several per address with holepunch connects; more than the Torrents' sum when one
	// was dropped mid-dial.
	ActiveHalfOpenAttempts int

	NumPeersUndialableWithoutHolepunch int
	// Number of unique peer addresses that were dialed after receiving a holepunch connect message,
	// that have previously been undialable without any hole-punching attempts.
	NumPeersUndialableWithoutHolepunchDialedAfterHolepunchConnect int
	// Number of unique peer addresses that were successfully dialed and connected after a holepunch
	// connect message and previously failing to connect without holepunching.
	NumPeersDialableOnlyAfterHolepunch              int
	NumPeersDialedSuccessfullyAfterHolepunchConnect int
	NumPeersProbablyOnlyConnectedDueToHolepunch     int
}

func (cl *Client) statsLocked() (stats ClientStats) {
	stats.AllConnStats = cl.connStats.Copy()
	stats.TorrentStatCounters = copyCountFields(&cl.counters)
	for t := range cl.torrents {
		stats.TorrentGauges.Add(t.gauges())
	}

	stats.ActiveHalfOpenAttempts = cl.numHalfOpen

	stats.NumPeersUndialableWithoutHolepunch = len(cl.undialableWithoutHolepunch)
	stats.NumPeersUndialableWithoutHolepunchDialedAfterHolepunchConnect = len(cl.undialableWithoutHolepunchDialedAfterHolepunchConnect)
	stats.NumPeersDialableOnlyAfterHolepunch = len(cl.dialableOnlyAfterHolepunch)
	stats.NumPeersDialedSuccessfullyAfterHolepunchConnect = len(cl.dialedSuccessfullyAfterHolepunchConnect)
	stats.NumPeersProbablyOnlyConnectedDueToHolepunch = len(cl.probablyOnlyConnectedDueToHolepunch)

	return
}
