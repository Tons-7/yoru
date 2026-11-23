package storage

import (
	"errors"
	"io"
	"io/fs"

	"github.com/anacrolix/missinggo/v2/panicif"

	"yoru/internal/bt/segments"
)

type fileTorrentImplIO struct {
	fts *fileTorrentImpl
}

func (fst fileTorrentImplIO) readFileAt(file file, b []byte, off int64) (n int, err error) {
	fst.fts.logger().Debug("readFileAt", "file.safeOsPath", file.safeOsPath)
	f, err := fst.fts.openSharedFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		err = io.EOF
		return
	}
	if err != nil {
		return
	}
	defer f.Close()
	if int64(len(b)) > file.length()-off {
		b = b[:file.length()-off]
	}
	for off < file.length() && len(b) != 0 {
		n1, err1 := f.ReadAt(b, off)
		b = b[n1:]
		n += n1
		off += int64(n1)
		if n1 == 0 {
			err = err1
			break
		}
	}
	return
}

func (fst fileTorrentImplIO) ReadAt(b []byte, off int64) (n int, err error) {
	for i, e := range fst.fts.segmentLocater.LocateIter(
		segments.Extent{Start: off, Length: int64(len(b))},
	) {
		n1, err1 := fst.readFileAt(fst.fts.file(i), b[:e.Length], e.Start)
		n += n1
		b = b[n1:]
		if segments.Int(n1) == e.Length {
			switch err1 {
			case nil, io.EOF:
			default:
				err = err1
				return
			}
		} else {
			panicif.Nil(err1)
			err = err1
			return
		}
	}
	if len(b) != 0 {
		err = io.EOF
	}
	return
}

func (fst fileTorrentImplIO) WriteAt(p []byte, off int64) (n int, err error) {
	for i, e := range fst.fts.segmentLocater.LocateIter(
		segments.Extent{Start: off, Length: int64(len(p))},
	) {
		var f fileWriter
		f, err = fst.fts.openForWrite(fst.fts.file(i))
		if err != nil {
			return
		}
		var n1 int
		n1, err = f.WriteAt(p[:e.Length], e.Start)
		closeErr := f.Close()
		n += n1
		p = p[n1:]
		if err == nil {
			err = closeErr
		}
		if err == nil && int64(n1) != e.Length {
			err = io.ErrShortWrite
		}
		if err != nil {
			return
		}
	}
	return
}
