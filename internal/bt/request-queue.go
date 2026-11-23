package bt

import "time"

var (
	RequestQueueTime = 2 * time.Second
	MinRequestQueue  = 16
)

const (
	rateWindow      = time.Second
	retryEmptyAfter = 250 * time.Millisecond
)

type queueRate struct {
	windowBytes  int64
	windowExpect time.Duration
	rate         float64
	foundNothing bool
}

func (p *Peer) sampleRate() {
	bytes, expect := p._stats.BytesReadUsefulData.Int64(), p.totalExpectingTime()
	q := &p.queue
	if expect-q.windowExpect < rateWindow {
		return
	}
	sample := float64(bytes-q.windowBytes) / (expect - q.windowExpect).Seconds()
	if q.rate == 0 {
		q.rate = sample
	} else {
		q.rate = (q.rate + sample) / 2
	}
	q.windowBytes, q.windowExpect = bytes, expect
}

func (cn *PeerConn) nominalMaxRequests() maxRequests {
	want := MinRequestQueue
	if r := cn.queue.rate; r > 0 && cn.t.chunkSize > 0 {
		want = max(want, int(r*RequestQueueTime.Seconds()/float64(cn.t.chunkSize)))
	}
	return max(1, min(cn.PeerMaxRequests, want, maxLocalToRemoteRequests))
}

func (p *PeerConn) steals(existing *PeerConn, req RequestIndex, upstream bool) bool {
	sent := p.t.requestState[req].when
	if !sent.IsZero() && time.Since(sent) >= RequestQueueTime && !existing.lastUsefulChunkReceived.After(sent) {
		return true
	}
	ours, theirs := p.queue.rate, existing.queue.rate
	switch {
	case theirs <= 0:
		return upstream
	case ours <= 0:
		return false
	}
	ourWait := float64(p.requestState.Requests.GetCardinality()+1) / ours
	theirWait := float64(existing.requestState.Requests.GetCardinality()) / theirs
	return 2*ourWait < theirWait
}

func (p *PeerConn) isLowOnRequests() bool {
	pending := maxRequests(p.requestState.Requests.GetCardinality() + p.requestState.Cancelled.GetCardinality())
	if pending == 0 {
		return true
	}
	if pending > p.nominalMaxRequests()/2 {
		return false
	}
	return !p.queue.foundNothing || time.Since(p.lastRequestUpdate) >= retryEmptyAfter
}
