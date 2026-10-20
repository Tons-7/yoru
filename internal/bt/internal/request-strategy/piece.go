package requestStrategy

type Piece interface {
	// Whether requests should be made for this piece. This would be false for cases like the piece
	// is currently being hashed, or already complete.
	Request() bool
	// Counts against the unverified limit (hashing, queued, metadata changing), so downloading can't run
	// ahead and leave many pieces stuck before completion.
	CountUnverified() bool
	// Whether a connected peer has it now. Read live: the order's cached availability misses peers that
	// announced everything (HaveAll, a full bitfield).
	Available() bool
}
