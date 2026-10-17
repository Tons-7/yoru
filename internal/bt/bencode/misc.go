package bencode

import (
	"reflect"
)

// Wow Go is retarded.
var (
	marshalerType   = reflect.TypeFor[Marshaler]()
	unmarshalerType = reflect.TypeFor[Unmarshaler]()
)
