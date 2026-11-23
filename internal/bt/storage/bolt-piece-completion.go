//go:build !noboltdb

package storage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"

	"go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"

	"yoru/internal/bt/metainfo"
)

const (
	boltDbCompleteValue   = "c"
	boltDbIncompleteValue = "i"
)

var completionBucketKey = []byte("completion")

type boltPieceCompletion struct {
	db *bbolt.DB
}

func (me boltPieceCompletion) Persistent() bool {
	return true
}

var _ PieceCompletion = (*boltPieceCompletion)(nil)

func NewBoltPieceCompletion(dir string) (ret PieceCompletion, err error) {
	os.MkdirAll(dir, 0o750)
	p := filepath.Join(dir, ".torrent.bolt.db")
	db, err := openBolt(p)
	if err != nil {
		return
	}
	db.NoSync = true
	ret = &boltPieceCompletion{db}
	return
}

func openBolt(p string) (db *bbolt.DB, err error) {
	var file *os.File
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if r := recover(); r != nil {
			if file != nil {
				file.Close()
			}
			db = nil
			err = fmt.Errorf("opening %s: %v", p, r)
		}
	}()
	db, err = bbolt.Open(p, 0o660, &bbolt.Options{
		Timeout: time.Second,
		OpenFile: func(name string, flag int, perm os.FileMode) (*os.File, error) {
			f, err := os.OpenFile(name, flag, perm)
			file = f
			return f, err
		},
	})
	if err != nil {
		return
	}
	if err = readThrough(db); err != nil {
		closeQuietly(db)
		db = nil
	}
	return
}

func closeQuietly(db *bbolt.DB) {
	defer func() { recover() }()
	db.Close()
}

func readThrough(db *bbolt.DB) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("reading through: %v", r)
		}
	}()
	return db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket(completionBucketKey)
		if c == nil {
			return nil
		}
		return c.ForEachBucket(func(k []byte) error {
			return c.Bucket(k).ForEach(func(_, _ []byte) error { return nil })
		})
	})
}

func damaged(err error) bool {
	var pathErr *os.PathError
	var errno syscall.Errno
	if errors.Is(err, bolterrors.ErrTimeout) || errors.As(err, &pathErr) || errors.As(err, &errno) ||
		strings.Contains(err.Error(), "mmap") {
		return false
	}
	return true
}

func NewBoltPieceCompletionRecovering(dir string) (ret PieceCompletion, reset bool, err error) {
	ret, err = NewBoltPieceCompletion(dir)
	if err == nil || !damaged(err) {
		return
	}
	p := filepath.Join(dir, ".torrent.bolt.db")
	if renameErr := os.Rename(p, p+".damaged-"+time.Now().Format("20060102-150405")); renameErr != nil {
		return nil, false, fmt.Errorf("%w (and moving it aside: %v)", err, renameErr)
	}
	pruneDamaged(p, 2)
	ret, err = NewBoltPieceCompletion(dir)
	return ret, err == nil, err
}

func pruneDamaged(p string, keep int) {
	old, _ := filepath.Glob(p + ".damaged-*")
	slices.Sort(old)
	for _, f := range old[:max(0, len(old)-keep)] {
		os.Remove(f)
	}
}

func (me boltPieceCompletion) Forget(ih metainfo.Hash) error {
	return me.db.Update(func(tx *bbolt.Tx) error {
		c := tx.Bucket(completionBucketKey)
		if c == nil || c.Bucket(ih[:]) == nil {
			return nil
		}
		return c.DeleteBucket(ih[:])
	})
}

func (me boltPieceCompletion) Get(pk metainfo.PieceKey) (cn Completion, err error) {
	err = me.db.View(func(tx *bbolt.Tx) error {
		cb := tx.Bucket(completionBucketKey)
		if cb == nil {
			return nil
		}
		ih := cb.Bucket(pk.InfoHash[:])
		if ih == nil {
			return nil
		}
		var key [4]byte
		binary.BigEndian.PutUint32(key[:], uint32(pk.Index))
		cn.Ok = true
		switch string(ih.Get(key[:])) {
		case boltDbCompleteValue:
			cn.Complete = true
		case boltDbIncompleteValue:
			cn.Complete = false
		default:
			cn.Ok = false
		}
		return nil
	})
	return
}

func (me boltPieceCompletion) Set(pk metainfo.PieceKey, b bool) error {
	if c, err := me.Get(pk); err == nil && c.Ok && c.Complete == b {
		return nil
	}
	return me.db.Update(func(tx *bbolt.Tx) error {
		c, err := tx.CreateBucketIfNotExists(completionBucketKey)
		if err != nil {
			return err
		}
		ih, err := c.CreateBucketIfNotExists(pk.InfoHash[:])
		if err != nil {
			return err
		}
		var key [4]byte
		binary.BigEndian.PutUint32(key[:], uint32(pk.Index))
		return ih.Put(key[:], []byte(func() string {
			if b {
				return boltDbCompleteValue
			} else {
				return boltDbIncompleteValue
			}
		}()))
	})
}

func (me *boltPieceCompletion) Close() error {
	return me.db.Close()
}
