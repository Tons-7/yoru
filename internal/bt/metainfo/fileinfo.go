package metainfo

import (
	"strings"

	g "github.com/anacrolix/generics"

	infohash_v2 "yoru/internal/bt/types/infohash-v2"
)

type FileInfo struct {
	Length   int64    `bencode:"length"`
	Path     []string `bencode:"path"`
	PathUtf8 []string `bencode:"path.utf-8,omitempty" json:"path.utf-8,omitempty"`

	ExtendedFileAttrs

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
