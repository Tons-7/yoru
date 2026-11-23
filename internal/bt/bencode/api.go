package bencode

import (
	"bytes"
	"fmt"
	"io"
	"reflect"

	"github.com/anacrolix/missinggo/expect"
)

type MarshalTypeError struct {
	Type reflect.Type
}

func (e *MarshalTypeError) Error() string {
	return "bencode: unsupported type: " + e.Type.String()
}

type UnmarshalInvalidArgError struct {
	Type reflect.Type
}

func (e *UnmarshalInvalidArgError) Error() string {
	if e.Type == nil {
		return "bencode: Unmarshal(nil)"
	}

	if e.Type.Kind() != reflect.Pointer {
		return "bencode: Unmarshal(non-pointer " + e.Type.String() + ")"
	}
	return "bencode: Unmarshal(nil " + e.Type.String() + ")"
}

type UnmarshalTypeError struct {
	BencodeTypeName     string
	UnmarshalTargetType reflect.Type
}

func (e *UnmarshalTypeError) Error() string {
	return fmt.Sprintf(
		"can't unmarshal a bencode %v into a %v",
		e.BencodeTypeName,
		e.UnmarshalTargetType,
	)
}

type SyntaxError struct {
	Offset int64
	What   error
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("bencode: syntax error (offset: %d): %s", e.Offset, e.What)
}

type MarshalerError struct {
	Type reflect.Type
	Err  error
}

func (e *MarshalerError) Error() string {
	return "bencode: error calling MarshalBencode for type " + e.Type.String() + ": " + e.Err.Error()
}

type UnmarshalerError struct {
	Type reflect.Type
	Err  error
}

func (e *UnmarshalerError) Error() string {
	return "bencode: error calling UnmarshalBencode for type " + e.Type.String() + ": " + e.Err.Error()
}

type Marshaler interface {
	MarshalBencode() ([]byte, error)
}

type Unmarshaler interface {
	UnmarshalBencode([]byte) error
}

func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	e := Encoder{w: &buf}
	err := e.Encode(v)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func MustMarshal(v any) []byte {
	b, err := Marshal(v)
	expect.Nil(err)
	return b
}

func Unmarshal(data []byte, v any) (err error) {
	buf := bytes.NewReader(data)
	dec := Decoder{r: buf, MaxStrLen: max(int64(len(data)), 1)}
	err = dec.Decode(v)
	if err != nil {
		return
	}
	if buf.Len() != 0 {
		return ErrUnusedTrailingBytes{buf.Len()}
	}
	return dec.ReadEOF()
}

type ErrUnusedTrailingBytes struct {
	NumUnusedBytes int
}

func (me ErrUnusedTrailingBytes) Error() string {
	return fmt.Sprintf("%d unused trailing bytes", me.NumUnusedBytes)
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: &scanner{r: r}}
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}
