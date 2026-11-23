package bt

import (
	"fmt"
	"net/netip"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/log"

	pp "yoru/internal/bt/peer_protocol"
)

const (
	pexRetryDelay         = 10 * time.Second
	pexInterval           = 1 * time.Minute
	pexMaxAddedPerMsg     = 100
	pexMaxRemoteLiveConns = 1000
)

type pexConnState struct {
	enabled         bool
	xid             pp.ExtensionNumber
	last            *pexEvent
	timer           *time.Timer
	gate            chan struct{}
	readyfn         func()
	torrent         *Torrent
	Listed          bool
	logger          log.Logger
	remoteLiveConns map[netip.AddrPort]g.Option[pp.PexPeerFlags]
	lastRecv        time.Time
	handshaken      bool
}

func (s *pexConnState) IsEnabled() bool {
	return s.enabled
}

func (s *pexConnState) Init(c *PeerConn) {
	xid, ok := c.PeerExtensionIDs[pp.ExtensionNamePex]
	if !ok || xid == 0 || c.t.cl.config.DisablePEX {
		return
	}
	s.xid = xid
	s.last = nil
	s.torrent = c.t
	s.logger = c.logger.WithDefaultLevel(log.Debug).WithNames("pex")
	s.readyfn = c.tickleWriter
	s.gate = make(chan struct{}, 1)
	gate := s.gate
	s.timer = time.AfterFunc(0, func() {
		select {
		case gate <- struct{}{}:
		default:
		}
		s.readyfn()
	})
	s.enabled = true
}

func (s *pexConnState) sched(delay time.Duration) {
	s.timer.Reset(delay)
}

func (s *pexConnState) genmsg() *pp.PexMsg {
	tx, last := s.torrent.pex.Genmsg(s.last)
	if tx.Len() == 0 {
		return nil
	}
	s.last = last
	return &tx
}

func (s *pexConnState) numPending() int {
	if s.torrent == nil {
		return 0
	}
	return s.torrent.pex.numPending(s.last)
}

func (s *pexConnState) Share(postfn messageWriter) bool {
	select {
	case <-s.gate:
		if tx := s.genmsg(); tx != nil {
			s.logger.Print("sending PEX message: ", tx)
			flow := postfn(tx.Message(s.xid))
			s.sched(pexInterval)
			return flow
		} else {
			s.sched(pexRetryDelay)
		}
	default:
	}
	return true
}

func (s *pexConnState) updateRemoteLiveConns(rx pp.PexMsg) (errs []error) {
	for _, dropped := range rx.Dropped {
		addrPort, _ := ipv4AddrPortFromKrpcNodeAddr(dropped)
		delete(s.remoteLiveConns, addrPort)
	}
	for _, dropped := range rx.Dropped6 {
		addrPort, _ := ipv6AddrPortFromKrpcNodeAddr(dropped)
		delete(s.remoteLiveConns, addrPort)
	}
	g.MakeMapIfNil(&s.remoteLiveConns)
	note := func(addrPort netip.AddrPort, flags g.Option[pp.PexPeerFlags]) {
		if _, ok := s.remoteLiveConns[addrPort]; ok || len(s.remoteLiveConns) < pexMaxRemoteLiveConns {
			s.remoteLiveConns[addrPort] = flags
		}
	}
	for i, added := range rx.Added {
		ip4 := added.IP.To4()
		if ip4 == nil {
			continue
		}
		note(netip.AddrPortFrom(netip.AddrFrom4([4]byte(ip4)), uint16(added.Port)), g.SliceGet(rx.AddedFlags, i))
	}
	for i, added := range rx.Added6 {
		ip16 := added.IP.To16()
		if ip16 == nil {
			continue
		}
		note(netip.AddrPortFrom(netip.AddrFrom16([16]byte(ip16)), uint16(added.Port)), g.SliceGet(rx.Added6Flags, i))
	}
	return
}

func capPexAdded[T any](added []T, flags []pp.PexPeerFlags, n int) ([]T, []pp.PexPeerFlags) {
	if len(added) > n {
		added = added[:n]
	}
	if len(flags) > n {
		flags = flags[:n]
	}
	return added, flags
}

func (s *pexConnState) Recv(payload []byte) error {
	rx, err := pp.LoadPexMsg(payload)
	if err != nil {
		return fmt.Errorf("unmarshalling pex message: %w", err)
	}
	s.logger.Printf("received pex message: %v", rx)

	timeSinceLastRecv := time.Since(s.lastRecv)
	if timeSinceLastRecv < 45*time.Second {
		s.logger.Printf("ignoring a pex message only %v after the last", timeSinceLastRecv)
		return nil
	}
	s.lastRecv = time.Now()
	rx.Added, rx.AddedFlags = capPexAdded(rx.Added, rx.AddedFlags, pexMaxAddedPerMsg)
	rx.Added6, rx.Added6Flags = capPexAdded(rx.Added6, rx.Added6Flags, pexMaxAddedPerMsg)
	s.updateRemoteLiveConns(rx)

	var peers peerInfos
	peers.AppendFromPex(rx.Added6, rx.Added6Flags)
	peers.AppendFromPex(rx.Added, rx.AddedFlags)
	if time.Now().Before(s.torrent.pex.rest) && !s.torrent.wantPeers() {
		s.logger.Printf("in cooldown period, incoming PEX discarded")
		return nil
	}
	added := s.torrent.addPeers(peers)
	s.logger.Printf("got %v peers over pex, added %v", len(peers), added)

	if len(peers) > 0 {
		s.torrent.pex.rest = time.Now().Add(pexInterval)
	}

	return nil
}

func (s *pexConnState) Close() {
	if s.timer != nil {
		s.timer.Stop()
	}
}
