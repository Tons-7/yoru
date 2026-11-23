package bt

import (
	"testing"
	"time"

	pp "yoru/internal/bt/peer_protocol"
)

func queuedPeer(t *Torrent, rate float64, queued int) *PeerConn {
	p := &PeerConn{PeerMaxRequests: 250}
	p.t = t
	p.requestState.Requests = &peerRequests{}
	for i := range queued {
		p.requestState.Requests.Add(RequestIndex(1000 + i))
	}
	p.queue.rate = rate
	return p
}

func TestStealsDecision(t *testing.T) {
	const req RequestIndex = 7
	for _, c := range []struct {
		name                  string
		ours, theirs          float64
		ourQueue, theirQueue  int
		upstream              bool
		sentAgo               time.Duration
		holderDeliveredLately bool
		want                  bool
	}{
		{name: "quick and idle takes it from slow and deep", ours: 1e6, theirs: 1e5, theirQueue: 20, want: true},
		{name: "equal peers keep it where it is", ours: 1e6, theirs: 1e6, ourQueue: 10, theirQueue: 10},
		{name: "just quicker is not enough", ours: 1.5e6, theirs: 1e6, ourQueue: 10, theirQueue: 10},
		{name: "an unmeasured stealer never steals", ours: 0, theirs: 1e5, theirQueue: 50},
		{name: "an unmeasured holder yields upstream", ours: 1e5, theirs: 0, upstream: true, want: true},
		{name: "an unmeasured holder keeps it otherwise", ours: 1e5, theirs: 0},
		{
			name: "a holder silent since the send loses it even to a slower peer", ours: 1e4, theirs: 1e6,
			sentAgo: RequestQueueTime + time.Second, want: true,
		},
		{
			name: "a holder that delivered since keeps the rate rule", ours: 1e4, theirs: 1e6,
			sentAgo: RequestQueueTime + time.Second, holderDeliveredLately: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			tor := &Torrent{requestState: map[RequestIndex]requestState{}}
			if c.sentAgo > 0 {
				tor.requestState[req] = requestState{when: time.Now().Add(-c.sentAgo)}
			}
			stealer, holder := queuedPeer(tor, c.ours, c.ourQueue), queuedPeer(tor, c.theirs, c.theirQueue)
			if c.holderDeliveredLately {
				holder.lastUsefulChunkReceived = time.Now()
			}
			if got := stealer.steals(holder, req, c.upstream); got != c.want {
				t.Fatalf("steals = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIsLowOnRequests(t *testing.T) {
	tor := &Torrent{chunkSize: defaultChunkSize}
	nominal := queuedPeer(tor, 4e6, 0).nominalMaxRequests()
	if nominal < 8 {
		t.Fatalf("nominal %d too small for the cases", nominal)
	}
	for _, c := range []struct {
		name       string
		queued     int
		cancelled  int
		foundNone  bool
		updatedAgo time.Duration
		want       bool
	}{
		{name: "empty", want: true},
		{name: "over half full", queued: nominal/2 + 1},
		{name: "exactly half", queued: nominal / 2, want: true},
		{name: "cancels awaiting reject count", queued: nominal/2 - 1, cancelled: 2},
		{name: "nothing found just now", queued: 1, foundNone: true},
		{name: "nothing found a while ago", queued: 1, foundNone: true, updatedAgo: retryEmptyAfter, want: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := queuedPeer(tor, 4e6, c.queued)
			for i := range c.cancelled {
				p.requestState.Cancelled.Add(RequestIndex(5000 + i))
			}
			p.queue.foundNothing = c.foundNone
			p.lastRequestUpdate = time.Now().Add(-c.updatedAgo)
			if got := p.isLowOnRequests(); got != c.want {
				t.Fatalf("isLowOnRequests = %v, want %v (nominal %d)", got, c.want, nominal)
			}
		})
	}
}

func TestRequestFifoSweepsCancelledEntries(t *testing.T) {
	live := map[Request]uint64{}
	alive := func(r Request, a uint64) bool {
		x, ok := live[r]
		return ok && x == a
	}
	var f requestFifo
	for i := range 100_000 {
		r := Request{Index: pp.Integer(i % 50)}
		live[r] = uint64(i)
		f.push(r, uint64(i), len(live), alive)
		delete(live, r)
	}
	if len(f.q) > 2*len(live)+65 {
		t.Fatalf("%d entries kept for %d live requests", len(f.q), len(live))
	}
}

func TestSampleRateAveragesWindows(t *testing.T) {
	p := &Peer{}
	read := func(bytes int64, expecting time.Duration) {
		p._stats.BytesReadUsefulData.Add(bytes)
		p.cumulativeExpectedToReceiveChunks += expecting
		p.sampleRate()
	}
	read(1_000_000, time.Second)
	if p.queue.rate != 1e6 {
		t.Fatalf("first window: rate = %v, want 1e6", p.queue.rate)
	}
	read(500_000, 500*time.Millisecond)
	if p.queue.rate != 1e6 {
		t.Fatalf("half a window moved the rate to %v", p.queue.rate)
	}
	read(3_000_000, 500*time.Millisecond)
	if want := (1e6 + 3.5e6) / 2; p.queue.rate != want {
		t.Fatalf("second window: rate = %v, want %v", p.queue.rate, want)
	}
}

func TestNominalMaxRequestsScalesWithRate(t *testing.T) {
	tor := &Torrent{chunkSize: 16 << 10}
	for _, c := range []struct {
		rate float64
		peer maxRequests
		want maxRequests
	}{
		{rate: 0, peer: 250, want: maxRequests(MinRequestQueue)},
		{rate: 16 << 10, peer: 250, want: maxRequests(MinRequestQueue)},
		{rate: 50 * 16 << 10, peer: 250, want: 100},
		{rate: 50 * 16 << 10, peer: 30, want: 30},
	} {
		p := queuedPeer(tor, c.rate, 0)
		p.PeerMaxRequests = c.peer
		if got := p.nominalMaxRequests(); got != c.want {
			t.Errorf("rate %v, peer allows %d: %d, want %d", c.rate, c.peer, got, c.want)
		}
	}
}
