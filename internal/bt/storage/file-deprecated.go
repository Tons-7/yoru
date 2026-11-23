package storage

import (
	"yoru/internal/bt/metainfo"
)

func NewFileWithCompletion(baseDir string, completion PieceCompletion) ClientImplCloser {
	return NewFileWithCustomPathMakerAndCompletion(baseDir, nil, completion)
}

func NewFileByInfoHash(baseDir string) ClientImplCloser {
	return NewFileWithCustomPathMaker(baseDir, infoHashPathMaker)
}

func NewFileWithCustomPathMaker(baseDir string, pathMaker func(baseDir string, info *metainfo.Info, infoHash metainfo.Hash) string) ClientImplCloser {
	return NewFileWithCustomPathMakerAndCompletion(baseDir, pathMaker, pieceCompletionForDir(baseDir))
}

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
