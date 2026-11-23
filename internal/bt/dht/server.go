package dht

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime/pprof"
	"slices"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/anacrolix/generics"
	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo"
	"yoru/internal/bt/bencode"
	"yoru/internal/bt/metainfo"

	"yoru/internal/bt/dht/int160"
	"yoru/internal/bt/dht/krpc"
	peer_store "yoru/internal/bt/dht/peer-store"
	"yoru/internal/bt/dht/transactions"
	"yoru/internal/bt/dht/traversal"
	"yoru/internal/bt/dht/types"
)

type Server struct {
	id          int160.T
	socket      net.PacketConn
	resendDelay func() time.Duration

	mu           sync.RWMutex
	transactions transactions.Dispatcher[*transaction]
	table        table
	closed       missinggo.Event
	tokenServer  tokenServer
	config       ServerConfig
	stats        ServerStats

	lastBootstrap    time.Time
	bootstrappingNow bool
}

func (s *Server) numGoodNodes() (num int) {
	s.table.forNodes(func(n *node) bool {
		if s.IsGood(n) {
			num++
		}
		return true
	})
	return
}

func prettySince(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	d /= time.Second
	d *= time.Second
	return fmt.Sprintf("%s ago", d)
}

func (s *Server) WriteStatus(w io.Writer) {
	fmt.Fprintf(w, "Listening on %s\n", s.Addr())
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(w, "Nodes in table: %d good, %d total\n", s.numGoodNodes(), s.numNodes())
	fmt.Fprintf(w, "Ongoing transactions: %d\n", s.transactions.NumActive())
	fmt.Fprintf(w, "Server node ID: %x\n", s.id.Bytes())
	buckets := &s.table.buckets
	for i := range s.table.buckets {
		b := &buckets[i]
		if b.Len() == 0 && b.lastChanged.IsZero() {
			continue
		}
		fmt.Fprintf(w,
			"b# %v: %v nodes, last updated: %v\n",
			i, b.Len(), prettySince(b.lastChanged))
		if b.Len() > 0 {
			tw := tabwriter.NewWriter(w, 0, 0, 1, ' ', 0)
			fmt.Fprintf(tw, "  node id\taddr\tlast query\tlast response\trecv\tdiscard\tflags\n")
			nodes := slices.SortedFunc(b.NodeIter(), func(l *node, r *node) int {
				return l.Id.Distance(s.id).Cmp(r.Id.Distance(s.id))
			})
			for _, n := range nodes {
				var flags []string
				if s.IsQuestionable(n) {
					flags = append(flags, "q10e")
				}
				if s.nodeIsBad(n) {
					flags = append(flags, "bad")
				}
				if s.IsGood(n) {
					flags = append(flags, "good")
				}
				if n.IsSecure() {
					flags = append(flags, "sec")
				}
				fmt.Fprintf(tw, "  %x\t%s\t%s\t%s\t%d\t%v\t%v\n",
					n.Id.Bytes(),
					n.Addr,
					prettySince(n.lastGotQuery),
					prettySince(n.lastGotResponse),
					n.numReceivesFrom,
					n.failedLastQuestionablePing,
					strings.Join(flags, ","),
				)
			}
			tw.Flush()
		}
	}
	fmt.Fprintln(w)
}

func (s *Server) numNodes() (num int) {
	s.table.forNodes(func(n *node) bool {
		num++
		return true
	})
	return
}

func (s *Server) Stats() ServerStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.stats
	ss.GoodNodes = s.numGoodNodes()
	ss.Nodes = s.numNodes()
	ss.OutstandingTransactions = s.transactions.NumActive()
	return ss
}

func (s *Server) Addr() net.Addr {
	return s.socket.LocalAddr()
}

func NewDefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		NoSecurity:    true,
		StartingNodes: func() ([]Addr, error) { return GlobalBootstrapAddrs("udp") },
		DefaultWant:   []krpc.Want{krpc.WantNodes, krpc.WantNodes6},
		SendLimiter:   DefaultSendLimiter,
	}
}

