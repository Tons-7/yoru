package merkle

import (
	"crypto/sha256"
	"hash"
	"unsafe"
)

func NewHash() *Hash {
	h := &Hash{
		nextBlock: sha256.New(),
	}
	return h
}

type Hash struct {
	blocks           [][32]byte
	nextBlock        hash.Hash
	nextBlockWritten int
}

func (h *Hash) remaining() int {
	return BlockSize - h.nextBlockWritten
}

func (h *Hash) Write(p []byte) (n int, err error) {
	for len(p) > 0 {
		var n1 int
		n1, err = h.nextBlock.Write(p[:min(len(p), h.remaining())])
		n += n1
		h.nextBlockWritten += n1
		p = p[n1:]
		if h.remaining() == 0 {
			h.blocks = append(h.blocks, h.nextBlockSum())
			h.nextBlock.Reset()
			h.nextBlockWritten = 0
		}
		if err != nil {
			break
		}
	}
	return
}

func (h *Hash) nextBlockSum() (sum [32]byte) {
	if unsafe.SliceData(h.nextBlock.Sum(sum[:0])) != unsafe.SliceData(sum[:]) {
		panic("go sux")
	}
	return
}

func (h *Hash) curBlocks() [][32]byte {
	blocks := h.blocks
	if h.nextBlockWritten != 0 {
		blocks = append(blocks, h.nextBlockSum())
	}
	return blocks
}

func (h *Hash) Sum(b []byte) []byte {
	sum := RootWithPadHash(h.curBlocks(), [32]byte{})
	return append(b, sum[:]...)
}

func (h *Hash) SumMinLength(b []byte, length int) []byte {
	blocks := h.curBlocks()
	minBlocks := (length + BlockSize - 1) / BlockSize
	blocks = append(blocks, make([][32]byte, max(0, minBlocks-len(blocks)))...)
	sum := RootWithPadHash(blocks, [32]byte{})
	return append(b, sum[:]...)
}

func (h *Hash) Reset() {
	h.blocks = h.blocks[:0]
	h.nextBlock.Reset()
	h.nextBlockWritten = 0
}

func (h *Hash) Size() int {
	return 32
}

func (h *Hash) BlockSize() int {
	return h.nextBlock.BlockSize()
}

var _ hash.Hash = (*Hash)(nil)
