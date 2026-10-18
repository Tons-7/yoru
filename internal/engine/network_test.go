package engine

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yoru/internal/bt"
	"yoru/internal/torrent"
)

// A new IPv6 privacy address beside the one in use changes nothing; the one in use going does.
func TestKeepsTheBoundAddress(t *testing.T) {
	v4, v6, temp := net.ParseIP("10.2.0.2").To4(), net.ParseIP("2001:db8::2"), net.ParseIP("2001:db8::99")
	bound := bindAddrs{v4: v4, v6: v6}
	for _, c := range []struct {
		now  bindAddrs
		want bool
	}{
		{bindAddrs{v4: v4, v6: temp, all: []net.IP{v4, temp, v6}}, true},
		{bindAddrs{v4: v4, v6: v6}, true},
		{bindAddrs{v4: v4, v6: temp, all: []net.IP{v4, temp}}, false},
		{bindAddrs{v4: net.ParseIP("10.2.0.3").To4(), v6: v6, all: []net.IP{v6}}, false},
	} {
		if got := c.now.keeps(bound); got != c.want {
			t.Errorf("%v keeps %v: %v, want %v", c.now.all, bound, got, c.want)
		}
	}
}

// With a network chosen but nothing asked of torrents yet, the client isn't started, nor said to be waiting.
func TestUnstartedEngineLeftAlone(t *testing.T) {
	was := networkEvery
	networkEvery = 20 * time.Millisecond
	t.Cleanup(func() { networkEvery = was })
	e, err := New(Options{CacheDir: t.TempDir(), SessionDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Stop)
	e.resolve = func(string) (bindAddrs, error) { return bindAddrs{v4: net.IPv4(127, 0, 0, 2).To4()}, nil }
	e.SetLimits(torrent.Limits{Upload: torrent.UploadWhileDownloading, Adapter: "vpn"})
	time.Sleep(200 * time.Millisecond)
	e.mu.Lock()
	started := e.cl != nil
	e.mu.Unlock()
	if started {
		t.Error("the network watcher started the client")
	}
	if n := e.Network(); n.Waiting {
		t.Errorf("network = %+v; nothing is waiting", n)
	}
}

// Torrents kept to an adapter leave only from its address, stop when it drops (nothing over the open line), and
// carry on under the same id when it's back.
func TestKeptToAnAdapter(t *testing.T) {
	was := networkEvery
	networkEvery = 100 * time.Millisecond
	t.Cleanup(func() { networkEvery = was })

	mi, seeds, seeders := bigFileSeeders(t, 16<<20, 256<<10, 2<<20)
	var useful atomic.Int64
	var up atomic.Bool
	up.Store(true)
	// A second loopback address stands in for the VPN's: the seeder sees which one a connection comes from.
	vpn := bindAddrs{v4: net.IPv4(127, 0, 0, 2).To4()}
	e, id := seedEngine(t, mi, seeds, nil, func(cfg *bt.ClientConfig) {
		cfg.Callbacks.ReceivedUsefulData = append(cfg.Callbacks.ReceivedUsefulData, func(ev bt.ReceivedUsefulDataEvent) {
			useful.Add(int64(len(ev.Message.Piece)))
		})
	}, func(e *Engine) {
		e.resolve = func(name string) (bindAddrs, error) {
			if name != "vpn" || !up.Load() {
				return bindAddrs{}, errNoAdapter
			}
			return vpn, nil
		}
		e.SetLimits(torrent.Limits{Upload: torrent.UploadWhileDownloading, Adapter: "vpn"})
	})
	until := func(what string, within time.Duration, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(within); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("waited %s for %s", within, what)
			}
		}
	}
	seeding, _ := seeders[0].Torrent(mi.HashInfoBytes())
	peersFrom := func() (hosts []string) {
		for _, pc := range seeding.PeerConns() {
			host, _, _ := net.SplitHostPort(pc.RemoteAddr.String())
			hosts = append(hosts, host)
		}
		return
	}

	until("the download to start", 10*time.Second, func() bool { return useful.Load() > 1<<20 })
	for _, host := range peersFrom() {
		if host != "127.0.0.2" {
			t.Errorf("the seeder sees a connection from %s; want only the chosen network's 127.0.0.2", host)
		}
	}
	if n := e.Network(); n.Adapter != "vpn" || n.Address != "127.0.0.2" || n.Waiting {
		t.Errorf("network = %+v", n)
	}

	// The VPN drops: everything stops, and nothing goes out another way.
	up.Store(false)
	until("torrents to wait for the network", 3*time.Second, func() bool { return e.Network().Waiting })
	until("the seeder to lose us", 3*time.Second, func() bool { return len(peersFrom()) == 0 })
	before := useful.Load()
	time.Sleep(1500 * time.Millisecond)
	if got := useful.Load() - before; got != 0 {
		t.Errorf("%d bytes arrived with the chosen network down", got)
	}
	if hosts := peersFrom(); len(hosts) != 0 {
		t.Errorf("connected with the chosen network down, from %v", hosts)
	}
	if _, err := e.Client().List(context.Background()); err == nil || !strings.Contains(err.Error(), "waiting for vpn") {
		t.Errorf("listing while down: %v, want it to say torrents wait for vpn", err)
	}

	// Back: the same torrent, by the same id, finishes.
	up.Store(true)
	until("the client to come back", 3*time.Second, func() bool { return !e.Network().Waiting })
	until("the episode to finish", 30*time.Second, func() bool {
		en, err := e.entryByID(id)
		return err == nil && en.t.Info() != nil && en.t.BytesMissing() == 0
	})
	for _, host := range peersFrom() {
		if host != "127.0.0.2" {
			t.Errorf("after coming back the seeder sees %s", host)
		}
	}
}

