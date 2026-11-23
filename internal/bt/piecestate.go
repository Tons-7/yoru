package bt

import (
	"yoru/internal/bt/storage"
)

type PieceState struct {
	storage.Completion
	Priority PiecePriority
	Checking bool

	Hashing       bool
	QueuedForHash bool
	Marking       bool

	Partial bool

	MissingPieceLayerHash bool
}

type PieceStateRun struct {
	PieceState
	Length int
}
