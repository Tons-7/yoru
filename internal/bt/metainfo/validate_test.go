package metainfo

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"yoru/internal/bt/bencode"
)

func v2File(length int64, root string) FileTree {
	return FileTree{File: FileTreeFile{Length: length, PiecesRoot: root}}
}

func v2Info(pieceLength int64, files map[string]FileTree) *Info {
	return &Info{Name: "x", PieceLength: pieceLength, MetaVersion: 2, FileTree: FileTree{Dir: files}}
}

func TestValidateRejectsHostileInfos(t *testing.T) {
	root := strings.Repeat("r", 32)
	for name, info := range map[string]*Info{
		"negative piece length": {PieceLength: -1, Length: 1, Pieces: make([]byte, 20)},
		"huge piece length":     {PieceLength: 1 << 40, Length: 1, Pieces: make([]byte, 20)},
		"odd pieces":            {PieceLength: 1 << 14, Length: 1, Pieces: make([]byte, 19)},
		"negative v1 length":    {PieceLength: 1 << 14, Length: -5},
		"negative v1 file":      {PieceLength: 1 << 14, Files: []FileInfo{{Length: -1, Path: []string{"a"}}}},
		"v2 zero piece length":  v2Info(0, map[string]FileTree{"a": v2File(10, root)}),
		"v2 not power of two":   v2Info(20000, map[string]FileTree{"a": v2File(10, root)}),
		"v2 short root":         v2Info(1<<14, map[string]FileTree{"a": v2File(10, root[:31])}),
		"v2 missing root":       v2Info(1<<14, map[string]FileTree{"a": v2File(10, "")}),
		"v2 negative length":    v2Info(1<<14, map[string]FileTree{"a": v2File(-1, root)}),
		"v2 too many pieces":    v2Info(1<<14, map[string]FileTree{"a": v2File(1<<40, root)}),
		"hybrid short pieces": func() *Info {
			i := v2Info(1<<14, map[string]FileTree{"a": v2File(3<<14, root)})
			i.Pieces = make([]byte, 40)
			return i
		}(),
	} {
		assert.Error(t, info.Validate(), name)
	}
}

func TestValidateAcceptsGoodInfos(t *testing.T) {
	root := strings.Repeat("r", 32)
	ok := v2Info(1<<14, map[string]FileTree{"a": v2File(3<<14, root), "b": v2File(0, ""), "c": v2File(0, root)})
	require.NoError(t, ok.Validate())
	ok.Pieces = make([]byte, 60)
	require.NoError(t, ok.Validate())
	require.NoError(t, (&Info{PieceLength: 1 << 14, Length: 100, Pieces: make([]byte, 20)}).Validate())

	b, err := os.ReadFile("../testdata/bittorrent-v2-test.torrent")
	require.NoError(t, err)
	var mi MetaInfo
	require.NoError(t, bencode.Unmarshal(b, &mi))
	info, err := mi.UnmarshalInfo()
	require.NoError(t, err)
	require.NoError(t, info.Validate())
}

func TestV2PieceLengths(t *testing.T) {
	root := strings.Repeat("r", 32)
	info := v2Info(16<<10, map[string]FileTree{"a": v2File(40<<10, root), "b": v2File(0, ""), "c": v2File(16<<10, root)})
	require.NoError(t, info.Validate())
	require.Equal(t, 4, info.NumPieces())
	var lengths []int64
	for i := range 4 {
		lengths = append(lengths, info.Piece(i).Length())
	}
	assert.Equal(t, []int64{16 << 10, 16 << 10, 8 << 10, 16 << 10}, lengths)
	files := info.UpvertedFiles()
	require.Len(t, files, 3)
	assert.EqualValues(t, []int64{0, 48 << 10, 48 << 10}, []int64{files[0].TorrentOffset, files[1].TorrentOffset, files[2].TorrentOffset})
	assert.Equal(t, 3, files[2].BeginPieceIndex(info.PieceLength))
	assert.Equal(t, 4, files[2].EndPieceIndex(info.PieceLength))
}

func TestFileTreeDepthIsBounded(t *testing.T) {
	deep := strings.Repeat("d1:a", maxFileTreeDepth+2) + "d0:d6:lengthi1eee" + strings.Repeat("e", maxFileTreeDepth+2)
	var ft FileTree
	assert.Error(t, ft.UnmarshalBencode([]byte(deep)))
	var shallow FileTree
	require.NoError(t, shallow.UnmarshalBencode([]byte("d1:ad0:d6:lengthi5e11:pieces root32:"+strings.Repeat("r", 32)+"eee")))
	assert.EqualValues(t, 5, shallow.Dir["a"].File.Length)
}