func (c *ServerConfig) InitNodeId() (deterministic bool) {
	if c.NodeId.IsZero() {
		var secure bool
		if c.Conn != nil && c.PublicIP != nil {
			c.NodeId = HashTuple(
				[]byte(c.Conn.LocalAddr().Network()),
				[]byte(c.Conn.LocalAddr().String()),
				c.PublicIP,
			)
			secure = true
			deterministic = true
		} else {
			c.NodeId = RandomNodeID()
			secure = !c.NoSecurity && c.PublicIP != nil
		}
		if secure {
			SecureNodeId(&c.NodeId, c.PublicIP)
		}
	}
	return
}

func NewServer(c *ServerConfig) (s *Server, err error) {
	if c == nil {
		c = NewDefaultServerConfig()
	}
	if c.Conn == nil {
		c.Conn, err = net.ListenPacket("udp", ":0")
		if err != nil {
			return
		}
	}
	c.InitNodeId()
	if c.Logger.IsZero() {
		c.Logger = log.Default.FilterLevel(log.Info)
	}
	c.Logger = c.Logger.WithDefaultLevel(log.Debug)

	if c.SendLimiter == nil {
		c.SendLimiter = DefaultSendLimiter
	}

	s = &Server{
		config: *c,
		tokenServer: tokenServer{
			maxIntervalDelta: 2,
			interval:         5 * time.Minute,
			secret:           make([]byte, 20),
		},
		table: table{
			k: 8,
		},
	}
	rand.Read(s.tokenServer.secret)
	s.socket = c.Conn
	s.id = int160.FromByteArray(c.NodeId)
	s.table.rootID = s.id
	s.resendDelay = s.config.QueryResendDelay
	if s.resendDelay == nil {
		s.resendDelay = defaultQueryResendDelay
	}
	go s.serveUntilClosed()
	return
}

func (s *Server) serveUntilClosed() {
	err := s.serve()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.IsSet() {
		return
	}
	if err != nil {
		s.logger().Levelf(log.Error, "dht server stopped reading: %v", err)
	}
}

func (s *Server) String() string {
	return fmt.Sprintf("dht server on %s (node id %v)", s.socket.LocalAddr(), s.id)
}

