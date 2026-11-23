package dht

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/anacrolix/chansync"
	"github.com/anacrolix/chansync/events"
	"github.com/anacrolix/log"

	"yoru/internal/bt/dht/int160"
	dhtutil "yoru/internal/bt/dht/k-nearest-nodes"
	"yoru/internal/bt/dht/krpc"
	"yoru/internal/bt/dht/traversal"
)

type Announce struct {
	Peers chan PeersValues

	server   *Server
	infoHash int160.T

	announcePeerOpts *AnnouncePeerOpts
	scrape           bool
	peerAnnounced    chansync.SetOnce

	traversal *traversal.Operation

	closed chansync.SetOnce
}

func (a *Announce) String() string {
	return fmt.Sprintf("%[1]T %[1]p of %v on %v", a, a.infoHash, a.server)
}

func (a *Announce) NumContacted() uint32 {
	return atomic.LoadUint32(&a.traversal.Stats().NumAddrsTried)
}

func (a *Announce) TraversalStats() TraversalStats {
	return *a.traversal.Stats()
}

type AnnounceOpt func(a *Announce)

func Scrape() AnnounceOpt {
	return func(a *Announce) {
		a.scrape = true
	}
}

type AnnouncePeerOpts struct {
	Port        int
	ImpliedPort bool
}

func AnnouncePeer(opts AnnouncePeerOpts) AnnounceOpt {
	return func(a *Announce) {
		a.announcePeerOpts = &opts
	}
}

func (s *Server) Announce(infoHash [20]byte, port int, impliedPort bool, opts ...AnnounceOpt) (_ *Announce, err error) {
	if port != 0 || impliedPort {
		opts = append([]AnnounceOpt{AnnouncePeer(AnnouncePeerOpts{
			Port:        port,
			ImpliedPort: impliedPort,
		})}, opts...)
	}
	return s.AnnounceTraversal(infoHash, opts...)
}

func (s *Server) AnnounceTraversal(infoHash [20]byte, opts ...AnnounceOpt) (_ *Announce, err error) {
	infoHashInt160 := int160.FromByteArray(infoHash)
	a := &Announce{
		Peers:    make(chan PeersValues),
		server:   s,
		infoHash: infoHashInt160,
	}
	for _, opt := range opts {
		opt(a)
	}
	a.traversal = traversal.Start(traversal.OperationInput{
		Target:     infoHash,
		DoQuery:    a.getPeers,
		NodeFilter: s.TraversalNodeFilter,
		DataFilter: func(data any) bool {
			_, ok := data.(string)
			return ok
		},
	})
	nodes, err := s.TraversalStartingNodes()
	if err != nil {
		a.traversal.Stop()
		return
	}
	a.traversal.AddNodes(nodes)
	go func() {
		<-a.traversal.Stalled()
		a.traversal.Stop()
		<-a.traversal.Stopped()
		if a.announcePeerOpts != nil {
			a.announceClosest()
		}
		a.peerAnnounced.Set()
		close(a.Peers)
	}()
	return a, nil
}

func (a *Announce) announceClosest() {
	var wg sync.WaitGroup
	a.traversal.Closest().Range(func(elem dhtutil.Elem) {
		wg.Go(func() {
			a.logger().Levelf(log.Debug,
				"announce_peer to %v: %v",
				elem, a.announcePeer(elem),
			)
		})
	})
	wg.Wait()
}

func (a *Announce) announcePeer(peer dhtutil.Elem) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-a.closed.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return a.server.announcePeer(
		ctx,
		NewAddr(peer.Addr.UDP()),
		a.infoHash,
		a.announcePeerOpts.Port,
		peer.Data.(string),
		a.announcePeerOpts.ImpliedPort,
		QueryRateLimiting{},
	).Err
}

func (a *Announce) getPeers(ctx context.Context, addr krpc.NodeAddr) traversal.QueryResult {
	res := a.server.GetPeers(ctx, NewAddr(addr.UDP()), a.infoHash, a.scrape, QueryRateLimiting{})
	if r := res.Reply.R; r != nil {
		peersValues := PeersValues{
			Peers: replyPeers(addr, r.Values),
			NodeInfo: krpc.NodeInfo{
				Addr: addr,
				ID:   r.ID,
			},
			Return: *r,
		}
		select {
		case a.Peers <- peersValues:
		case <-a.traversal.Stopped():
		}
	}
	return res.TraversalQueryResult(addr)
}

const maxPeersPerReply = 100

func replyPeers(from krpc.NodeAddr, values []Peer) []Peer {
	local := !traversal.PublicAddr(from.ToNodeAddrPort().Addr())
	seen := make(map[string]bool, min(len(values), maxPeersPerReply))
	kept := make([]Peer, 0, min(len(values), maxPeersPerReply))
	for _, p := range values {
		if len(kept) == maxPeersPerReply {
			break
		}
		if p.Port <= 0 || p.Port > 65535 {
			continue
		}
		if !local && !traversal.PublicAddr(p.ToNodeAddrPort().Addr()) {
			continue
		}
		key := p.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, p)
	}
	return kept
}

type PeersValues struct {
	Peers []Peer
	krpc.NodeInfo
	krpc.Return
}

func (a *Announce) Close() {
	a.StopTraversing()
	a.closed.Set()
}

func (a *Announce) logger() log.Logger {
	return a.server.logger()
}

func (a *Announce) StopTraversing() {
	a.traversal.Stop()
}

func (a *Announce) Finished() events.Done {
	return a.peerAnnounced.Done()
}
