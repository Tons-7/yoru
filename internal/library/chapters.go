package library

import (
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const chapterFileAge = 7 * 24 * time.Hour

var chapterFileName = regexp.MustCompile(`^chapters-\d+-\d+\.ini$`)

func pruneChapterFiles(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !chapterFileName.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > chapterFileAge {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}
