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
	GreetingFileContents = "hello,\x00world\n"
	GreetingFileName     = "greeting"
)

func CreateDummyTorrentData(dirName string) string {
	f, err := os.Create(filepath.Join(dirName, "greeting"))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	f.WriteString(GreetingFileContents)
	return f.Name()
}

func GreetingMetaInfo() *metainfo.MetaInfo {
	mi, _ := Greeting.Generate(5)
	return &mi
}

func GreetingTestTorrent() (tempDir string, metaInfo *metainfo.MetaInfo) {
	tempDir, err := os.MkdirTemp(os.TempDir(), "")
	if err != nil {
		panic(err)
	}
	CreateDummyTorrentData(tempDir)
	metaInfo = GreetingMetaInfo()
	return
}
