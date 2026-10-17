package krpc

import (
	"yoru/internal/bt/metainfo"
)

type Bep46Payload struct {
	Ih metainfo.Hash `bencode:"ih"`
}
