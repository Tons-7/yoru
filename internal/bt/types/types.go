package types

import (
	"fmt"

	pp "yoru/internal/bt/peer_protocol"
)

type PieceIndex = int

type ChunkSpec struct {
	Begin, Length pp.Integer
}

type Request struct {
	Index pp.Integer
	ChunkSpec
}

func (r Request) String() string {
	return fmt.Sprintf("piece %v, %v bytes at %v", r.Index, r.Length, r.Begin)
}

func (r Request) ToMsg(mt pp.MessageType) pp.Message {
	return pp.Message{
		Type:   mt,
		Index:  r.Index,
		Begin:  r.Begin,
		Length: r.Length,
	}
}

type PiecePriority byte

func (pp *PiecePriority) Raise(maybe PiecePriority) bool {
	if maybe > *pp {
		*pp = maybe
		return true
	}
	return false
}

const (
	PiecePriorityNone PiecePriority = iota
	PiecePriorityNormal
	PiecePriorityHigh
	PiecePriorityReadahead
	PiecePriorityNext
	PiecePriorityNow
)
