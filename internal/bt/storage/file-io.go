package storage

import (
	"io"
)

type fileWriter interface {
	io.WriterAt
	io.Closer
}

type fileReader interface {
	seekDataOrEof(offset int64) (ret int64, err error)
	writeToN(w io.Writer, n int64) (written int64, err error)
	io.ReadCloser
}

type fileIo interface {
	openForSharedRead(name string) (sharedFileIf, error)
	openForRead(name string) (fileReader, error)
	openForWrite(name string, size int64) (fileWriter, error)
	flush(name string, offset, nbytes int64) error
	rename(from, to string) error
}
