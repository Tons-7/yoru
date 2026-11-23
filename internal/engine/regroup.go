package engine

import (
	"time"

	"yoru/internal/bt"
	"yoru/internal/bt/metainfo"
)

var (
	regroupAfter    = 20 * time.Second
	regroupMax      = 10 * time.Minute
	rememberedPeers = 200
)

type remembered struct {
	info bt.PeerInfo
	seen time.Time
}

func connected(t *bt.Torrent) []bt.PeerInfo {
	conns := t.PeerConns()
	out := make([]bt.PeerInfo, 0, len(conns))
	for _, c := range conns {
		if c.RemoteAddr != nil {
			out = append(out, bt.PeerInfo{Addr: c.RemoteAddr, Source: c.Discovery})
		}
	}
	return out
}

func (e *Engine) remember(hash metainfo.Hash, peers []bt.PeerInfo, now time.Time) {
	if len(peers) == 0 {
		return
	}
	known := e.known[hash]
	if known == nil {
		known = map[string]remembered{}
		e.known[hash] = known
	}
	for _, p := range peers {
		if p.Addr == nil {
			continue
		}
		key := p.Addr.String()
		if _, held := known[key]; !held && len(known) >= rememberedPeers {
			oldest, at := "", now
			for k, r := range known {
				if r.seen.Before(at) {
					oldest, at = k, r.seen
				}
			}
			if oldest == "" {
				continue
			}
			delete(known, oldest)
		}
		known[key] = remembered{info: bt.PeerInfo{Addr: p.Addr, Source: p.Source}, seen: now}
	}
}

func (en *entry) tryAgain(now time.Time) bool {
	if en.aloneSince.IsZero() {
		en.aloneSince, en.regroupWait = now, regroupAfter
	}
	if now.Sub(en.aloneSince) < en.regroupWait {
		return false
	}
	en.aloneSince = now
	en.regroupWait = min(en.regroupWait*2, regroupMax)
	en.regroups++
	return true
}

func (e *Engine) regroup(en *entry, now time.Time) {
	t := en.t
	if t.Info() == nil {
		return
	}
	hash := t.InfoHash()
	peers := connected(t)
	alone := len(peers) == 0 && t.Stats().ActivePeers == 0

	e.mu.Lock()
	e.remember(hash, peers, now)
	starved := alone && !en.paused && !en.verifying && wantsData(en)
	if !starved {
		en.aloneSince, en.regroupWait, en.regroups = time.Time{}, 0, 0
		e.mu.Unlock()
		return
	}
	tries := en.regroups
	due := en.tryAgain(now)
	var again []bt.PeerInfo
	if due {
		for _, r := range e.known[hash] {
			again = append(again, r.info)
		}
	}
	cl, current := e.cl, e.byID[en.id] == en
	e.mu.Unlock()

	if !due || !current || cl == nil {
		return
	}
	level := e.log.Info
	if tries > 0 {
		level = e.log.Debug
	}
	level("download has no peers; trying the ones it had again", "name", t.Name(), "peers", len(again), "try", tries+1)
	askAround(t, cl, again)
}

func askAround(t *bt.Torrent, cl *bt.Client, peers []bt.PeerInfo) {
	t.AddPeers(peers)
	for _, dht := range cl.DhtServers() {
		if _, stop, err := t.AnnounceToDht(dht); err == nil {
			time.AfterFunc(time.Minute, stop)
		}
	}
}
