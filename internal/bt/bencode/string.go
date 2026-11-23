package bencode

import "unsafe"

func bytesAsString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
