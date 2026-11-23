package db

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

const pragmas = "_pragma=journal_mode(WAL)" +
	"&_pragma=busy_timeout(5000)" +
	"&_pragma=synchronous(NORMAL)" +
	"&_pragma=foreign_keys(1)" +
	"&_pragma=temp_store(MEMORY)" +
	"&_pragma=cache_size(-32000)" +
	"&_pragma=journal_size_limit(67108864)"

type DB struct {
	W *sql.DB
	R *sql.DB

	dsn  string
	path string
}

func Open(path string) (*DB, error) {
	dsn := path + "?" + pragmas

	write, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	if err := write.Ping(); err != nil {
		write.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}

	read, err := sql.Open("sqlite", dsn+"&_pragma=query_only(1)")
	if err != nil {
		write.Close()
		return nil, err
	}
	readers := max(4, runtime.NumCPU())
	read.SetMaxOpenConns(readers)
	read.SetMaxIdleConns(readers)
	read.SetConnMaxIdleTime(5 * time.Minute)
	if err := read.Ping(); err != nil {
		write.Close()
		read.Close()
		return nil, fmt.Errorf("open %s for reading: %w", path, err)
	}

	return &DB{W: write, R: read, dsn: dsn, path: path}, nil
}

func (d *DB) Close() error {
	d.W.Exec("PRAGMA optimize")
	return errors.Join(d.R.Close(), d.W.Close())
}

func (d *DB) Migrate() error {
	if err := prepareGoose(); err != nil {
		return err
	}
	migrator, err := d.migrator()
	if err != nil {
		return err
	}
	defer migrator.Close()

	from, err := goose.GetDBVersion(migrator)
	if err != nil {
		return err
	}
	all, err := goose.CollectMigrations("migrations", 0, goose.MaxVersion)
	if err != nil {
		return err
	}
	last, err := all.Last()
	if err != nil {
		return err
	}
	if from >= last.Version {
		d.dropSnapshots("")
		return nil
	}

	snapshot := ""
	if from > 0 && d.path != "" {
		snapshot = fmt.Sprintf("%s.pre-%d.bak", d.path, from)
		d.dropSnapshots(snapshot)
		os.Remove(snapshot)
		if _, err := migrator.Exec(`VACUUM INTO ?`, snapshot); err != nil {
			return fmt.Errorf("copy the database before updating it: %w", err)
		}
	}
	failed := func(err error) error {
		if snapshot != "" {
			return fmt.Errorf("%w (the database before this update is at %s)", err, snapshot)
		}
		return err
	}
	if err := goose.Up(migrator, "migrations"); err != nil {
		return failed(err)
	}
	var table, parent string
	var rowid sql.NullInt64
	var fkid int
	if err := migrator.QueryRow("PRAGMA foreign_key_check").Scan(&table, &rowid, &parent, &fkid); err == nil {
		return failed(fmt.Errorf("migration left a dangling foreign key in %s (row %d) to %s", table, rowid.Int64, parent))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return failed(err)
	}
	return nil
}

func (d *DB) migrator() (*sql.DB, error) {
	m, err := sql.Open("sqlite", d.dsn+"&_pragma=foreign_keys(0)")
	if err != nil {
		return nil, err
	}
	m.SetMaxOpenConns(1)
	return m, nil
}

func (d *DB) dropSnapshots(keep string) {
	if d.path == "" {
		return
	}
	old, _ := filepath.Glob(d.path + ".pre-*.bak")
	for _, f := range old {
		if f != keep {
			os.Remove(f)
		}
	}
}

func (d *DB) MigrateTo(version int64) error {
	if err := prepareGoose(); err != nil {
		return err
	}
	migrator, err := d.migrator()
	if err != nil {
		return err
	}
	defer migrator.Close()
	return goose.UpTo(migrator, "migrations", version)
}

func prepareGoose() error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	return goose.SetDialect("sqlite3")
}
