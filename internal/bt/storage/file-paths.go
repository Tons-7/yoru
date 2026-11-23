package storage

import (
	"os"
	"path/filepath"
	"strings"

	"yoru/internal/bt/metainfo"
)

type FilePathMaker func(opts FilePathMakerOpts) string

type TorrentDirFilePathMaker func(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string

type FilePathMakerOpts struct {
	Info *metainfo.Info
	File *metainfo.FileInfo
}

func defaultPathMaker(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string {
	return baseDir
}

func infoHashPathMaker(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string {
	return filepath.Join(baseDir, infoHash.HexString())
}

func isSubFilepath(base, sub string) bool {
	rel, err := filepath.Rel(base, sub)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
