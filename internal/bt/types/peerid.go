package types

import (
	"encoding/json"
	"fmt"
)

type PeerID [20]byte

func (me PeerID) String() string {
	return fmt.Sprintf("%+q", me[:])
}

func (me PeerID) MarshalJSON() ([]byte, error) {
	return json.Marshal(me.String())
}
