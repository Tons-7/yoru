package storage

import (
	"io"
	"os"
	"path/filepath"
	"sync"

	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/segments"
)

func minFileLengthsForTorrentExtent(
	fileSegmentsIndex segments.Index,
	off, n int64,
	each func(fileIndex int, length int64) bool,
) {
	for fileIndex, segmentBounds := range fileSegmentsIndex.LocateIter(segments.Extent{
		Start:  off,
		Length: n,
	}) {
		if !each(fileIndex, segmentBounds.Start+segmentBounds.Length) {
			return
		}
	}
}

func fsync(filePath string) (err error) {
	f, err := os.OpenFile(filePath, os.O_WRONLY, filePerm)
	if err != nil {
		return
	}
	defer f.Close()
	if err = f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

func CreateNativeZeroLengthFile(name string) error {
	os.MkdirAll(filepath.Dir(name), dirPerm)
	var f io.Closer
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		stat, statErr := os.Stat(name)
		if statErr == nil && stat.Mode().IsRegular() && stat.Size() == 0 {
			return nil
		}
		return err
	}
	return f.Close()
}

type file struct {
	*metainfo.Info
	*metainfo.FileInfo
	*fileExtra
}

func (f *file) beginPieceIndex() int {
	return f.FileInfo.BeginPieceIndex(f.Info.PieceLength)
}

func (f *file) endPieceIndex() int {
	return f.FileInfo.EndPieceIndex(f.Info.PieceLength)
}

func (f *file) length() int64 {
	return f.FileInfo.Length
}

func (f *file) torrentOffset() int64 {
	return f.FileInfo.TorrentOffset
}

type fileExtra struct {
	mu         sync.RWMutex
	safeOsPath string
	race       byte
}

func (f *fileExtra) partFilePath() string {
	return f.safeOsPath + ".part"
}
