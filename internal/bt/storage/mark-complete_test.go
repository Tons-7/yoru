package storage_test

import (
	"testing"

	g "github.com/anacrolix/generics"

	"yoru/internal/bt/storage"
	test_storage "yoru/internal/bt/storage/test"
)

func BenchmarkMarkComplete(b *testing.B) {
	bench := func(b *testing.B, ci storage.ClientImpl) {
		test_storage.BenchmarkPieceMarkComplete(
			b, ci, test_storage.DefaultPieceSize, test_storage.DefaultNumPieces, 0)
	}
	b.Run("File", func(b *testing.B) {
		ci := storage.NewFileOpts(storage.NewFileClientOpts{
			ClientBaseDir: b.TempDir(),
			UsePartFiles:  g.Some(false),
		})
		b.Cleanup(func() { ci.Close() })
		bench(b, ci)
	})
}
