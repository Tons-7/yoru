package dht

import (
	"context"
	"errors"
	"time"

	"yoru/internal/bt/dht/krpc"
	"yoru/internal/bt/dht/traversal"
)

type TraversalStats = traversal.Stats

func (s *Server) Bootstrap() (TraversalStats, error) {
	return s.BootstrapContext(context.Background())
}

func (s *Server) BootstrapContext(ctx context.Context) (_ TraversalStats, err error) {
	s.mu.Lock()
	if s.bootstrappingNow {
		s.mu.Unlock()
		err = errors.New("already bootstrapping")
		return
	}
	s.bootstrappingNow = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.bootstrappingNow = false
	}()
	t := traversal.Start(traversal.OperationInput{
		Target: s.id.AsByteArray(),
		K:      16,
		DoQuery: func(ctx context.Context, addr krpc.NodeAddr) traversal.QueryResult {
			return s.FindNode(NewAddr(addr.UDP()), s.id, QueryRateLimiting{}).TraversalQueryResult(addr)
		},
		NodeFilter: s.TraversalNodeFilter,
	})
	nodes, err := s.TraversalStartingNodes()
	if err != nil {
		t.Stop()
		return
	}
	t.AddNodes(nodes)
	s.mu.Lock()
	s.lastBootstrap = time.Now()
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case <-t.Stalled():
	}
	t.Stop()
	if err != nil {
		return
	}
	<-t.Stopped()
	return *t.Stats(), nil
}
