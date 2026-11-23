package traversal

import (
	"context"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"yoru/internal/bt/dht/int160"
	k_nearest_nodes "yoru/internal/bt/dht/k-nearest-nodes"
	"yoru/internal/bt/dht/krpc"
	"yoru/internal/bt/dht/types"
)

type fakeNode struct {
	info krpc.NodeInfo
	id   int160.T
}

func fakeGraph(n int) []fakeNode {
	nodes := make([]fakeNode, n)
	for i := range nodes {
		var id krpc.ID
		id[0], id[1] = byte(i*37), byte(i*101)
		nodes[i] = fakeNode{
			info: krpc.NodeInfo{ID: id, Addr: krpc.NodeAddr{IP: net.IPv4(127, 0, 1, byte(i+1)).To4(), Port: 7000}},
			id:   int160.FromByteArray(id),
		}
	}
	return nodes
}

func nearest(nodes []fakeNode, target int160.T, k int) []fakeNode {
	sorted := slices.Clone(nodes)
	slices.SortFunc(sorted, func(a, b fakeNode) int { return a.id.Distance(target).Cmp(b.id.Distance(target)) })
	return sorted[:min(k, len(sorted))]
}

func TestTraversalConverges(t *testing.T) {
	nodes := fakeGraph(50)
	byAddr := map[string]fakeNode{}
	for _, n := range nodes {
		byAddr[n.info.Addr.String()] = n
	}
	var target krpc.ID
	target[0] = 0x55
	targetID := int160.FromByteArray(target)

	var mu sync.Mutex
	asked := map[string]int{}
	var calls atomic.Int32
	op := Start(OperationInput{
		Target: target,
		K:      8,
		Alpha:  3,
		DoQuery: func(_ context.Context, addr krpc.NodeAddr) QueryResult {
			calls.Add(1)
			mu.Lock()
			asked[addr.String()]++
			mu.Unlock()
			me := byAddr[addr.String()]
			var replies []krpc.NodeInfo
			for _, n := range nearest(nodes, me.id, 8) {
				replies = append(replies, n.info)
			}
			return QueryResult{ResponseFrom: &me.info, Nodes: replies}
		},
	})
	var start []types.AddrMaybeId
	for _, n := range nodes[:3] {
		start = append(start, types.AddrMaybeIdSliceFromNodeInfoSlice([]krpc.NodeInfo{n.info})...)
	}
	op.AddNodes(start)

	select {
	case <-op.Stalled():
	case <-time.After(10 * time.Second):
		t.Fatal("the traversal never stalled")
	}
	op.Stop()
	<-op.Stopped()

	mu.Lock()
	defer mu.Unlock()
	for addr, n := range asked {
		require.Equal(t, 1, n, "%s was asked more than once", addr)
	}
	var want, got []string
	for _, n := range nearest(nodes, targetID, 8) {
		want = append(want, n.info.Addr.String())
	}
	op.Closest().Range(func(e k_nearest_nodes.Elem) {
		got = append(got, e.Addr.String())
	})
	slices.Sort(want)
	slices.Sort(got)
	require.Equal(t, want, got)
}
