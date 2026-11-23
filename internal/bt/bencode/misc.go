package bencode

import (
	"reflect"
)

var (
	marshalerType   = reflect.TypeFor[Marshaler]()
	unmarshalerType = reflect.TypeFor[Unmarshaler]()
)
