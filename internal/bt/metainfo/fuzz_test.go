package metainfo

import (
	"os"
	"path/filepath"
	"testing"

	"yoru/internal/bt/bencode"
)

func Fuzz(f *testing.F) {
	matches, err := filepath.Glob(filepath.FromSlash("testdata/*.torrent"))
	if err != nil {
		f.Fatal(err)
	}
	matches = append(matches, filepath.FromSlash("../testdata/bittorrent-v2-test.torrent"))
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err != nil {
			f.Fatal(err)
		}
		f.Logf("adding %q", m)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		var mi MetaInfo
		err := bencode.Unmarshal(b, &mi)
		if err != nil {
			t.Skip(err)
		}
		_, err = bencode.Marshal(mi)
		if err != nil {
			panic(err)
		}
		info, err := mi.UnmarshalInfo()
		if err != nil {
			t.Skip(err)
		}
		_, err = bencode.Marshal(info)
		if err != nil {
			panic(err)
		}
		if info.Validate() != nil {
			return
		}
		info.TotalLength()
		n := info.NumPieces()
		info.UpvertedFiles()
		info.FileSegmentsIndex()
		if info.HasV1() {
			for i := range min(n, len(info.Pieces)/20) {
				info.Piece(i).Length()
			}
		}
	})
}
