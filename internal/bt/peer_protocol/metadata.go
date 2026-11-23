package peer_protocol

import (
	"yoru/internal/bt/bencode"
)

const (
	ExtensionNameMetadata = "ut_metadata"
)

type (
	ExtendedMetadataRequestMsg struct {
		Piece     int                            `bencode:"piece"`
		TotalSize int                            `bencode:"total_size"`
		Type      ExtendedMetadataRequestMsgType `bencode:"msg_type"`
	}

	ExtendedMetadataRequestMsgType int
)

func MetadataExtensionRequestMsg(peerMetadataExtensionId ExtensionNumber, piece int) Message {
	return Message{
		Type:       Extended,
		ExtendedID: peerMetadataExtensionId,
		ExtendedPayload: bencode.MustMarshal(ExtendedMetadataRequestMsg{
			Piece: piece,
			Type:  RequestMetadataExtensionMsgType,
		}),
	}
}

func (me ExtendedMetadataRequestMsg) PieceSize() int {
	ret := min(me.TotalSize-me.Piece*(1<<14), 1<<14)
	return ret
}
