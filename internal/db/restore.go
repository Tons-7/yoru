package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/pressly/goose/v3"
)

var ErrNotBackup = errors.New("this file is not a Yoru backup")

func RestorePath(dbPath string) string { return dbPath + ".restore" }

func CheckBackup(path string) error {
	conn, err := sql.Open("sqlite", path+"?_pragma=query_only(1)")
	if err != nil {
		return ErrNotBackup
	}
	defer conn.Close()
	var result string
	if err := conn.QueryRow(`PRAGMA quick_check`).Scan(&result); err != nil || result != "ok" {
		return ErrNotBackup
	}
	var version int64
	if err := conn.QueryRow(`SELECT coalesce(max(version_id), 0) FROM goose_db_version WHERE is_applied = 1`).Scan(&version); err != nil || version == 0 {
		return ErrNotBackup
	}
	goose.SetBaseFS(migrations)
	all, err := goose.CollectMigrations("migrations", 0, goose.MaxVersion)
	if err != nil {
		return err
	}
	last, err := all.Last()
	if err != nil {
		return err
	}
	if version > last.Version {
		return fmt.Errorf("this backup comes from a newer Yoru; update first")
	}
	var anime int
	if err := conn.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'anime'`).Scan(&anime); err != nil || anime == 0 {
		return ErrNotBackup
	}
	return nil
}

func ApplyPendingRestore(dbPath string) (bool, error) {
	pending := RestorePath(dbPath)
	if _, err := os.Stat(pending); err != nil {
		return false, nil
	}
	if err := CheckBackup(pending); err != nil {
		os.Remove(pending)
		return false, err
	}
	before := dbPath + ".before-restore"
	os.Remove(before)
	if _, err := os.Stat(dbPath); err == nil {
		if err := os.Rename(dbPath, before); err != nil {
			return false, err
		}
	}
	for _, side := range []string{"-wal", "-shm"} {
		os.Remove(dbPath + side)
	}
	if err := os.Rename(pending, dbPath); err != nil {
		os.Rename(before, dbPath)
		return false, err
	}
	return true, nil
}
