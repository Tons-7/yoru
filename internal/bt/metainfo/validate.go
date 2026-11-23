package metainfo

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
)

const (
	minV2PieceLength = 16 << 10
	maxPieceLength   = 256 << 20
	maxPieces        = 1 << 22
)

func (info *Info) Validate() error {
	if info.PieceLength < 0 || info.PieceLength > maxPieceLength {
		return fmt.Errorf("piece length %d is out of range", info.PieceLength)
	}
	if len(info.Pieces)%20 != 0 {
		return errors.New("pieces has invalid length")
	}
	if info.HasV1() {
		if err := info.validateV1Lengths(); err != nil {
			return err
		}
	}
	if !info.HasV2() {
		return nil
	}
	if info.PieceLength < minV2PieceLength || bits.OnesCount64(uint64(info.PieceLength)) != 1 {
		return fmt.Errorf("v2 piece length %d is not a power of two of at least 16 KiB", info.PieceLength)
	}
	pieces, err := info.FileTree.validate(info.PieceLength)
	if err != nil {
		return err
	}
	if len(info.Pieces) != 0 && int64(len(info.Pieces)/20) != pieces {
		return fmt.Errorf("hybrid info has %d v1 pieces for %d v2 pieces", len(info.Pieces)/20, pieces)
	}
	return nil
}

func (info *Info) validateV1Lengths() error {
	if info.Length < 0 {
		return errors.New("negative length")
	}
	var total int64
	for _, f := range info.Files {
		if f.Length < 0 {
			return errors.New("negative file length")
		}
		if total > math.MaxInt64-f.Length {
			return errors.New("file lengths overflow")
		}
		total += f.Length
	}
	return nil
}

func (ft *FileTree) validate(pieceLength int64) (pieces int64, err error) {
	var walk func(ft *FileTree) error
	walk = func(ft *FileTree) error {
		if ft.IsDir() {
			for key, sub := range ft.Dir {
				if key == FileTreePropertiesKey {
					continue
				}
				if err := walk(&sub); err != nil {
					return err
				}
			}
			return nil
		}
		n := ft.File.Length
		if n < 0 || n > math.MaxInt64-pieceLength {
			return fmt.Errorf("file length %d is out of range", n)
		}
		if root := len(ft.File.PiecesRoot); root != 32 && (n > 0 || root != 0) {
			return fmt.Errorf("pieces root of %d bytes", root)
		}
		pieces += (n + pieceLength - 1) / pieceLength
		if pieces > maxPieces {
			return fmt.Errorf("more than %d pieces", maxPieces)
		}
		return nil
	}
	err = walk(ft)
	return
}
