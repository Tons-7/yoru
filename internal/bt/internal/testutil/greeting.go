// Package testutil helps test torrents: "greeting" is a one-file torrent holding "hello, world\n".

package testutil

import (
	"os"
	"path/filepath"

	"yoru/internal/bt/metainfo"
)

var Greeting = Torrent{
	Files: []File{{
		Data: GreetingFileContents,
	}},
	Name: GreetingFileName,
}

const (
	// A null in the middle triggers an error if SQLite stores data as text instead of blob.
	GreetingFileContents = "hello,\x00world\n"
	GreetingFileName     = "greeting"
)

func CreateDummyTorrentData(dirName string) string {
	f, _ := os.Create(filepath.Join(dirName, "greeting"))
	defer f.Close()
	f.WriteString(GreetingFileContents)
	return f.Name()
}

func GreetingMetaInfo() *metainfo.MetaInfo {
	mi, _ := Greeting.Generate(5)
	return &mi
}

// A temporary directory holding the complete "greeting" torrent (os.RemoveAll it after), and its metainfo.
func GreetingTestTorrent() (tempDir string, metaInfo *metainfo.MetaInfo) {
	tempDir, err := os.MkdirTemp(os.TempDir(), "")
	if err != nil {
		panic(err)
	}
	CreateDummyTorrentData(tempDir)
	metaInfo = GreetingMetaInfo()
	return
}
