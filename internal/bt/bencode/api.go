package bencode

import (
	"bytes"
	"fmt"
	"io"
	"reflect"

	"github.com/anacrolix/missinggo/expect"
)

// MarshalTypeError is a type bencode can't represent, such as float32/float64.
type MarshalTypeError struct {
	Type reflect.Type
}

func (e *MarshalTypeError) Error() string {
	return "bencode: unsupported type: " + e.Type.String()
}

// Unmarshal argument must be a non-nil value of some pointer type.
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

// Unmarshaler spotted a value that was not appropriate for a given Go value.
type UnmarshalTypeError struct {
	BencodeTypeName     string
	UnmarshalTargetType reflect.Type
}

// A pointer receiver, as callers may already assume one.
func (e *UnmarshalTypeError) Error() string {
	return fmt.Sprintf(
		"can't unmarshal a bencode %v into a %v",
		e.BencodeTypeName,
		e.UnmarshalTargetType,
	)
}

// Unmarshaler tried to write to an unexported (therefore unwritable) field.
type UnmarshalFieldError struct {
	Key   string
	Type  reflect.Type
	Field reflect.StructField
}

func (e *UnmarshalFieldError) Error() string {
	return "bencode: key \"" + e.Key + "\" led to an unexported field \"" +
		e.Field.Name + "\" in type: " + e.Type.String()
}

// Malformed bencode input, unmarshaler failed to parse it.
type SyntaxError struct {
	Offset int64 // location of the error
	What   error // error description
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("bencode: syntax error (offset: %d): %s", e.Offset, e.What)
}

// MarshalerError is the error a type's MarshalBencode returned.
type MarshalerError struct {
	Type reflect.Type
	Err  error
}

func (e *MarshalerError) Error() string {
	return "bencode: error calling MarshalBencode for type " + e.Type.String() + ": " + e.Err.Error()
}

// UnmarshalerError is the error a type's UnmarshalBencode returned.
type UnmarshalerError struct {
	Type reflect.Type
	Err  error
}

func (e *UnmarshalerError) Error() string {
	return "bencode: error calling UnmarshalBencode for type " + e.Type.String() + ": " + e.Err.Error()
}

// A Marshaler encodes itself with MarshalBencode.
type Marshaler interface {
	MarshalBencode() ([]byte, error)
}

// An Unmarshaler decodes itself with UnmarshalBencode.
type Unmarshaler interface {
	UnmarshalBencode([]byte) error
}

// Marshal returns v's bencode form.
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

// Unmarshal decodes data into the value v points to. Trailing bytes give ErrUnusedTrailingBytes with v still
// set; Decoder.Decode is the steadier way to rely on that.
func Unmarshal(data []byte, v any) (err error) {
	buf := bytes.NewReader(data)
	dec := Decoder{r: buf}
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
