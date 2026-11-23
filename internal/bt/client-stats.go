package bt

import (
	"net/netip"

	g "github.com/anacrolix/generics"
)

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

	ActiveHalfOpenAttempts int

	NumPeersUndialableWithoutHolepunch                            int
	NumPeersUndialableWithoutHolepunchDialedAfterHolepunchConnect int
	NumPeersDialableOnlyAfterHolepunch                            int
	NumPeersDialedSuccessfullyAfterHolepunchConnect               int
	NumPeersProbablyOnlyConnectedDueToHolepunch                   int
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
