// Package version is how Yoru names itself to peers, trackers and routers.
package version

var (
	// DefaultExtendedHandshakeClientVersion is the client name peers see in the extension handshake.
	DefaultExtendedHandshakeClientVersion = "Yoru"
	// DefaultBep20Prefix starts Yoru's peer IDs; bump it when a change matters to other peers.
	DefaultBep20Prefix = "-YR0001-"
	// DefaultHttpUserAgent is sent to HTTP trackers and web seeds.
	DefaultHttpUserAgent = "Yoru"
	// DefaultUpnpId names Yoru's port mapping in the router's list.
	DefaultUpnpId = "Yoru"
)
