package bt

import (
	"testing"
	"time"

	pp "yoru/internal/bt/peer_protocol"
)

// queuedPeer is a peer measured at rate bytes a second with queued requests waiting on it.
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

// A seek's urgent request moves only to a peer that would serve it twice as soon, or off a holder gone silent.
func TestStealsDecision(t *testing.T) {
	const req RequestIndex = 7
	for _, c := range []struct {
		name                  string
		ours, theirs          float64
		ourQueue, theirQueue  int
		upstream              bool
		sentAgo               time.Duration // 0: never sent
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

// Request then cancel, over and over, against a request server stalled on its data allowance: the arrival
// order stays bounded by what is live.
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

// The rate is closed per second of expecting data and averaged with the last; a shorter span changes nothing.
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
	// The half window counts once the second is full: 3.5 MB over that second, averaged with 1e6.
	read(3_000_000, 500*time.Millisecond)
	if want := (1e6 + 3.5e6) / 2; p.queue.rate != want {
		t.Fatalf("second window: rate = %v, want %v", p.queue.rate, want)
	}
}

// The queue holds RequestQueueTime of the rate, never less than MinRequestQueue nor more than the peer allows.
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
