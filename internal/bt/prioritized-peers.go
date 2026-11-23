package bt

import (
	"hash/maphash"

	"github.com/anacrolix/multiless"
	"github.com/google/btree"
)

type prioritizedPeersItem struct {
	prio peerPriority
	p    PeerInfo
	hash int64
}

var hashSeed = maphash.MakeSeed()

func newPrioritizedItem(prio peerPriority, p PeerInfo) prioritizedPeersItem {
	var h maphash.Hash
	h.SetSeed(hashSeed)
	h.WriteString(p.Addr.String())
	return prioritizedPeersItem{prio: prio, p: p, hash: int64(h.Sum64())}
}

func (me prioritizedPeersItem) Less(than btree.Item) bool {
	other := than.(prioritizedPeersItem)
	return multiless.New().Bool(
		me.p.Trusted, other.p.Trusted).Uint32(
		me.prio, other.prio).Int64(
		me.hash, other.hash,
	).Less()
}

type prioritizedPeers struct {
	om      *btree.BTree
	getPrio func(PeerInfo) peerPriority
}

func (me *prioritizedPeers) Each(f func(PeerInfo)) {
	me.om.Ascend(func(i btree.Item) bool {
		f(i.(prioritizedPeersItem).p)
		return true
	})
}

func (me *prioritizedPeers) Len() int {
	if me == nil || me.om == nil {
		return 0
	}
	return me.om.Len()
}

func (me *prioritizedPeers) Add(p PeerInfo) bool {
	return me.om.ReplaceOrInsert(newPrioritizedItem(me.getPrio(p), p)) != nil
}

func (me *prioritizedPeers) AddReturningReplacedPeer(p PeerInfo) (ret PeerInfo, ok bool) {
	item := me.om.ReplaceOrInsert(newPrioritizedItem(me.getPrio(p), p))
	if item == nil {
		return
	}
	ret = item.(prioritizedPeersItem).p
	ok = true
	return
}

func (me *prioritizedPeers) DeleteMin() (ret prioritizedPeersItem, ok bool) {
	i := me.om.DeleteMin()
	if i == nil {
		return
	}
	ret = i.(prioritizedPeersItem)
	ok = true
	return
}

func (me *prioritizedPeers) PopMax() PeerInfo {
	return me.om.DeleteMax().(prioritizedPeersItem).p
}
