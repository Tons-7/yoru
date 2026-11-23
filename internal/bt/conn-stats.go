package bt

import (
	"io"

	pp "yoru/internal/bt/peer_protocol"
)

type ConnStats struct {
	BytesWritten     Count
	BytesWrittenData Count

	BytesRead                   Count
	BytesReadData               Count
	BytesReadUsefulData         Count
	BytesReadUsefulIntendedData Count

	ChunksWritten Count

	ChunksRead       Count
	ChunksReadUseful Count
	ChunksReadWasted Count

	MetadataChunksRead Count

	PiecesDirtiedGood Count
	PiecesDirtiedBad  Count
}

func (me *ConnStats) Copy() (ret ConnStats) {
	return copyCountFields(me)
}

func (cs *ConnStats) wroteMsg(msg *pp.Message) {
	switch msg.Type {
	case pp.Piece:
		cs.ChunksWritten.Add(1)
		cs.BytesWrittenData.Add(int64(len(msg.Piece)))
	}
}

func (cs *ConnStats) receivedChunk(size int64) {
	cs.ChunksRead.Add(1)
	cs.BytesReadData.Add(size)
}

func (cs *ConnStats) incrementPiecesDirtiedGood() bool {
	cs.PiecesDirtiedGood.Add(1)
	return true
}

func (cs *ConnStats) incrementPiecesDirtiedBad() bool {
	cs.PiecesDirtiedBad.Add(1)
	return true
}

func add(n int64, f func(*ConnStats) *Count) func(*ConnStats) {
	return func(cs *ConnStats) {
		p := f(cs)
		p.Add(n)
	}
}

type connStatsReadWriter struct {
	rw io.ReadWriter
	c  *PeerConn
}

func (me connStatsReadWriter) Write(b []byte) (n int, err error) {
	n, err = me.rw.Write(b)
	me.c.wroteBytes(int64(n))
	return
}

func (me connStatsReadWriter) Read(b []byte) (n int, err error) {
	n, err = me.rw.Read(b)
	me.c.readBytes(int64(n))
	return
}
