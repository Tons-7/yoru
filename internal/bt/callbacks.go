package bt

import (
	"yoru/internal/bt/mse"
	pp "yoru/internal/bt/peer_protocol"
)

// Callbacks run synchronously, maybe under the Client's and other locks; their arguments aren't theirs to
// keep. Nil ones are skipped.
type Callbacks struct {
	// After a peer connection's BitTorrent handshake, without the Client lock.
	CompletedHandshake func(*PeerConn, InfoHash)
	ReadMessage        func(*PeerConn, *pp.Message)
	// This can be folded into the general case below.
	ReadExtendedHandshake func(*PeerConn, *pp.ExtendedHandshakeMessage)
	PeerConnClosed        func(*PeerConn)
	// A BEP 10 message, to each handler in order.
	PeerConnReadExtensionMessage []func(PeerConnReadExtensionMessageEvent)

	// Provides secret keys to be tried against incoming encrypted connections.
	ReceiveEncryptedHandshakeSkeys mse.SecretKeyIter

	ReceivedUsefulData []func(ReceivedUsefulDataEvent)
	ReceivedRequested  []func(PeerMessageEvent)
	DeletedRequest     []func(PeerRequestEvent)
	SentRequest        []func(PeerRequestEvent)
	PeerClosed         []func(*Peer)
	NewPeer            []func(*Peer)
	// A PeerConn joined a Torrent, handshaken but before the extended handshake: the time to change
	// the extensions offered.
	PeerConnAdded []func(*PeerConn)

	// Sends status event updates. Useful to inform the user of specific events as they happen,
	// for logging or to action on.
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
	PeerConn *PeerConn
	// You can look up what protocol this corresponds to using the PeerConn.LocalLtepProtocolMap.
	ExtensionNumber pp.ExtensionNumber
	Payload         []byte
}

type StatusUpdatedEvent struct {
	Event StatusEvent `json:"event"`
	Error error       `json:"error"`
	// The following fields may or may not be populated depending on the event.
	PeerId   PeerID `json:"peer_id"`
	Url      string `json:"url"`
	InfoHash string `json:"info_hash"`
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
