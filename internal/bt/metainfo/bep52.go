package metainfo

import (
	"fmt"

	"yoru/internal/bt/merkle"
)

func ValidatePieceLayers(
	pieceLayers map[string]string,
	fileTree *FileTree,
	pieceLength int64,
) (err error) {
	fileTree.Walk(nil, func(path []string, ft *FileTree) {
		if err != nil {
			return
		}
		if ft.IsDir() {
			return
		}
		piecesRoot := ft.PiecesRootAsByteArray()
		if !piecesRoot.Ok {
			return
		}
		filePieceLayers, ok := pieceLayers[string(piecesRoot.Value[:])]
		if !ok {
			// BEP 52: piece layers only for files larger than a piece; a smaller one's pieces root
			// stands in for the layer hash.
			if ft.File.Length > pieceLength {
				err = fmt.Errorf("no piece layers for file %q", path)
			}
			return
		}
		var layerHashes [][32]byte
		layerHashes, err = merkle.CompactLayerToSliceHashes(filePieceLayers)
		root := merkle.RootWithPadHash(layerHashes, HashForPiecePad(pieceLength))
		if root != piecesRoot.Value {
			err = fmt.Errorf("file %q: expected hash %x got %x", path, piecesRoot.Value, root)
			return
		}
	})
	return
}

// Returns the padding hash for the hash layer corresponding to a piece. It can't be zero because
// that's the bottom-most layer (the hashes for the smallest blocks).
func HashForPiecePad(pieceLength int64) (hash [32]byte) {
	// This should be a power of two, and probably checked elsewhere.
	blocksPerPiece := pieceLength / (1 << 14)
	blockHashes := make([][32]byte, blocksPerPiece)
	return merkle.Root(blockHashes)
}
