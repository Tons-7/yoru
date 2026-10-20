package krpc

import (
	"yoru/internal/bt/bencode"
)

// Msg is a KRPC message, a bencoded dictionary nodes send each other over UDP: a query, response or error
// ("y": q, r or e), and "t", the querier's transaction ID (2 bytes cover 2^16 queries) echoed in the reply.
type Msg struct {
	Q        string   `bencode:"q,omitempty"` // Query method (one of 4: "ping", "find_node", "get_peers", "announce_peer")
	A        *MsgArgs `bencode:"a,omitempty"` // named arguments sent with a query
	T        string   `bencode:"t"`           // required: transaction ID
	Y        string   `bencode:"y"`           // required: type of the message: q for QUERY, r for RESPONSE, e for ERROR
	R        *Return  `bencode:"r,omitempty"` // RESPONSE type only
	E        *Error   `bencode:"e,omitempty"` // ERROR type only
	IP       NodeAddr `bencode:"ip,omitempty"`
	ReadOnly bool     `bencode:"ro,omitempty"` // BEP 43. Sender does not respond to queries.
	// https://www.libtorrent.org/dht_extensions.html
	ClientId string `bencode:"v,omitempty"`
}

const (
	YQuery    = "q"
	YResponse = "r"
	YError    = "e"
)

type MsgArgs struct {
	ID       ID `bencode:"id"`                  // ID of the querying Node
	InfoHash ID `bencode:"info_hash,omitempty"` // InfoHash of the torrent
	Target   ID `bencode:"target,omitempty"`    // ID of the node sought
	// Token received from an earlier get_peers query. Also used in a BEP 44 put.
	Token       string `bencode:"token,omitempty"`
	Port        *int   `bencode:"port,omitempty"`         // Sender's torrent port
	ImpliedPort bool   `bencode:"implied_port,omitempty"` // Use senders apparent DHT port
	Want        []Want `bencode:"want,omitempty"`         // Contains strings like "n4" and "n6" from BEP 32.
	NoSeed      int    `bencode:"noseed,omitempty"`       // BEP 33
	Scrape      int    `bencode:"scrape,omitempty"`       // BEP 33

	// BEP 44

	// Not bencode.Bytes: a value that marshals back differently fails its hash, which may also stop abuse.
	V any `bencode:"v,omitempty"`
	// A pointer, to tell unset from zero.
	Seq  *int64   `bencode:"seq,omitempty"`
	Cas  int64    `bencode:"cas,omitempty"`
	K    [32]byte `bencode:"k,omitempty"`
	Salt []byte   `bencode:"salt,omitempty"`
	Sig  [64]byte `bencode:"sig,omitempty"`
}

type Want string

const (
	WantNodes  Want = "n4"
	WantNodes6 Want = "n6"
)

// BEP 51 (DHT Infohash Indexing)
type Bep51Return struct {
	Interval *int64 `bencode:"interval,omitempty"`
	Num      *int64 `bencode:"num,omitempty"`
	// Always sent by nodes that support it, empty or not, so indexers can tell them apart.
	Samples *CompactInfohashes `bencode:"samples,omitempty"`
}

type Bep44Return struct {
	V   bencode.Bytes `bencode:"v,omitempty"`
	K   [32]byte      `bencode:"k,omitempty"`
	Sig [64]byte      `bencode:"sig,omitempty"`
	Seq *int64        `bencode:"seq,omitempty"`
}

type Return struct {
	// All returns are supposed to contain an ID, but what if they don't?
	ID ID `bencode:"id"` // ID of the queried (and responding) node

	// K closest nodes to the requested target. Included in responses to queries that imply
	// traversal, for example get_peers, find_nodes, get, sample_infohashes.
	Nodes  CompactIPv4NodeInfo `bencode:"nodes,omitempty"`
	Nodes6 CompactIPv6NodeInfo `bencode:"nodes6,omitempty"`

	Token  *string    `bencode:"token,omitempty"`  // Token for future announce_peer or put (BEP 44)
	Values []NodeAddr `bencode:"values,omitempty"` // Torrent peers

	// BEP 33 (scrapes)
	BFsd *ScrapeBloomFilter `bencode:"BFsd,omitempty"`
	BFpe *ScrapeBloomFilter `bencode:"BFpe,omitempty"`

	Bep51Return

	// BEP 44
	Bep44Return
}

func (r Return) ForAllNodes(f func(NodeInfo)) {
	for _, n := range r.Nodes {
		f(n)
	}
	for _, n := range r.Nodes6 {
		f(n)
	}
}

// SenderID is the sending node's ID, nil if absent. TODO: checked Msgs could guarantee one.
func (m Msg) SenderID() *ID {
	switch m.Y {
	case "q":
		if m.A == nil {
			return nil
		}
		return &m.A.ID
	case "r":
		if m.R == nil {
			return nil
		}
		return &m.R.ID
	}
	return nil
}

// This does not return an error, but (*Error)(nil) is still a non-nil error. You have been warned!
// This language is evil.
func (m Msg) Error() *Error {
	if m.Y != "e" {
		return nil
	}
	return m.E
}
