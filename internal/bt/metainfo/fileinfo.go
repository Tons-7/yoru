package metainfo

import (
	"strings"

	g "github.com/anacrolix/generics"

	infohash_v2 "yoru/internal/bt/types/infohash-v2"
)

// Information specific to a single file inside the MetaInfo structure.
type FileInfo struct {
	// BEP 3; optional under BEP 47, which the API can't say without breaking.
	Length int64    `bencode:"length"`
	Path   []string `bencode:"path"` // BEP3
	// Unofficial extension by BiglyBT? https://github.com/BiglySoftware/BiglyBT/issues/1274. Might
	// be a safer bet when available.
	PathUtf8 []string `bencode:"path.utf-8,omitempty" json:"path.utf-8,omitempty"`

	ExtendedFileAttrs

	// BEP 52, not encoded in a v1 FileInfo but here for APIs dealing in v1 files.
	PiecesRoot    g.Option[infohash_v2.T] `bencode:"-" json:"-"`
	TorrentOffset int64                   `bencode:"-" json:"-"`
}

func (fi *FileInfo) DisplayPath(info *Info) string {
	if info.IsDir() {
		return strings.Join(fi.BestPath(), "/")
	} else {
		return info.BestName()
	}
}

func (fi *FileInfo) BestPath() []string {
	if len(fi.PathUtf8) != 0 {
		return fi.PathUtf8
	}
	return fi.Path
}

func (fi *FileInfo) BeginPieceIndex(pieceLength int64) int {
	if pieceLength == 0 {
		return 0
	}
	return int(fi.TorrentOffset / pieceLength)
}

func (fi *FileInfo) EndPieceIndex(pieceLength int64) int {
	if pieceLength == 0 {
		return 0
	}
	return int((fi.TorrentOffset + fi.Length + pieceLength - 1) / pieceLength)
}