// Wi-Fi change: connections on the old address go now; the new address redials (regroup set a minute off).
func TestNetworkChangeRedialsAtOnce(t *testing.T) {
	was, group, addrs, route := networkEvery, regroupAfter, localAddrs, routeSource
	networkEvery, regroupAfter = 100*time.Millisecond, time.Minute
	var machine atomic.Pointer[map[string]bool]
	set := func(ips ...string) {
		m := map[string]bool{}
		for _, ip := range ips {
			m[ip] = true
		}
		machine.Store(&m)
	}
	set("127.0.0.1", "10.9.9.9")
	localAddrs = func() map[string]bool { return *machine.Load() }
	// The peer is reached from 127.0.0.1, standing in for the Wi-Fi's address, while the machine has it.
	routeSource = func(net.IP) net.IP {
		if (*machine.Load())["127.0.0.1"] {
			return net.IPv4(127, 0, 0, 1)
		}
		return nil
	}
	t.Cleanup(func() { networkEvery, regroupAfter, localAddrs, routeSource = was, group, addrs, route })

	mi, seeds, _ := bigFileSeeders(t, 16<<20, 256<<10, 1<<20)
	var useful atomic.Int64
	e, id := seedEngine(t, mi, seeds, nil, func(cfg *bt.ClientConfig) {
		cfg.Callbacks.ReceivedUsefulData = append(cfg.Callbacks.ReceivedUsefulData, func(ev bt.ReceivedUsefulDataEvent) {
			useful.Add(int64(len(ev.Message.Piece)))
		})
	})
	en, err := e.entryByID(id)
	if err != nil {
		t.Fatal(err)
	}
	until := func(what string, within time.Duration, ok func() bool) {
		t.Helper()
		for end := time.Now().Add(within); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("waited %s for %s", within, what)
			}
		}
	}
	until("the download to start", 10*time.Second, func() bool { return useful.Load() > 1<<20 })
	// Remembered by the janitor, as a real swarm's peers are.
	until("the peer to be remembered", 3*time.Second, func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return len(e.known[en.t.InfoHash()]) > 0
	})

	// The address in use goes and none comes: dropped, and nothing to redial on yet.
	set("10.9.9.9")
	// Closed, a connection's bytes stop at once, though bt lists a uTP one until its reader ends.
	quiet := func() bool {
		before := useful.Load()
		time.Sleep(400 * time.Millisecond)
		return useful.Load() == before
	}
	until("the connections on the old address to go", 3*time.Second, quiet)
	stopped := useful.Load()

	set("10.9.9.9", "127.0.0.1")
	until("the download to carry on", 3*time.Second, func() bool { return useful.Load() > stopped+(256<<10) })
}

// A change elsewhere (a Docker adapter) leaves working connections alone: here loopback, routed for real.
func TestUnrelatedAddressChangeLeavesPeersAlone(t *testing.T) {
	mi, seeds, _ := bigFileSeeders(t, 16<<20, 256<<10, 1<<20)
	e, id := seedEngine(t, mi, seeds, nil, func(*bt.ClientConfig) {})
	en, err := e.entryByID(id)
	if err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(10 * time.Second); len(en.t.PeerConns()) == 0; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("no connection to the seeder")
		}
	}
	if n := e.dropDead(map[string]bool{"10.9.9.9": true}, map[string]bool{"10.1.1.1": true}); n != 0 {
		t.Errorf("dropped %d connections that the change did not touch", n)
	}
}
