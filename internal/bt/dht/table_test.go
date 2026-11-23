package dht

import (
	"net"
	"testing"

	qt "github.com/frankban/quicktest"
	"github.com/stretchr/testify/assert"

	"yoru/internal/bt/dht/int160"
)

func TestTable(t *testing.T) {
	tbl := table{k: 8}
	var maxFar int160.T
	maxFar.SetMax()
	assert.Equal(t, 0, tbl.bucketIndex(maxFar))
	assert.Panics(t, func() { tbl.bucketIndex(tbl.rootID) })

	assert.Error(t, tbl.addNode(&node{}))
	assert.Equal(t, 0, tbl.buckets[0].Len())

	id0 := int160.FromByteString("\x2f\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	id1 := int160.FromByteString("\x2e\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	n0 := &node{nodeKey: nodeKey{
		Id:   id0,
		Addr: NewAddr(&net.UDPAddr{}),
	}}
	n1 := &node{nodeKey: nodeKey{
		Id:   id1,
		Addr: NewAddr(&net.UDPAddr{}),
	}}

	assert.NoError(t, tbl.addNode(n0))
	assert.Equal(t, 1, tbl.buckets[2].Len())

	assert.Error(t, tbl.addNode(n0))
	assert.Equal(t, 1, tbl.buckets[2].Len())
	assert.Equal(t, 1, tbl.numNodes())

	assert.NoError(t, tbl.addNode(n1))
	assert.Equal(t, 2, tbl.buckets[2].Len())
	assert.Equal(t, 2, tbl.numNodes())

	tbl.dropNode(n0)
	assert.Equal(t, 1, tbl.buckets[2].Len())
	assert.Equal(t, 1, tbl.numNodes())

	tbl.dropNode(n1)
	assert.Equal(t, 0, tbl.buckets[2].Len())
	assert.Equal(t, 0, tbl.numNodes())
}

func TestClosestNodesAreTheNearestInOrder(t *testing.T) {
	tbl := table{k: 8}
	var nodes []*node
	for i := 1; i <= 40; i++ {
		id := int160.FromByteArray([20]byte{byte(i), byte(i * 7)})
		n := &node{nodeKey: nodeKey{Id: id, Addr: NewAddr(&net.UDPAddr{IP: net.IPv4(1, 2, 3, byte(i)), Port: 1000 + i})}}
		if tbl.addNode(n) == nil {
			nodes = append(nodes, n)
		}
	}
	target := int160.FromByteArray([20]byte{0x20, 0x01})
	got := tbl.closestNodes(5, target, func(*node) bool { return true })
	assert.Len(t, got, 5)
	for i := 1; i < len(got); i++ {
		assert.LessOrEqual(t, got[i-1].Id.Distance(target).Cmp(got[i].Id.Distance(target)), 0)
	}
	far := got[len(got)-1].Id.Distance(target)
	for _, n := range nodes {
		if n.Id.Distance(target).Cmp(far) < 0 {
			assert.Contains(t, got, n)
		}
	}
}

func TestRandomIdInBucket(t *testing.T) {
	tbl := table{
		rootID: int160.FromByteArray(RandomNodeID()),
	}
	t.Logf("%v: table root id", tbl.rootID)
	for i := range tbl.buckets {
		id := tbl.randomIdForBucket(i)
		t.Logf("%v: random id for bucket index %v", id, i)
		qt.Assert(t, tbl.bucketIndex(id), qt.Equals, i)
	}
}
