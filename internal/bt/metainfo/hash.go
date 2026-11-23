package metainfo

import (
	"yoru/internal/bt/types/infohash"
)

const HashSize = infohash.Size

type Hash = infohash.T

var (
	NewHashFromHex = infohash.FromHexString
	HashBytes      = infohash.HashBytes
)
