//go:build !noboltdb && !wasm

package storage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// openBolt opens the db, turning a panic on a damaged file into an error.
func openBolt(p string) (db *bbolt.DB, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("opening %s: %v", p, r)
		}
	}()
	return bbolt.Open(p, 0o660, &bbolt.Options{Timeout: time.Second})
}

// damaged: bbolt read the file and found it broken, not that the OS refused it (held, denied, mmap).
func damaged(err error) bool {
	var pathErr *os.PathError
	var errno syscall.Errno
	if errors.Is(err, bolterrors.ErrTimeout) || errors.As(err, &pathErr) || errors.As(err, &errno) ||
		strings.Contains(err.Error(), "mmap") {
		return false
	}
	return true
}

// NewBoltPieceCompletionRecovering swaps a db too damaged to open (NoSync risks a torn write) for a new one
// (reset: every piece is rehashed). One the OS withholds (another process, access) is an error, never moved.
func NewBoltPieceCompletionRecovering(dir string) (ret PieceCompletion, reset bool, err error) {
	ret, err = NewBoltPieceCompletion(dir)
	if err == nil || !damaged(err) {
		return
	}
	p := filepath.Join(dir, ".torrent.bolt.db")
	if renameErr := os.Rename(p, p+".damaged-"+time.Now().Format("20060102-150405")); renameErr != nil {
		return nil, false, fmt.Errorf("%w (and moving it aside: %v)", err, renameErr)
	}
	ret, err = NewBoltPieceCompletion(dir)
	return ret, err == nil, err
}

// Forget drops a torrent's records in one transaction, so the db doesn't keep every torrent ever held.
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
