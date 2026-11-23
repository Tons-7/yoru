package metainfo

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"os"
	"time"

	"yoru/internal/bt/bencode"
	infohash_v2 "yoru/internal/bt/types/infohash-v2"
)

type MetaInfo struct {
	InfoBytes    bencode.Bytes     `bencode:"info,omitempty"`
	Announce     string            `bencode:"announce,omitempty" mapstructure:",omitempty"`
	AnnounceList AnnounceList      `bencode:"announce-list,omitempty"`
	Nodes        []Node            `bencode:"nodes,omitempty,ignore_unmarshal_type_error" mapstructure:",omitempty"`
	CreationDate int64             `bencode:"creation date,omitempty,ignore_unmarshal_type_error"`
	Comment      string            `bencode:"comment,omitempty"`
	CreatedBy    string            `bencode:"created by,omitempty"`
	Encoding     string            `bencode:"encoding,omitempty" mapstructure:",omitempty"`
	UrlList      UrlList           `bencode:"url-list,omitempty"`
	PieceLayers  map[string]string `bencode:"piece layers,omitempty" mapstructure:",omitempty"`
}

func Load(r io.Reader) (*MetaInfo, error) {
	var mi MetaInfo
	d := bencode.NewDecoder(r)
	err := d.Decode(&mi)
	if err != nil {
		return nil, err
	}
	err = d.ReadEOF()
	if err != nil {
		err = fmt.Errorf("error after decoding metainfo: %w", err)
	}
	return &mi, err
}

func LoadFromFile(filename string) (*MetaInfo, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var buf bufio.Reader
	buf.Reset(f)
	return Load(&buf)
}

func (mi *MetaInfo) UnmarshalInfo() (info Info, err error) {
	err = bencode.Unmarshal(mi.InfoBytes, &info)
	return
}

func (mi *MetaInfo) HashInfoBytes() (infoHash Hash) {
	return HashBytes(mi.InfoBytes)
}

func (mi *MetaInfo) Write(w io.Writer) error {
	return bencode.NewEncoder(w).Encode(mi)
}

func (mi *MetaInfo) SetDefaults() {
	mi.CreatedBy = "yoru/internal/bt"
	mi.CreationDate = time.Now().Unix()
}

func (mi MetaInfo) Magnet(infoHash *Hash, info *Info) (m Magnet) {
	m.Trackers = append(m.Trackers, mi.UpvertedAnnounceList().DistinctValues()...)
	if info != nil {
		m.DisplayName = info.BestName()
	}
	if infoHash != nil {
		m.InfoHash = *infoHash
	} else {
		m.InfoHash = mi.HashInfoBytes()
	}
	m.Params = make(url.Values)
	m.Params["ws"] = append([]string(nil), mi.UrlList...)
	return
}

func (mi *MetaInfo) MagnetV2() (m MagnetV2, err error) {
	m.Trackers = append(m.Trackers, mi.UpvertedAnnounceList().DistinctValues()...)
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return
	}
	m.DisplayName = info.BestName()
	if info.HasV1() {
		m.InfoHash.Set(mi.HashInfoBytes())
	}
	if info.HasV2() {
		m.V2InfoHash.Set(infohash_v2.HashBytes(mi.InfoBytes))
	}
	m.Params = make(url.Values)
	m.Params["ws"] = append([]string(nil), mi.UrlList...)
	return
}

func (mi *MetaInfo) UpvertedAnnounceList() AnnounceList {
	if mi.AnnounceList.OverridesAnnounce(mi.Announce) {
		return mi.AnnounceList
	}
	if mi.Announce != "" {
		return [][]string{{mi.Announce}}
	}
	return nil
}
