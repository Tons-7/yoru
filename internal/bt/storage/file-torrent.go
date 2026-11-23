package storage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"

	"github.com/anacrolix/missinggo/v2"
	"github.com/anacrolix/missinggo/v2/panicif"

	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/segments"
)

type fileTorrentImpl struct {
	info              *metainfo.Info
	files             []fileExtra
	metainfoFileInfos []metainfo.FileInfo
	segmentLocater    segments.Index
	infoHash          metainfo.Hash
	io                fileIo
	client            *fileClientImpl
}

func (fts *fileTorrentImpl) logger() *slog.Logger {
	return fts.client.opts.Logger
}

func (fts *fileTorrentImpl) pieceCompletion() PieceCompletion {
	return fts.client.opts.PieceCompletion
}

func (fts *fileTorrentImpl) pieceCompletionKey(p int) metainfo.PieceKey {
	return metainfo.PieceKey{
		InfoHash: fts.infoHash,
		Index:    p,
	}
}

func (fts *fileTorrentImpl) setPieceCompletion(p int, complete bool) error {
	return fts.pieceCompletion().Set(fts.pieceCompletionKey(p), complete)
}

func (fts *fileTorrentImpl) setCompletionFromPartFiles() error {
	notComplete := make([]bool, fts.info.NumPieces())
	for fileIndex := range fts.files {
		f := fts.file(fileIndex)
		fi, err := os.Stat(f.safeOsPath)
		if err == nil {
			if fi.Size() == f.length() {
				continue
			}
			fts.logger().Warn("file has unexpected size", "file", f.safeOsPath, "size", fi.Size(), "expected", f.length())
		} else if !errors.Is(err, fs.ErrNotExist) {
			fts.logger().Warn("error checking file size", "err", err)
		}
		for pieceIndex := f.beginPieceIndex(); pieceIndex < f.endPieceIndex(); pieceIndex++ {
			notComplete[pieceIndex] = true
		}
	}
	for i, nc := range notComplete {
		if nc {
			c := fts.getCompletion(i)
			if c.Complete {
				panicif.Err(fts.setPieceCompletion(i, false))
			}
		} else {
			err := fts.setPieceCompletion(i, true)
			if err != nil {
				return fmt.Errorf("setting piece %v completion: %w", i, err)
			}
		}
	}
	return nil
}

func (fts *fileTorrentImpl) partFiles() bool {
	return fts.client.opts.partFiles()
}

func (fts *fileTorrentImpl) pathForWrite(f *file) string {
	if fts.partFiles() {
		return f.partFilePath()
	}
	return f.safeOsPath
}

func (fts *fileTorrentImpl) getCompletion(piece int) Completion {
	cmpl, err := fts.pieceCompletion().Get(metainfo.PieceKey{InfoHash: fts.infoHash, Index: piece})
	cmpl.Err = errors.Join(cmpl.Err, err)
	return cmpl
}

func (fts *fileTorrentImpl) Piece(p metainfo.Piece) PieceImpl {
	_io := fileTorrentImplIO{fts}
	return &filePieceImpl{
		fts,
		p,
		missinggo.NewSectionWriter(_io, p.Offset(), p.Length()),
		io.NewSectionReader(_io, p.Offset(), p.Length()),
	}
}

func (fs *fileTorrentImpl) Close() error {
	return nil
}

func (fts *fileTorrentImpl) file(index int) file {
	return file{
		Info:      fts.info,
		FileInfo:  &fts.metainfoFileInfos[index],
		fileExtra: &fts.files[index],
	}
}

func (me *fileTorrentImpl) openSharedFile(file file) (f sharedFileIf, err error) {
	file.mu.RLock()
	if me.partFiles() {
		f, err = me.io.openForSharedRead(file.partFilePath())
	}
	if err == nil && f == nil || errors.Is(err, fs.ErrNotExist) {
		f, err = me.io.openForSharedRead(file.safeOsPath)
	}
	file.mu.RUnlock()
	return
}

func (me *fileTorrentImpl) openFile(file file) (f fileReader, err error) {
	file.mu.RLock()
	if me.partFiles() {
		f, err = me.io.openForRead(file.partFilePath())
	}
	if err == nil && f == nil || errors.Is(err, fs.ErrNotExist) {
		f, err = me.io.openForRead(file.safeOsPath)
	}
	file.mu.RUnlock()
	return
}

func (fst *fileTorrentImpl) openForWrite(file file) (_ fileWriter, err error) {
	fst.logger().Debug("openForWrite", "file.safeOsPath", file.safeOsPath)
	return fst.io.openForWrite(fst.pathForWrite(&file), file.FileInfo.Length)
}
