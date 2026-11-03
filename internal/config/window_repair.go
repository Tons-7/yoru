package config

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const storageAside = "Local Storage.damaged-"

func repairStorage(profile string, log func(msg string, args ...any)) {
	dir := filepath.Join(profile, "Default", "Local Storage")
	opened, err := os.ReadFile(filepath.Join(dir, "leveldb", "LOG"))
	if err != nil || !bytes.Contains(opened, []byte("Corruption")) {
		return
	}
	if runtime.GOOS != "windows" {
		if _, err := os.Lstat(filepath.Join(profile, "SingletonLock")); err == nil {
			return
		}
	}
	aside := filepath.Join(profile, "Default", storageAside+time.Now().Format("2006-01-02-150405"))
	if err := os.Rename(dir, aside); err != nil {
		log("the window's saved page data is damaged and could not be set aside", "err", err)
		return
	}
	older, _ := filepath.Glob(filepath.Join(profile, "Default", storageAside+"*"))
	for _, path := range older {
		if path != aside {
			os.RemoveAll(path)
		}
	}
	log("the window's saved page data was damaged; set aside so the browser starts it afresh", "aside", aside)
}
