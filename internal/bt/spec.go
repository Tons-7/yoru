package bt

import (
	"fmt"

	g "github.com/anacrolix/generics"

	"yoru/internal/bt/metainfo"
	infohash_v2 "yoru/internal/bt/types/infohash-v2"
)

type TorrentSpec struct {
	AddTorrentOpts
	Trackers    [][]string
	DisplayName string
	Webseeds    []string
	DhtNodes    []string
	PeerAddrs   []string
	Sources     []string
	PieceLayers map[string]string
}

func TorrentSpecFromMagnetUri(uri string) (spec *TorrentSpec, err error) {
	m, err := metainfo.ParseMagnetV2Uri(uri)
	if err != nil {
		return
	}
	spec = &TorrentSpec{
		Trackers:    [][]string{m.Trackers},
		DisplayName: m.DisplayName,
		Webseeds:    m.Params["ws"],
		Sources:     append(m.Params["xs"], m.Params["as"]...),
		PeerAddrs:   m.Params["x.pe"],
	}
	spec.InfoHash = m.InfoHash.UnwrapOrZeroValue()
	spec.InfoHashV2 = m.V2InfoHash
	return
}

func TorrentSpecFromMetaInfoErr(mi *metainfo.MetaInfo) (*TorrentSpec, error) {
	info, err := mi.UnmarshalInfo()
	if err != nil {
		err = fmt.Errorf("unmarshalling info: %w", err)
	}
	var v1Ih metainfo.Hash
	if info.HasV1() {
		v1Ih = mi.HashInfoBytes()
	}
	var v2Infohash g.Option[infohash_v2.T]
	if info.HasV2() {
		v2Infohash.Set(infohash_v2.HashBytes(mi.InfoBytes))
	}

	return &TorrentSpec{
		Trackers:    mi.UpvertedAnnounceList(),
		PieceLayers: mi.PieceLayers,
		DisplayName: info.BestName(),
		Webseeds:    mi.UrlList,
		DhtNodes: func() (ret []string) {
			ret = make([]string, 0, len(mi.Nodes))
			for _, node := range mi.Nodes {
				ret = append(ret, string(node))
			}
			return
		}(),
		AddTorrentOpts: AddTorrentOpts{
			InfoHash:   v1Ih,
			InfoHashV2: v2Infohash,
			InfoBytes:  mi.InfoBytes,
		},
	}, err
}

func TorrentSpecFromMetaInfo(mi *metainfo.MetaInfo) *TorrentSpec {
	ts, err := TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		panic(err)
	}
	return ts
}
