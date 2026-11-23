package krpc

import (
	"yoru/internal/bt/bencode"
)

type Msg struct {
	Q        string   `bencode:"q,omitempty"`
	A        *MsgArgs `bencode:"a,omitempty"`
	T        string   `bencode:"t"`
	Y        string   `bencode:"y"`
	R        *Return  `bencode:"r,omitempty"`
	E        *Error   `bencode:"e,omitempty"`
	IP       NodeAddr `bencode:"ip,omitempty"`
	ReadOnly bool     `bencode:"ro,omitempty"`
	ClientId string   `bencode:"v,omitempty"`
}

const (
	YQuery    = "q"
	YResponse = "r"
	YError    = "e"
)

type MsgArgs struct {
	ID          ID     `bencode:"id"`
	InfoHash    ID     `bencode:"info_hash,omitempty"`
	Target      ID     `bencode:"target,omitempty"`
	Token       string `bencode:"token,omitempty"`
	Port        *int   `bencode:"port,omitempty"`
	ImpliedPort bool   `bencode:"implied_port,omitempty"`
	Want        []Want `bencode:"want,omitempty"`
	NoSeed      int    `bencode:"noseed,omitempty"`
	Scrape      int    `bencode:"scrape,omitempty"`

	V    any      `bencode:"v,omitempty"`
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

type Bep51Return struct {
	Interval *int64             `bencode:"interval,omitempty"`
	Num      *int64             `bencode:"num,omitempty"`
	Samples  *CompactInfohashes `bencode:"samples,omitempty"`
}

type Bep44Return struct {
	V   bencode.Bytes `bencode:"v,omitempty"`
	K   [32]byte      `bencode:"k,omitempty"`
	Sig [64]byte      `bencode:"sig,omitempty"`
	Seq *int64        `bencode:"seq,omitempty"`
}

type Return struct {
	ID ID `bencode:"id"`

	Nodes  CompactIPv4NodeInfo `bencode:"nodes,omitempty"`
	Nodes6 CompactIPv6NodeInfo `bencode:"nodes6,omitempty"`

	Token  *string    `bencode:"token,omitempty"`
	Values []NodeAddr `bencode:"values,omitempty"`

	BFsd *ScrapeBloomFilter `bencode:"BFsd,omitempty"`
	BFpe *ScrapeBloomFilter `bencode:"BFpe,omitempty"`

	Bep51Return

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

func (m Msg) Error() *Error {
	if m.Y != "e" {
		return nil
	}
	return m.E
}
