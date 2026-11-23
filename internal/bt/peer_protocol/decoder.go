package peer_protocol

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	g "github.com/anacrolix/generics"
)

type Decoder struct {
	R         *bufio.Reader
	Pool      *sync.Pool
	MaxLength Integer
}

type decodeReader struct {
	lr io.LimitedReader
	br *bufio.Reader
}

func (dr *decodeReader) Init(r *bufio.Reader, length int64) {
	dr.lr.R = r
	dr.lr.N = length
	dr.br = r
}

func (dr *decodeReader) ReadByte() (c byte, err error) {
	if dr.lr.N <= 0 {
		err = io.EOF
		return
	}
	c, err = dr.br.ReadByte()
	if err == nil {
		dr.lr.N--
	}
	return
}

func (dr *decodeReader) Read(p []byte) (n int, err error) {
	n, err = dr.lr.Read(p)
	if dr.lr.N != 0 && err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return
}

func (dr *decodeReader) UnreadLength() int64 {
	return dr.lr.N
}

type expectReader struct {
	dr *decodeReader
}

func (er expectReader) ReadByte() (c byte, err error) {
	c, err = er.dr.ReadByte()
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return
}

func (er expectReader) Read(p []byte) (n int, err error) {
	n, err = er.dr.Read(p)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return
}

func (er expectReader) UnreadLength() int64 {
	return er.dr.UnreadLength()
}

func (d *Decoder) Decode(msg *Message) (err error) {
	var dr decodeReader
	{
		var length Integer
		err = length.Read(d.R)
		if err != nil {
			return fmt.Errorf("reading message length: %w", err)
		}
		if length > d.MaxLength {
			return errors.New("message too long")
		}
		if length == 0 {
			msg.Keepalive = true
			return
		}
		dr.Init(d.R, int64(length))
	}
	r := expectReader{&dr}
	c, err := r.ReadByte()
	if err != nil {
		return
	}
	msg.Type = MessageType(c)
	err = readMessageAfterType(msg, &r, d.Pool)
	if err != nil {
		err = fmt.Errorf("reading fields for message type %v: %w", msg.Type, err)
		return
	}
	if r.UnreadLength() != 0 {
		err = fmt.Errorf("%v unused bytes in message type %v", r.UnreadLength(), msg.Type)
	}
	return
}

func readMessageAfterType(msg *Message, r *expectReader, piecePool *sync.Pool) (err error) {
	switch msg.Type {
	case Choke, Unchoke, Interested, NotInterested, HaveAll, HaveNone:
	case Have, AllowedFast, Suggest:
		err = msg.Index.Read(r)
	case Request, Cancel, Reject:
		for _, data := range []*Integer{&msg.Index, &msg.Begin, &msg.Length} {
			err = data.Read(r)
			if err != nil {
				break
			}
		}
	case Bitfield:
		b := make([]byte, r.UnreadLength())
		_, err = io.ReadFull(r, b)
		msg.Bitfield = unmarshalBitfield(b)
	case Piece:
		for _, pi := range []*Integer{&msg.Index, &msg.Begin} {
			err = pi.Read(r)
			if err != nil {
				return
			}
		}
		dataLen := r.UnreadLength()
		if piecePool == nil {
			msg.Piece = make([]byte, dataLen)
		} else {
			buf := piecePool.Get().(*[]byte)
			if int64(cap(*buf)) < dataLen {
				piecePool.Put(buf)
				return errors.New("piece data longer than expected")
			}
			msg.Piece = (*buf)[:dataLen]
		}
		_, err = io.ReadFull(r, msg.Piece)
	case Extended:
		var b byte
		b, err = r.ReadByte()
		if err != nil {
			break
		}
		msg.ExtendedID = ExtensionNumber(b)
		msg.ExtendedPayload = make([]byte, r.UnreadLength())
		_, err = io.ReadFull(r, msg.ExtendedPayload)
	case Port:
		err = binary.Read(r, binary.BigEndian, &msg.Port)
	case HashRequest, HashReject:
		err = readHashRequest(r, msg)
	case Hashes:
		err = readHashRequest(r, msg)
		numHashes := (r.UnreadLength() + 31) / 32
		g.MakeSliceWithCap(&msg.Hashes, numHashes)
		for range numHashes {
			var oneHash [32]byte
			_, err = io.ReadFull(r, oneHash[:])
			if err != nil {
				err = fmt.Errorf("error while reading hashes: %w", err)
				return
			}
			msg.Hashes = append(msg.Hashes, oneHash)
		}
	default:
		err = errors.New("unhandled message type")
	}
	return
}

func readHashRequest(r io.Reader, msg *Message) (err error) {
	_, err = io.ReadFull(r, msg.PiecesRoot[:])
	if err != nil {
		return
	}
	return readSeq(r, &msg.BaseLayer, &msg.Index, &msg.Length, &msg.ProofLayers)
}

func readSeq(r io.Reader, data ...any) (err error) {
	for _, d := range data {
		err = binary.Read(r, binary.BigEndian, d)
		if err != nil {
			return
		}
	}
	return
}

func unmarshalBitfield(b []byte) (bf []bool) {
	bf = make([]bool, 0, 8*len(b))
	for _, c := range b {
		for i := 7; i >= 0; i-- {
			bf = append(bf, (c>>uint(i))&1 == 1)
		}
	}
	return
}
