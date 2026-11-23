package bt

import (
	"reflect"
)

type TorrentStats struct {
	AllConnStats
	TorrentStatCounters
	TorrentGauges
}

type AllConnStats struct {
	ConnStats
	WebSeeds  ConnStats
	PeerConns ConnStats
}

func (me *AllConnStats) Copy() (ret AllConnStats) {
	ret.ConnStats = me.ConnStats.Copy()
	ret.WebSeeds = me.WebSeeds.Copy()
	ret.PeerConns = me.PeerConns.Copy()
	return
}

type TorrentGauges struct {
	TotalPeers       int
	PendingPeers     int
	ActivePeers      int
	ConnectedSeeders int
	HalfOpenPeers    int
	PiecesComplete   int
}

func (me *TorrentGauges) Add(agg TorrentGauges) {
	src := reflect.ValueOf(agg)
	dst := reflect.ValueOf(me).Elem()
	for i := 0; i < reflect.TypeFor[TorrentGauges]().NumField(); i++ {
		*dst.Field(i).Addr().Interface().(*int) += src.Field(i).Interface().(int)
	}
}

type TorrentStatCounters struct {
	BytesHashed Count
}
