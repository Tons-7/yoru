package storage

import (
	"cmp"
	"iter"
	"os"

	"github.com/anacrolix/log"

	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/types/infohash"
)

type PieceCompletionGetSetter interface {
	Get(metainfo.PieceKey) (Completion, error)
	Set(_ metainfo.PieceKey, complete bool) error
}

type PieceCompletion interface {
	PieceCompletionGetSetter
	Close() error
}

type PieceCompletionPersistenter interface {
	Persistent() bool
}

func pieceCompletionIsPersistent(pc PieceCompletion) bool {
	if p, ok := pc.(PieceCompletionPersistenter); ok {
		return p.Persistent()
	}
	return true
}

type PieceCompletionGetRanger interface {
	GetRange(_ infohash.T, begin, end int) iter.Seq[Completion]
}

func GetPieceCompletionRange(pc PieceCompletion, ih infohash.T, begin, end int) iter.Seq[Completion] {
	if a, ok := pc.(PieceCompletionGetRanger); ok {
		return a.GetRange(ih, begin, end)
	}
	return func(yield func(Completion) bool) {
		for i := begin; i < end; i++ {
			c, err := pc.Get(metainfo.PieceKey{
				InfoHash: ih,
				Index:    i,
			})
			c.Err = cmp.Or(c.Err, err)
			if !yield(c) {
				return
			}
		}
	}
}

func pieceCompletionForDir(dir string) (ret PieceCompletion) {
	os.MkdirAll(dir, 0o700)
	ret, err := NewDefaultPieceCompletionForDir(dir)
	if err != nil {
		log.Levelf(log.Warning, "couldn't open piece completion db in %q: %s", dir, err)
		ret = NewMapPieceCompletion()
	}
	return
}
