package bt

import (
	"fmt"
	"slices"

	g "github.com/anacrolix/generics"

	pp "yoru/internal/bt/peer_protocol"
)

type LocalLtepProtocolMap struct {
	// Extension names by number less one; the first NumBuiltin use built-in handlers. Moved past them a
	// protocol is yours to handle, removed it's off; change it in PeerConnAdded.
	Index []pp.ExtensionName
	// How many of the protocols are using the builtin handlers.
	NumBuiltin int
}

func (me *LocalLtepProtocolMap) toSupportedExtensionDict() (m map[pp.ExtensionName]pp.ExtensionNumber) {
	g.MakeMapWithCap(&m, len(me.Index))
	for i, name := range me.Index {
		old := g.MapInsert(m, name, pp.ExtensionNumber(i+1))
		if old.Ok {
			panic(fmt.Sprintf("extension %q already defined with id %v", name, old.Value))
		}
	}
	return
}

// LookupId names a local extension ID, builtin when handled here; ID 0 is the handshake, an error.
func (me *LocalLtepProtocolMap) LookupId(id pp.ExtensionNumber) (name pp.ExtensionName, builtin bool, err error) {
	if id == 0 {
		err = fmt.Errorf("extension ID 0 is handshake")
		builtin = true
		return
	}
	protocolIndex := int(id - 1)
	if protocolIndex >= len(me.Index) {
		err = fmt.Errorf("unexpected extended message ID: %v", id)
		return
	}
	builtin = protocolIndex < me.NumBuiltin
	name = me.Index[protocolIndex]
	return
}

func (me *LocalLtepProtocolMap) builtin() []pp.ExtensionName {
	return me.Index[:me.NumBuiltin]
}

func (me *LocalLtepProtocolMap) user() []pp.ExtensionName {
	return me.Index[me.NumBuiltin:]
}

func (me *LocalLtepProtocolMap) AddUserProtocol(name pp.ExtensionName) {
	builtin := slices.DeleteFunc(me.builtin(), func(delName pp.ExtensionName) bool {
		return delName == name
	})
	user := slices.DeleteFunc(me.user(), func(delName pp.ExtensionName) bool {
		return delName == name
	})
	me.Index = append(append(builtin, user...), name)
	me.NumBuiltin = len(builtin)
}
