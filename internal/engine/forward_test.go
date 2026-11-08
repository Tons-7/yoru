package engine

import (
	"sync/atomic"
	"testing"

	"yoru/internal/bt"
)

func TestANewNetworkAsksItsRouterForThePeerPort(t *testing.T) {
	addrs, fwd := localAddrs, forwardOn
	t.Cleanup(func() { localAddrs, forwardOn = addrs, fwd })
	mi, seeds, _ := bigFileSeeders(t, 1<<20, 256<<10, 1<<20)
	e, _ := seedEngine(t, mi, seeds, nil, func(*bt.ClientConfig) {})

	var asked atomic.Int32
	forwardOn = func(*bt.Client) { asked.Add(1) }
	localAddrs = func() map[string]bool { return map[string]bool{"10.0.0.5": true} }

	e.followAddresses(map[string]bool{"10.0.0.4": true}, false)
	if asked.Load() == 0 {
		t.Fatal("moving to another network left the new router unasked")
	}
	before := asked.Load()
	e.followAddresses(map[string]bool{"10.0.0.5": true}, false)
	if asked.Load() != before {
		t.Error("the router was asked again with the network unchanged")
	}

	localAddrs = func() map[string]bool { return map[string]bool{"10.0.0.5": true, "2001:db8::42": true} }
	e.followAddresses(map[string]bool{"10.0.0.5": true, "2001:db8::41": true}, false)
	if asked.Load() != before {
		t.Error("an IPv6 address rotating asked the router again")
	}
}
