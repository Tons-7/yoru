package peer_protocol

import (
	"fmt"
	"unsafe"
)

const (
	Protocol = "\x13BitTorrent protocol"
)

func protocolBytes() []byte {
	return unsafe.Slice(unsafe.StringData(Protocol), len(Protocol))
}

type MessageType byte

//go:generate stringer -type=MessageType

func (mt MessageType) FastExtension() bool {
	return mt >= Suggest && mt <= AllowedFast
}

func (mt *MessageType) UnmarshalBinary(b []byte) error {
	if len(b) != 1 {
		return fmt.Errorf("message type of %d bytes", len(b))
	}
	*mt = MessageType(b[0])
	return nil
}

const (
	Choke         MessageType = 0
	Unchoke       MessageType = 1
	Interested    MessageType = 2
	NotInterested MessageType = 3
	Have          MessageType = 4
	Bitfield      MessageType = 5
	Request       MessageType = 6
	Piece         MessageType = 7
	Cancel        MessageType = 8

	Port MessageType = 9

	Suggest     MessageType = 0x0d
	HaveAll     MessageType = 0x0e
	HaveNone    MessageType = 0x0f
	Reject      MessageType = 0x10
	AllowedFast MessageType = 0x11

	Extended MessageType = 20

	HashRequest MessageType = 21
	Hashes      MessageType = 22
	HashReject  MessageType = 23
)

const (
	HandshakeExtendedID = 0

	RequestMetadataExtensionMsgType ExtendedMetadataRequestMsgType = 0
	DataMetadataExtensionMsgType    ExtendedMetadataRequestMsgType = 1
	RejectMetadataExtensionMsgType  ExtendedMetadataRequestMsgType = 2
)