func (s *Server) processPacket(b []byte, addr Addr) {
	if len(b) < 2 || b[0] != 'd' {
		return
	}
	var d krpc.Msg
	err := bencode.Unmarshal(b, &d)
	if _, ok := err.(bencode.ErrUnusedTrailingBytes); ok {
	} else if err != nil {
		func() {
			if se, ok := err.(*bencode.SyntaxError); ok {
				if int(se.Offset) == len(b) {
					return
				}
				if int(se.Offset) < len(b) && b[se.Offset] == 0 {
					return
				}
				if se.Offset == 0 {
					return
				}
			}
			s.logger().Levelf(log.Debug, "received a bad krpc message of %d bytes from %s: %s", len(b), addr, err)
		}()
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.IsSet() {
		return
	}
	if d.Y == "q" {
		s.logger().Printf("received query %q from %v", d.Q, addr)
		s.handleQuery(addr, d)
		return
	}
	tk := transactionKey{
		RemoteAddr: addr.String(),
		T:          d.T,
	}
	if !s.transactions.Have(tk) {
		s.logger().Printf("received response for untracked transaction %q from %v", d.T, addr)
		return
	}
	t := s.transactions.Pop(tk)
	go t.handleResponse(d)
	s.updateNode(addr, d.SenderID(), !d.ReadOnly, func(n *node) {
		n.lastGotResponse = time.Now()
		n.failedLastQuestionablePing = false
		n.numReceivesFrom++
	})
}

func (s *Server) serve() error {
	var b [0x10000]byte
	for {
		n, addr, err := s.socket.ReadFrom(b[:])
		if err != nil {
			if ignoreReadFromError(err) {
				continue
			}
			return err
		}
		if n == len(b) {
			continue
		}
		if missinggo.AddrPort(addr) == 0 {
			continue
		}
		if s.closed.IsSet() {
			return errors.New("server is closed")
		}
		s.processPacketSafely(b[:n], NewAddr(addr))
	}
}

func (s *Server) processPacketSafely(b []byte, addr Addr) {
	defer func() {
		if r := recover(); r != nil {
			s.logger().Printf("dropped a DHT packet from %v that panicked: %v", addr, r)
		}
	}()
	s.processPacket(b, addr)
}

func (s *Server) AddNode(ni krpc.NodeInfo) error {
	id := int160.FromByteArray(ni.ID)
	if id.IsZero() {
		go s.Ping(ni.Addr.UDP())
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateNode(NewAddr(ni.Addr.UDP()), (*krpc.ID)(&ni.ID), true, func(*node) {})
}

func wantsContain(ws []krpc.Want, w krpc.Want) bool {
	return slices.Contains(ws, w)
}

func shouldReturnNodes(queryWants []krpc.Want, querySource net.IP) bool {
	if len(queryWants) != 0 {
		return wantsContain(queryWants, krpc.WantNodes)
	}
	return querySource.To4() != nil
}

func shouldReturnNodes6(queryWants []krpc.Want, querySource net.IP) bool {
	if len(queryWants) != 0 {
		return wantsContain(queryWants, krpc.WantNodes6)
	}
	return querySource.To4() == nil
}

func (s *Server) makeReturnNodes(target int160.T, filter func(krpc.NodeAddr) bool) []krpc.NodeInfo {
	return s.closestGoodNodeInfos(8, target, filter)
}

var krpcErrMissingArguments = krpc.Error{
	Code: krpc.ErrorCodeProtocolError,
	Msg:  "missing arguments dict",
}

func filterPeers(querySourceIp net.IP, queryWants []krpc.Want, allPeers []krpc.NodeAddr) (filtered []krpc.NodeAddr) {
	retain4 := shouldReturnNodes(queryWants, querySourceIp)
	retain6 := shouldReturnNodes6(queryWants, querySourceIp)
	for _, peer := range allPeers {
		if ip, ok := func(ip net.IP) (net.IP, bool) {
			as4 := peer.IP.To4()
			as16 := peer.IP.To16()
			switch {
			case retain4 && len(ip) == net.IPv4len:
				return ip, true
			case retain6 && len(ip) == net.IPv6len:
				return ip, true
			case retain4 && as4 != nil:
				return as4, true
			case retain6 && as16 != nil:
				return as16, true
			default:
				return nil, false
			}
		}(peer.IP); ok {
			filtered = append(filtered, krpc.NodeAddr{IP: ip, Port: peer.Port})
		}
	}
	return
}

func (s *Server) setReturnNodes(r *krpc.Return, queryMsg krpc.Msg, querySource Addr) *krpc.Error {
	if queryMsg.A == nil {
		return &krpcErrMissingArguments
	}
	target := int160.FromByteArray(queryMsg.A.InfoHash)
	if queryMsg.Q == "find_node" {
		target = int160.FromByteArray(queryMsg.A.Target)
	}
	if shouldReturnNodes(queryMsg.A.Want, querySource.IP()) {
		r.Nodes = s.makeReturnNodes(target, func(na krpc.NodeAddr) bool { return na.IP.To4() != nil })
	}
	if shouldReturnNodes6(queryMsg.A.Want, querySource.IP()) {
		r.Nodes6 = s.makeReturnNodes(target, func(krpc.NodeAddr) bool { return true })
	}
	return nil
}

func (s *Server) handleQuery(source Addr, m krpc.Msg) {
	s.updateNode(source, m.SenderID(), !m.ReadOnly, func(n *node) {
		n.lastGotQuery = time.Now()
		n.numReceivesFrom++
	})
	if s.config.OnQuery != nil {
		propagate := s.config.OnQuery(&m, source.Raw())
		if !propagate {
			return
		}
	}
	if s.config.Passive {
		return
	}
	args := m.A
	if args == nil && m.Q != "ping" {
		s.sendError(source, m.T, krpcErrMissingArguments)
		return
	}
	switch m.Q {
	case "ping":
		s.reply(source, m.T, krpc.Return{})
	case "get_peers":
		if m.A == nil {
			s.sendError(source, m.T, krpcErrMissingArguments)
			break
		}
		var r krpc.Return
		if ps := s.config.PeerStore; ps != nil {
			r.Values = filterPeers(source.IP(), m.A.Want, ps.GetPeers(peer_store.InfoHash(args.InfoHash)))
			r.Token = func() *string {
				t := s.createToken(source)
				return &t
			}()
		}
		if len(r.Values) == 0 {
			if err := s.setReturnNodes(&r, m, source); err != nil {
				s.sendError(source, m.T, *err)
				break
			}
		}
		s.reply(source, m.T, r)
	case "find_node":
		var r krpc.Return
		if err := s.setReturnNodes(&r, m, source); err != nil {
			s.sendError(source, m.T, *err)
			break
		}
		s.reply(source, m.T, r)
	case "announce_peer":
		if !s.validToken(args.Token, source) {
			return
		}

		var port int
		portOk := false
		if args.Port != nil {
			port = *args.Port
			portOk = true
		}
		if args.ImpliedPort {
			port = source.Port()
			portOk = true
		}
		if port <= 0 || port > 65535 {
			portOk = false
		}
		if h := s.config.OnAnnouncePeer; h != nil && portOk {
			go h(metainfo.Hash(args.InfoHash), source.IP(), port, portOk)
		}
		if ps := s.config.PeerStore; ps != nil && portOk {
			go ps.AddPeer(
				peer_store.InfoHash(args.InfoHash),
				krpc.NodeAddr{IP: source.IP(), Port: port},
			)
		}

		s.reply(source, m.T, krpc.Return{})
	default:
		s.sendError(source, m.T, krpc.ErrorMethodUnknown)
	}
}

func (s *Server) sendError(addr Addr, t string, e krpc.Error) {
	go func() {
		m := krpc.Msg{
			T: t,
			Y: "e",
			E: &e,
		}
		b, err := bencode.Marshal(m)
		if err != nil {
			panic(err)
		}
		s.logger().Printf("sending error to %q: %v", addr, e)
		_, err = s.writeToNode(context.Background(), b, addr, false, true)
		if err != nil {
			s.logger().Printf("error replying to %q: %v", addr, err)
		}
	}()
}

func (s *Server) reply(addr Addr, t string, r krpc.Return) {
	go func() {
		r.ID = s.id.AsByteArray()
		m := krpc.Msg{
			T:  t,
			Y:  "r",
			R:  &r,
			IP: addr.KRPC(),
		}
		b := bencode.MustMarshal(m)
		log.Fmsg("replying to %q", addr).Log(s.logger())
		_, err := s.writeToNode(context.Background(), b, addr, s.config.WaitToReply, true)
		if err != nil {
			s.config.Logger.Printf("error replying to %s: %s", addr, err)
		}
	}()
}

func (s *Server) addNode(n *node) error {
	if s.nodeIsBad(n) {
		return errors.New("node is bad")
	}
	b := s.table.bucketForID(n.Id)
	if b.Len() >= s.table.k {
		if b.EachNode(func(bn *node) bool {
			if s.nodeIsBad(bn) || (s.IsGood(n) && bn.lastGotResponse.IsZero()) {
				s.table.dropNode(bn)
			}
			return b.Len() >= s.table.k
		}) {
			return errors.New("no room in bucket")
		}
	}
	if err := s.table.addNode(n); err != nil {
		panic(fmt.Sprintf("expected to add node: %s", err))
	}
	return nil
}

func (s *Server) NodeRespondedToPing(addr Addr, id int160.T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == s.id {
		return
	}
	b := s.table.bucketForID(id)
	if b.GetNode(addr, id) == nil {
		return
	}
	b.lastChanged = time.Now()
}

func (s *Server) updateNode(addr Addr, id *krpc.ID, tryAdd bool, update func(*node)) error {
	if id == nil {
		return errors.New("id is nil")
	}
	int160Id := int160.FromByteArray(*id)
	n := s.table.getNode(addr, int160Id)
	missing := n == nil
	if missing {
		if !tryAdd {
			return errors.New("node not present and add flag false")
		}
		if int160Id == s.id {
			return errors.New("can't store own id in routing table")
		}
		n = &node{nodeKey: nodeKey{
			Id:   int160Id,
			Addr: addr,
		}}
	}
	update(n)
	if !missing {
		return nil
	}
	return s.addNode(n)
}

func (s *Server) nodeIsBad(n *node) bool {
	return s.nodeErr(n) != nil
}

func (s *Server) nodeErr(n *node) error {
	if n.Id == s.id {
		return errors.New("is self")
	}
	if n.Id.IsZero() {
		return errors.New("has zero id")
	}
	if !(s.config.NoSecurity || n.IsSecure()) {
		return errors.New("not secure")
	}
	if n.failedLastQuestionablePing {
		return errors.New("didn't respond to last questionable node ping")
	}
	return nil
}

func (s *Server) writeToNode(ctx context.Context, b []byte, node Addr, wait, rate bool) (wrote bool, err error) {
	func() {
		s.mu.RLock()
		defer s.mu.RUnlock()
		if s.closed.IsSet() {
			err = errors.New("server is closed")
			return
		}
	}()
	if err != nil {
		return
	}
	if rate {
		if wait {
			err = s.config.SendLimiter.Wait(ctx)
			if err != nil {
				err = fmt.Errorf("waiting for rate-limit token: %w", err)
				return false, err
			}
		} else {
			if !s.config.SendLimiter.Allow() {
				return false, errors.New("rate limit exceeded")
			}
		}
	}
	n, err := s.socket.WriteTo(b, node.Raw())
	if rate {
	} else {
	}
	if err != nil {
		if rate {
			s.config.SendLimiter.AllowN(time.Now(), -1)
		}
		err = fmt.Errorf("error writing %d bytes to %s: %s", len(b), node, err)
		return
	}
	wrote = true
	if n != len(b) {
		err = io.ErrShortWrite
		return
	}
	return
}

func (s *Server) nextTransactionID() string {
	var b [4]byte
	rand.Read(b[:])
	return string(b[:])
}

func (s *Server) deleteTransaction(k transactionKey) {
	if s.transactions.Have(k) {
		s.transactions.Pop(k)
	}
}

func (s *Server) addTransaction(k transactionKey, t *transaction) {
	s.transactions.Add(k, t)
}

func (s *Server) ID() [20]byte {
	return s.id.AsByteArray()
}

func (s *Server) createToken(addr Addr) string {
	return s.tokenServer.CreateToken(addr)
}

func (s *Server) validToken(token string, addr Addr) bool {
	return s.tokenServer.ValidToken(token, addr)
}

type numWrites int

func (s *Server) makeQueryBytes(q string, a krpc.MsgArgs, t string) []byte {
	a.ID = s.ID()
	m := krpc.Msg{
		T: t,
		Y: "q",
		Q: q,
		A: &a,
	}
	if s.config.Passive {
		m.ReadOnly = true
	}
	b, err := bencode.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

type QueryResult struct {
	Reply  krpc.Msg
	Writes numWrites
	Err    error
}

func (qr QueryResult) ToError() error {
	if qr.Err != nil {
		return qr.Err
	}
	e := qr.Reply.Error()
	if e != nil {
		return e
	}
	return nil
}

func (me QueryResult) TraversalQueryResult(addr krpc.NodeAddr) (ret traversal.QueryResult) {
	r := me.Reply.R
	if r == nil {
		return
	}
	ret.ResponseFrom = &krpc.NodeInfo{
		Addr: addr,
		ID:   r.ID,
	}
	ret.Nodes = r.Nodes
	ret.Nodes6 = r.Nodes6
	if r.Token != nil {
		ret.ClosestData = *r.Token
	}
	return
}

type QueryRateLimiting struct {
	NotFirst      bool
	NotAny        bool
	WaitOnRetries bool
	NoWaitFirst   bool
}

type QueryInput struct {
	MsgArgs      krpc.MsgArgs
	RateLimiting QueryRateLimiting
	NumTries     int
}

func (s *Server) Query(ctx context.Context, addr Addr, q string, input QueryInput) (ret QueryResult) {
	if input.NumTries == 0 {
		input.NumTries = defaultMaxQuerySends
	}
	defer func(started time.Time) {
		s.logger().WithDefaultLevel(log.Debug).WithValues(q).Printf(
			"Query(%v) returned after %v (err=%v, reply.Y=%v, reply.E=%v, writes=%v)",
			q, time.Since(started), ret.Err, ret.Reply.Y, ret.Reply.E, ret.Writes)
	}(time.Now())
	replyChan := make(chan krpc.Msg, 1)
	t := &transaction{
		onResponse: func(m krpc.Msg) {
			replyChan <- m
		},
	}
	tk := transactionKey{
		RemoteAddr: addr.String(),
	}
	s.mu.Lock()
	tk.T = s.nextTransactionID()
	for s.transactions.Have(tk) {
		tk.T = s.nextTransactionID()
	}
	tid := tk.T
	s.stats.OutboundQueriesAttempted++
	s.addTransaction(tk, t)
	s.mu.Unlock()
	sendErr := make(chan error, 1)
	sendCtx, cancelSend := context.WithCancel(pprof.WithLabels(ctx, pprof.Labels("q", q)))
	go func() {
		err := s.transactionQuerySender(
			sendCtx,
			s.makeQueryBytes(q, input.MsgArgs, tid),
			&ret.Writes,
			addr,
			input.RateLimiting,
			input.NumTries)
		if err != nil {
			sendErr <- err
		}
		close(sendErr)
	}()
	select {
	case ret.Reply = <-replyChan:
	case <-ctx.Done():
		ret.Err = ctx.Err()
	case ret.Err = <-sendErr:
	}
	cancelSend()
	<-sendErr
	s.mu.Lock()
	s.deleteTransaction(tk)
	s.mu.Unlock()
	return
}

func (s *Server) transactionQuerySender(
	sendCtx context.Context,
	b []byte,
	writes *numWrites,
	addr Addr,
	rateLimiting QueryRateLimiting,
	numTries int,
) error {
	err := transactionSender(
		sendCtx,
		func() error {
			wrote, err := s.writeToNode(sendCtx, b, addr,
				func() bool {
					if *writes == 0 {
						return !rateLimiting.NoWaitFirst
					} else {
						return rateLimiting.WaitOnRetries
					}
				}(),
				func() bool {
					if rateLimiting.NotAny {
						return false
					}
					if *writes == 0 {
						return !rateLimiting.NotFirst
					}
					return true
				}(),
			)
			if wrote {
				*writes++
			}
			return err
		},
		s.resendDelay,
		numTries,
	)
	if err != nil {
		return err
	}
	select {
	case <-sendCtx.Done():
		err = sendCtx.Err()
	case <-time.After(s.resendDelay()):
		err = TransactionTimeout
	}
	return fmt.Errorf("after %v tries: %w", numTries, err)
}

func (s *Server) PingQueryInput(node *net.UDPAddr, qi QueryInput) QueryResult {
	addr := NewAddr(node)
	res := s.Query(context.TODO(), addr, "ping", qi)
	if res.Err == nil {
		id := res.Reply.SenderID()
		if id != nil {
			s.NodeRespondedToPing(addr, id.Int160())
		}
	}
	return res
}

func (s *Server) Ping(node *net.UDPAddr) QueryResult {
	return s.PingQueryInput(node, QueryInput{})
}

func (s *Server) announcePeer(
	ctx context.Context,
	node Addr, infoHash int160.T, port int, token string, impliedPort bool, rl QueryRateLimiting,
) (
	ret QueryResult,
) {
	if port == 0 && !impliedPort {
		ret.Err = errors.New("no port specified")
		return
	}
	ret = s.Query(
		ctx, node, "announce_peer",
		QueryInput{
			MsgArgs: krpc.MsgArgs{
				ImpliedPort: impliedPort,
				InfoHash:    infoHash.AsByteArray(),
				Port:        &port,
				Token:       token,
			},
			RateLimiting: rl,
		})
	if ret.Err != nil {
		return
	}
	if krpcError := ret.Reply.Error(); krpcError != nil {
		ret.Err = krpcError
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.SuccessfulOutboundAnnouncePeerQueries++
	return
}

func (s *Server) FindNode(addr Addr, targetID int160.T, rl QueryRateLimiting) (ret QueryResult) {
	ret = s.Query(context.TODO(), addr, "find_node", QueryInput{
		MsgArgs: krpc.MsgArgs{
			Target: targetID.AsByteArray(),
			Want:   s.config.DefaultWant,
		},
		RateLimiting: rl,
	})
	return
}

func (s *Server) NumNodes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.numNodes()
}

func (s *Server) Nodes() (nis []krpc.NodeInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notBadNodes()
}

func (s *Server) notBadNodes() (nis []krpc.NodeInfo) {
	s.table.forNodes(func(n *node) bool {
		if s.nodeIsBad(n) {
			return true
		}
		nis = append(nis, krpc.NodeInfo{
			Addr: n.Addr.KRPC(),
			ID:   n.Id.AsByteArray(),
		})
		return true
	})
	return
}

func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed.Set()
	go s.socket.Close()
}

func (s *Server) GetPeers(
	ctx context.Context,
	addr Addr,
	infoHash int160.T,
	scrape bool,
	rl QueryRateLimiting,
) (ret QueryResult) {
	args := krpc.MsgArgs{
		InfoHash: infoHash.AsByteArray(),
		Want:     s.config.DefaultWant,
	}
	if scrape {
		args.Scrape = 1
	}
	ret = s.Query(ctx, addr, "get_peers", QueryInput{
		MsgArgs:      args,
		RateLimiting: rl,
	})
	m := ret.Reply
	if m.R != nil {
		if m.R.Token == nil {
		} else if len(*m.R.Token) == 0 {
		} else {
		}
	}
	return
}

func (s *Server) closestGoodNodeInfos(
	k int,
	targetID int160.T,
	filter func(krpc.NodeAddr) bool,
) (
	ret []krpc.NodeInfo,
) {
	for _, n := range s.closestNodes(k, targetID, func(n *node) bool {
		return s.IsGood(n) && filter(n.NodeInfo().Addr)
	}) {
		ret = append(ret, n.NodeInfo())
	}
	return
}

func (s *Server) closestNodes(k int, target int160.T, filter func(*node) bool) []*node {
	return s.table.closestNodes(k, target, filter)
}

const minStartingTableNodes = 8

func (s *Server) TraversalStartingNodes() (nodes []addrMaybeId, err error) {
	s.mu.RLock()
	s.table.forNodes(func(n *node) bool {
		if !s.nodeIsBad(n) {
			nodes = append(nodes, addrMaybeId{
				Addr: n.Addr.KRPC().ToNodeAddrPort(),
				Id:   generics.Some(n.Id)})
		}
		return true
	})
	s.mu.RUnlock()
	if len(nodes) >= minStartingTableNodes {
		return
	}
	if s.config.StartingNodes != nil {
		s.logger().Levelf(log.Debug, "falling back on starting nodes")
		addrs, err := s.config.StartingNodes()
		if err != nil {
			if len(nodes) > 0 {
				return nodes, nil
			}
			return nil, fmt.Errorf("getting starting nodes: %w", err)
		}
		for _, a := range addrs {
			nodes = append(nodes, addrMaybeId{Addr: a.KRPC().ToNodeAddrPort()})
		}
	}
	if len(nodes) == 0 {
		err = errors.New("no initial nodes")
	}
	return
}

func (s *Server) AddNodesFromFile(fileName string) (added int, err error) {
	ns, err := ReadNodesFromFile(fileName)
	if err != nil {
		return
	}
	for _, n := range ns {
		if s.AddNode(n) == nil {
			added++
		}
	}
	return
}

func (s *Server) logger() log.Logger {
	return s.config.Logger
}

func (s *Server) PeerStore() peer_store.Interface {
	return s.config.PeerStore
}

func (s *Server) shouldStopRefreshingBucket(bucketIndex int) bool {
	if s.closed.IsSet() {
		return true
	}
	b := &s.table.buckets[bucketIndex]
	return b.Len() == s.table.K() && b.EachNode(func(n *node) bool {
		return !s.nodeIsBad(n)
	})
}

func (s *Server) refreshBucket(bucketIndex int) *traversal.Stats {
	s.mu.RLock()
	id := s.table.randomIdForBucket(bucketIndex)
	op := traversal.Start(traversal.OperationInput{
		Target: id.AsByteArray(),
		Alpha:  3,
		K:      s.table.K(),
		DoQuery: func(ctx context.Context, addr krpc.NodeAddr) traversal.QueryResult {
			res := s.FindNode(NewAddr(addr.UDP()), id, QueryRateLimiting{})
			err := res.Err
			if err != nil && !errors.Is(err, TransactionTimeout) {
				s.logger().Levelf(log.Debug, "error doing find node while refreshing bucket: %v", err)
			}
			return res.TraversalQueryResult(addr)
		},
		NodeFilter: s.TraversalNodeFilter,
	})
	defer func() {
		s.mu.RUnlock()
		op.Stop()
		<-op.Stopped()
	}()
	b := &s.table.buckets[bucketIndex]
wait:
	for {
		if s.shouldStopRefreshingBucket(bucketIndex) {
			break wait
		}
		op.AddNodes(types.AddrMaybeIdSliceFromNodeInfoSlice(s.notBadNodes()))
		bucketChanged := b.changed.Signaled()
		serverClosed := s.closed.C()
		s.mu.RUnlock()
		select {
		case <-op.Stalled():
			s.mu.RLock()
			break wait
		case <-bucketChanged:
		case <-serverClosed:
		}
		s.mu.RLock()
	}
	return op.Stats()
}

func (s *Server) shouldBootstrap() bool {
	return s.lastBootstrap.IsZero() || time.Since(s.lastBootstrap) > 30*time.Minute
}

func (s *Server) shouldBootstrapUnlocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.shouldBootstrap()
}

func (s *Server) pingQuestionableNodesInBucket(bucketIndex int) {
	b := &s.table.buckets[bucketIndex]
	var wg sync.WaitGroup
	b.EachNode(func(n *node) bool {
		if s.IsQuestionable(n) {
			wg.Go(func() {
				err := s.questionableNodePing(context.TODO(), n.Addr, n.Id.AsByteArray()).Err
				if err != nil {
					s.logger().WithDefaultLevel(log.Debug).Printf("error pinging questionable node in bucket %v: %v", bucketIndex, err)
				}
			})
		}
		return true
	})
	s.mu.RUnlock()
	wg.Wait()
	s.mu.RLock()
}

func (s *Server) TableMaintainer() {
	logger := s.logger()
	for {
		if s.shouldBootstrapUnlocked() {
			stats, err := s.Bootstrap()
			if err != nil {
				logger.Levelf(log.Error, "error bootstrapping during bucket refresh: %v", err)
			}
			logger.Levelf(log.Debug, "bucket refresh bootstrap stats: %v", stats)
		}
		s.mu.RLock()
		for i := range s.table.buckets {
			s.pingQuestionableNodesInBucket(i)
			if s.shouldStopRefreshingBucket(i) {
				continue
			}
			logger.Levelf(log.Debug, "refreshing bucket %v", i)
			s.mu.RUnlock()
			stats := s.refreshBucket(i)
			logger.Levelf(log.Debug, "finished refreshing bucket %v: %v", i, stats)
			s.mu.RLock()
			if !s.shouldStopRefreshingBucket(i) {
				break
			}
		}
		s.mu.RUnlock()
		select {
		case <-s.closed.LockedChan(&s.mu):
			return
		case <-time.After(time.Minute):
		}
	}
}

func (s *Server) questionableNodePing(ctx context.Context, addr Addr, id krpc.ID) QueryResult {
	res := s.Query(ctx, addr, "ping", QueryInput{
		RateLimiting: QueryRateLimiting{
			WaitOnRetries: true,
		},
		NumTries: 3,
	})
	if res.Err == nil && res.Reply.R != nil {
		s.NodeRespondedToPing(addr, res.Reply.R.ID.Int160())
	} else {
		s.mu.Lock()
		s.updateNode(addr, &id, false, func(n *node) {
			n.failedLastQuestionablePing = true
		})
		s.mu.Unlock()
	}
	return res
}

func (s *Server) TraversalNodeFilter(node addrMaybeId) bool {
	if !validNodeAddr(node.Addr.UDP()) {
		return false
	}
	if !node.Id.Ok {
		return true
	}
	return s.config.NoSecurity || NodeIdSecure(node.Id.Value.AsByteArray(), node.Addr.IP())
}

func validNodeAddr(addr net.Addr) bool {
	ua := addr.(*net.UDPAddr)
	if ua.Port == 0 {
		return false
	}
	if ip4 := ua.IP.To4(); ip4 != nil && ip4[0] == 0 {
		return false
	}
	if ua.IP.IsUnspecified() || ua.IP.IsMulticast() || ua.IP.Equal(net.IPv4bcast) {
		return false
	}
	return true
}
