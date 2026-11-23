package bt

import (
	"yoru/internal/bt/mse"
	pp "yoru/internal/bt/peer_protocol"
)

type Callbacks struct {
	CompletedHandshake           func(*PeerConn, InfoHash)
	ReadMessage                  func(*PeerConn, *pp.Message)
	ReadExtendedHandshake        func(*PeerConn, *pp.ExtendedHandshakeMessage)
	PeerConnClosed               func(*PeerConn)
	PeerConnReadExtensionMessage []func(PeerConnReadExtensionMessageEvent)

	ReceiveEncryptedHandshakeSkeys mse.SecretKeyIter

	ReceivedUsefulData []func(ReceivedUsefulDataEvent)
	ReceivedRequested  []func(PeerMessageEvent)
	DeletedRequest     []func(PeerRequestEvent)
	SentRequest        []func(PeerRequestEvent)
	PeerClosed         []func(*Peer)
	NewPeer            []func(*Peer)
	PeerConnAdded      []func(*PeerConn)

	StatusUpdated []func(StatusUpdatedEvent)
}

type ReceivedUsefulDataEvent = PeerMessageEvent

type PeerMessageEvent struct {
	Peer    *Peer
	Message *pp.Message
}

type PeerRequestEvent struct {
	Peer *Peer
	Request
}

type PeerConnReadExtensionMessageEvent struct {
	PeerConn        *PeerConn
	ExtensionNumber pp.ExtensionNumber
	Payload         []byte
}

type StatusUpdatedEvent struct {
	Event    StatusEvent `json:"event"`
	Error    error       `json:"error"`
	PeerId   PeerID      `json:"peer_id"`
	Url      string      `json:"url"`
	InfoHash string      `json:"info_hash"`
}

type StatusEvent string

const (
	PeerConnected             StatusEvent = "peer_connected"
	PeerDisconnected          StatusEvent = "peer_disconnected"
	TrackerConnected          StatusEvent = "tracker_connected"
	TrackerDisconnected       StatusEvent = "tracker_disconnected"
	TrackerAnnounceSuccessful StatusEvent = "tracker_announce_successful"
	TrackerAnnounceError      StatusEvent = "tracker_announce_error"
)
