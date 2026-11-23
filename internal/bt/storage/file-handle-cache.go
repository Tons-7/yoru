package storage

import (
	"io"
	"os"
)

var sharedFiles sharedFilesInterface = regularFsSharedFiles{}

type regularFsSharedFiles struct{}

func (me regularFsSharedFiles) CloseAll(name string) error {
	return nil
}

func (me regularFsSharedFiles) Open(name string) (sharedFileIf, error) {
	return os.Open(name)
}

type sharedFileIf interface {
	io.ReaderAt
	io.Closer
}

type sharedFilesInterface interface {
	Open(name string) (sharedFileIf, error)
	CloseAll(name string) error
}
