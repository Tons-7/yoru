package storage

import (
	"yoru/internal/bt/metainfo"
)

func NewFileWithCompletion(baseDir string, completion PieceCompletion) ClientImplCloser {
	return NewFileWithCustomPathMakerAndCompletion(baseDir, nil, completion)
}

// File storage with data partitioned by infohash.
func NewFileByInfoHash(baseDir string) ClientImplCloser {
	return NewFileWithCustomPathMaker(baseDir, infoHashPathMaker)
}

// Deprecated: Takes a function giving each torrent's data path; it must sanitise what it uses (info.Name).
func NewFileWithCustomPathMaker(baseDir string, pathMaker func(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string) ClientImplCloser {
	return NewFileWithCustomPathMakerAndCompletion(baseDir, pathMaker, pieceCompletionForDir(baseDir))
}

// Deprecated: Allows passing custom PieceCompletion
func NewFileWithCustomPathMakerAndCompletion(
	baseDir string,
	pathMaker TorrentDirFilePathMaker,
	completion PieceCompletion,
) ClientImplCloser {
	return NewFileOpts(NewFileClientOpts{
		ClientBaseDir:   baseDir,
		TorrentDirMaker: pathMaker,
		PieceCompletion: completion,
	})
}
