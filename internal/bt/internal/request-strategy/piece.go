package requestStrategy

type Piece interface {
	Request() bool
	CountUnverified() bool
	Available() bool
}
