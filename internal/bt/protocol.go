package bt

import (
	pp "yoru/internal/bt/peer_protocol"
)

func makeCancelMessage(r Request) pp.Message {
	return pp.MakeCancelMessage(r.Index, r.Begin, r.Length)
}
