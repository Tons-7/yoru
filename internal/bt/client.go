package bt

import (
	"bufio"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/anacrolix/chansync"
	"github.com/anacrolix/chansync/events"
	. "github.com/anacrolix/generics"
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2"
	"github.com/anacrolix/missinggo/v2/bitmap"
	"github.com/anacrolix/missinggo/v2/panicif"
	"github.com/cespare/xxhash"
	"github.com/dustin/go-humanize"
	gbtree "github.com/google/btree"
	"golang.org/x/time/rate"
	"yoru/internal/bt/dht"
	"yoru/internal/bt/dht/krpc"
	"yoru/internal/bt/internal/amortize"
	"yoru/internal/bt/internal/extracmp"

	"yoru/internal/bt/bencode"
	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/mse"
	pp "yoru/internal/bt/peer_protocol"
	"yoru/internal/bt/storage"
	"yoru/internal/bt/types/infohash"
	infohash_v2 "yoru/internal/bt/types/infohash-v2"
)

const webseedRequestUpdateTimerInterval = 5 * time.Second

type Client struct {
	connStats AllConnStats
	counters  TorrentStatCounters

	_mu                lockWithDeferreds
	unlockHandlers     clientUnlockHandlers
	check              amortize.Value
	roaringIntIterator roaring.IntIterator
	event              sync.Cond
	closed             chansync.SetOnce

	config  *ClientConfig
	logger  log.Logger
	slogger *slog.Logger

	peerID         PeerID
	defaultStorage *storage.Client
	onClose        []func()
	dialers        []Dialer
	listeners      []Listener
	closeLast      []func()
	dhtServers     []DhtServer

	dopplegangerAddrs   map[string]struct{}
	badPeerIPs          map[netip.Addr]time.Time
	torrents            map[*Torrent]struct{}
	torrentsByShortHash map[InfoHash]*Torrent

	pieceRequestOrder map[clientPieceRequestOrderKeySumType]clientPieceRequestOrderValue

	acceptLimiter map[ipStr]int
	numHalfOpen   int

	regularTrackerAnnounceDispatcher regularTrackerAnnounceDispatcher

	numWebSeedRequests map[webseedHostKeyHandle]int

	httpClient *http.Client

	clientHolepunchAddrSets

	defaultLocalLtepProtocolMap LocalLtepProtocolMap

	upnpMappings []*upnpMapping

	clientWebseedState

	activePieceHashers int
}

type clientWebseedState struct {
	webseedRequestTimer   *time.Timer
	webseedUpdateReason   updateRequestReason
	activeWebseedRequests map[webseedUniqueRequestKey]*webseedRequest
	aprioriMap            map[webseedUniqueRequestKey]aprioriMapValue
	heapSlice             []webseedRequestHeapElem
}

type ipStr string

func (cl *Client) BadPeerIPs() (ips []string) {
	cl.rLock()
	ips = cl.badPeerIPsLocked()
	cl.rUnlock()
	return
}

func (cl *Client) badPeerIPsLocked() (ips []string) {
	now := time.Now()
	for k, until := range cl.badPeerIPs {
		if now.Before(until) {
			ips = append(ips, k.String())
		}
	}
	return
}

func (cl *Client) PeerID() PeerID {
	return cl.peerID
}

func (cl *Client) LocalPort() (port int) {
	for i := 0; i < len(cl.listeners); i += 1 {
		if port = addrPortOrZero(cl.listeners[i].Addr()); port != 0 {
			return
		}
	}
	return
}

func (cl *Client) WriteStatus(_w io.Writer) {
	cl.rLock()
	defer cl.rUnlock()
	w := bufio.NewWriter(_w)
	defer w.Flush()
	fmt.Fprintf(w, "Listen port: %d\n", cl.LocalPort())
	fmt.Fprintf(w, "Peer ID: %+q\n", cl.PeerID())
	fmt.Fprintf(w, "Extension bits: %v\n", cl.config.Extensions)
	fmt.Fprintf(w, "Announce key: %x\n", cl.announceKey())
	fmt.Fprintf(w, "Banned IPs: %d\n", len(cl.badPeerIPsLocked()))
	cl.eachDhtServer(func(s DhtServer) {
		fmt.Fprintf(w, "%s DHT server at %s:\n", s.Addr().Network(), s.Addr().String())
		writeDhtServerStatus(w, s)
	})
	dumpStats(w, cl.statsLocked())
	torrentsSlice := cl.torrentsAsSlice()
	incomplete := 0
	for _, t := range torrentsSlice {
		if !t.Complete().Bool() {
			incomplete++
		}
	}
	fmt.Fprintf(w, "# Torrents: %d (%v incomplete)\n", len(torrentsSlice), incomplete)
	fmt.Fprintln(w)
	slices.SortFunc(torrentsSlice, func(a, b *Torrent) int {
		return cmp.Or(
			extracmp.CompareBool(a.haveInfo(), b.haveInfo()),
			func() int {
				if a.haveInfo() && b.haveInfo() {
					return -cmp.Compare(a.bytesLeft(), b.bytesLeft())
				} else {
					return 0
				}
			}(),
			cmp.Compare(a.canonicalShortInfohash().AsString(), b.canonicalShortInfohash().AsString()),
		)
	})
	for _, t := range torrentsSlice {
		if t.name() == "" {
			fmt.Fprint(w, "<unknown name>")
		} else {
			fmt.Fprint(w, t.name())
		}
		fmt.Fprint(w, "\n")
		if t.info != nil {
			fmt.Fprintf(
				w,
				"%f%% of %d bytes (%s)",
				100*t.progressUnitFloat(),
				t.length(),
				humanize.Bytes(uint64(t.length())))
		} else {
			w.WriteString("<missing metainfo>")
		}
		fmt.Fprint(w, "\n")
		t.writeStatus(w)
		fmt.Fprintln(w)
	}
	cl.writeRegularTrackerAnnouncerStatus(w)
}

func (cl *Client) writeRegularTrackerAnnouncerStatus(w io.Writer) {
	cl.regularTrackerAnnounceDispatcher.writeStatus(w)
}

func (cl *Client) getLoggers() (log.Logger, *slog.Logger) {
	logger := cl.config.Logger
	slogger := cl.config.Slogger
	if slogger == nil {
		if logger.IsZero() {
			logger = log.Default
		}
		if cl.config.Debug {
			logger = logger.WithFilterLevel(log.Debug)
		}
		logger = logger.WithValues(cl)
		return logger, logger.Slogger()
	}
	if logger.IsZero() {
		logger = log.NewLogger().WithDefaultLevel(log.Debug)
		logger.SetHandlers(log.SlogHandlerAsHandler{SlogHandler: slogger.Handler()})
	}
	return logger, slogger
}

func (cl *Client) initLogger() {
	cl.logger, cl.slogger = cl.getLoggers()
}

func (cl *Client) announceKey() int32 {
	return int32(binary.BigEndian.Uint32(cl.peerID[16:20]))
}

func (cl *Client) init(cfg *ClientConfig) {
	cl.unlockHandlers.init()
	cl.config = cfg
	cl._mu.client = cl
	cl.initLogger()
	cl.regularTrackerAnnounceDispatcher.init(cl)
	cfg.setRateLimiterBursts()
	g.MakeMap(&cl.dopplegangerAddrs)
	g.MakeMap(&cl.torrentsByShortHash)
	g.MakeMap(&cl.torrents)
	cl.torrentsByShortHash = make(map[metainfo.Hash]*Torrent)
	cl.event.L = cl.locker()
	cl.httpClient = &http.Client{
		Transport: &http.Transport{
			DialContext:     cfg.HTTPDialContext,
			MaxConnsPerHost: 10,
		},
	}
	cfg.MetainfoSourcesClient = cmp.Or(cfg.MetainfoSourcesClient, cl.httpClient)
	cl.defaultLocalLtepProtocolMap = makeBuiltinLtepProtocols(!cfg.DisablePEX)
	g.MakeMap(&cl.numWebSeedRequests)

	go cl.acceptLimitClearer()

	storageImpl := cfg.DefaultStorage
	if storageImpl == nil {
		storageImplCloser := storage.NewFile(cfg.DataDir)
		cl.onClose = append(cl.onClose, func() {
			if err := storageImplCloser.Close(); err != nil {
				cl.logger.Printf("error closing default storage: %s", err)
			}
		})
		storageImpl = storageImplCloser
	}
	cl.defaultStorage = storage.NewClient(storageImpl)

	if cfg.PeerID != "" {
		missinggo.CopyExact(&cl.peerID, cfg.PeerID)
	} else {
		o := copy(cl.peerID[:], cfg.Bep20)
		_, err := rand.Read(cl.peerID[o:])
		if err != nil {
			panic("error generating peer id")
		}
	}

	cl.webseedRequestTimer = time.AfterFunc(webseedRequestUpdateTimerInterval, cl.updateWebseedRequestsTimerFunc)
}

func NewClient(cfg *ClientConfig) (cl *Client, err error) {
	if cfg == nil {
		cfg = NewDefaultClientConfig()
		cfg.ListenPort = 0
	}
	cl = &Client{}
	cl.init(cfg)
	defer func() {
		if err != nil {
			cl.Close()
			cl = nil
		}
	}()
	builtinListenNetworks := cl.listenNetworks()
	sockets, err := listenAll(
		builtinListenNetworks,
		cl.config.ListenHost,
		cl.config.ListenPort,
		cl.logger,
	)
	if err != nil {
		return
	}
	if len(sockets) == 0 && len(builtinListenNetworks) != 0 {
		err = fmt.Errorf("no sockets created for networks %v", builtinListenNetworks)
		return
	}

	cl.LocalPort()

	for _, s := range sockets {
		cl.closeLast = append(cl.closeLast, func() { s.Close() })
		if peerNetworkEnabled(parseNetworkString(s.Addr().Network()), cl.config) {
			if cl.config.DialForPeerConns {
				cl.dialers = append(cl.dialers, s)
			}
			cl.listeners = append(cl.listeners, s)
			if cl.config.AcceptPeerConnections {
				go cl.acceptConnections(s)
			}
		}
	}

	if !cfg.NoDefaultPortForwarding {
		go cl.forwardPort()
	}
	if !cfg.NoDHT {
		var servers []*dht.Server
		for _, s := range sockets {
			if pc, ok := s.(net.PacketConn); ok {
				ds, err := cl.NewDhtServer(pc)
				if err != nil {
					panic(err)
				}
				cl.dhtServers = append(cl.dhtServers, DhtServerWrapper{ds})
				cl.closeLast = append(cl.closeLast, func() { ds.Close() })
				servers = append(servers, ds)
			}
		}
		if file := cfg.DhtNodesFile; file != "" && len(servers) > 0 {
			loadDhtNodes(file, servers)
			cl.closeLast = append(cl.closeLast, func() { saveDhtNodes(file, servers) })
			go cl.keepSavingDhtNodes(file, servers)
		}
	}

	err = cl.checkConfig()
	return
}

func (cl *Client) SetUploads(upload, stopSeedingWhenComplete bool) {
	cl.lock()
	defer cl.unlock()
	cl.config.NoUpload = !upload
	cl.config.StopSeedingWhenComplete = stopSeedingWhenComplete
	for t := range cl.torrents {
		for c := range t.conns {
			c.tickleWriter()
		}
	}
}

func (cl *Client) AddDialer(d Dialer) {
	cl.lock()
	defer cl.unlock()
	cl.dialers = append(cl.dialers, d)
	for t := range cl.torrents {
		t.openNewConns()
	}
}

func (cl *Client) Listeners() []Listener {
	return cl.listeners
}

func (cl *Client) AddListener(l Listener) {
	cl.lock()
	cl.listeners = append(cl.listeners, l)
	cl.unlock()
	if cl.config.AcceptPeerConnections {
		go cl.acceptConnections(l)
	}
}

func (cl *Client) listenOnNetwork(n network) bool {
	if n.Ipv4 && cl.config.DisableIPv4 {
		return false
	}
	if n.Ipv6 && cl.config.DisableIPv6 {
		return false
	}
	if n.Tcp && cl.config.DisableTCP {
		return false
	}
	if n.Udp && cl.config.DisableUTP && cl.config.NoDHT {
		return false
	}
	return true
}

func (cl *Client) listenNetworks() (ns []network) {
	for _, n := range allPeerNetworks {
		if cl.listenOnNetwork(n) {
			ns = append(ns, n)
		}
	}
	return
}

func (cl *Client) NewDhtServer(conn net.PacketConn) (s *dht.Server, err error) {
	logger := cl.logger.WithNames("dht", conn.LocalAddr().String())
	cfg := dht.ServerConfig{
		Conn:           conn,
		OnAnnouncePeer: cl.onDHTAnnouncePeer,
		StartingNodes:  cl.config.DhtStartingNodes(conn.LocalAddr().Network()),
		Logger:         logger,
	}
	s, err = dht.NewServer(&cfg)
	if err == nil {
		go s.TableMaintainer()
	}
	return
}

func (cl *Client) Closed() events.Done {
	return cl.closed.Done()
}

func (cl *Client) eachDhtServer(f func(DhtServer)) {
	for _, ds := range cl.dhtServers {
		f(ds)
	}
}

const utpFinGrace = 250 * time.Millisecond

func (cl *Client) Close() (errs []error) {
	cl.closed.Set()
	if cl.webseedRequestTimer != nil {
		cl.webseedRequestTimer.Stop()
	}
	var closeGroup sync.WaitGroup
	cl.lock()
	utpPeers := false
	for t := range cl.torrents {
		for pc := range t.conns {
			utpPeers = utpPeers || parseNetworkString(pc.Network).Udp
		}
		cl.dropTorrent(t, &closeGroup)
	}
	panicif.NotZero(len(cl.torrents))
	panicif.NotZero(len(cl.torrentsByShortHash))
	unmapping := cl.clearPortMappings()
	for i := range cl.onClose {
		cl.onClose[len(cl.onClose)-1-i]()
	}
	cl.unlock()
	cl.event.Broadcast()
	closeGroup.Wait()
	if utpPeers {
		time.Sleep(utpFinGrace)
	}
	for i := range cl.closeLast {
		cl.closeLast[len(cl.closeLast)-1-i]()
	}
	waitBriefly(unmapping, 3*time.Second)
	time.AfterFunc(5*time.Second, func() {
		cl.lock()
		defer cl.unlock()
		cl.regularTrackerAnnounceDispatcher.close()
	})
	return
}

func (cl *Client) wantConns() bool {
	if cl.config.AlwaysWantConns {
		return true
	}
	for t := range cl.torrents {
		if t.wantIncomingConns() {
			return true
		}
	}
	return false
}

func (cl *Client) rejectAccepted(conn net.Conn) error {
	if !cl.wantConns() {
		return errors.New("don't want conns right now")
	}
	ra := conn.RemoteAddr()
	if rip := addrIpOrNil(ra); rip != nil {
		if cl.config.DisableIPv4 && len(rip) == net.IPv4len {
			return errors.New("ipv4 disabled")
		}
		if cl.config.DisableIPv6 && len(rip) == net.IPv6len && rip.To4() == nil {
			return errors.New("ipv6 disabled")
		}
		if cl.rateLimitAccept(rip) {
			return errors.New("source IP accepted rate limited")
		}
		if cl.badPeerIPPort(rip, missinggo.AddrPort(ra)) {
			return errors.New("bad source addr")
		}
	}
	return nil
}

func (cl *Client) acceptConnections(l Listener) {
	var acceptBackoff time.Duration
	for {
		conn, err := l.Accept()
		if err == nil {
			holepunchAddr, holepunchErr := addrPortFromPeerRemoteAddr(conn.RemoteAddr())
			if holepunchErr == nil {
				cl.lock()
				if g.MapContains(cl.undialableWithoutHolepunch, holepunchAddr) {
					setAdd(&cl.accepted, holepunchAddr)
				}
				if g.MapContains(
					cl.undialableWithoutHolepunchDialedAfterHolepunchConnect,
					holepunchAddr,
				) {
					setAdd(&cl.probablyOnlyConnectedDueToHolepunch, holepunchAddr)
				}
				cl.unlock()
			}
		}
		cl.rLock()
		closed := cl.closed.IsSet()
		var reject error
		if !closed && conn != nil {
			reject = cl.rejectAccepted(conn)
		}
		cl.rUnlock()
		if closed {
			if conn != nil {
				conn.Close()
			}
			return
		}
		if err != nil {
			log.Fmsg("error accepting connection: %s", err).LogLevel(log.Debug, cl.logger)
			if errors.Is(err, net.ErrClosed) {
				return
			}
			acceptBackoff = min(max(acceptBackoff*2, 5*time.Millisecond), time.Second)
			time.Sleep(acceptBackoff)
			continue
		}
		acceptBackoff = 0
		go func() {
			if reject != nil {
				cl.logger.LazyLog(log.Debug, func() log.Msg {
					return log.Fmsg("rejecting accepted conn: %v", reject)
				})
				conn.Close()
			} else {
				go cl.incomingConnection(conn)
			}
			cl.logger.LazyLog(log.Debug, func() log.Msg {
				return log.Fmsg("accepted %q connection at %q from %q",
					l.Addr().Network(),
					conn.LocalAddr(),
					conn.RemoteAddr(),
				)
			})
		}()
	}
}

func regularNetConnPeerConnConnString(nc net.Conn) string {
	return fmt.Sprintf("%s-%s", nc.LocalAddr(), nc.RemoteAddr())
}

func (cl *Client) incomingConnection(nc net.Conn) {
	defer nc.Close()
	if tc, ok := nc.(*net.TCPConn); ok {
		tc.SetLinger(0)
	}
	remoteAddr, _ := tryIpPortFromNetAddr(nc.RemoteAddr())
	c := cl.newConnection(
		nc,
		newConnectionOpts{
			outgoing:        false,
			remoteAddr:      nc.RemoteAddr(),
			localPublicAddr: cl.publicAddr(remoteAddr.IP),
			network:         nc.RemoteAddr().Network(),
			connString:      regularNetConnPeerConnConnString(nc),
		})
	c.Discovery = PeerSourceIncoming
	cl.runReceivedConn(c)

	cl.lock()
	c.close()
	cl.unlock()
}

func (cl *Client) Torrent(ih metainfo.Hash) (t *Torrent, ok bool) {
	cl.rLock()
	defer cl.rUnlock()
	t, ok = cl.torrentsByShortHash[ih]
	return
}

type DialResult struct {
	Conn   net.Conn
	Dialer Dialer
}

func countDialResult(err error) {
	if err == nil {
	} else {
	}
}

func reducedDialTimeout(minDialTimeout, max time.Duration, halfOpenLimit, pendingPeers int) (ret time.Duration) {
	ret = max / time.Duration((pendingPeers+halfOpenLimit)/halfOpenLimit)
	if ret < minDialTimeout {
		ret = minDialTimeout
	}
	return
}

func (cl *Client) dopplegangerAddr(addr string) bool {
	_, ok := cl.dopplegangerAddrs[addr]
	return ok
}

func dialFromSocket(ctx context.Context, s Dialer, addr string) net.Conn {
	c, err := s.Dial(ctx, addr)
	if err != nil {
		log.ContextLogger(ctx).Levelf(log.Debug, "error dialing %q: %v", addr, err)
	}
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetLinger(0)
	}
	countDialResult(err)
	return c
}

func (cl *Client) noLongerHalfOpen(t *Torrent, addr string, attemptKey outgoingConnAttemptKey) {
	path := t.getHalfOpenPath(addr, attemptKey)
	if !path.Exists() {
		panic("should exist")
	}
	path.Delete()
	cl.numHalfOpen--
	if cl.numHalfOpen < 0 {
		panic("should not be possible")
	}
	for t := range cl.torrents {
		t.openNewConns()
	}
}

func (cl *Client) initiateProtocolHandshakes(
	ctx context.Context,
	nc net.Conn,
	t *Torrent,
	encryptHeader bool,
	newConnOpts newConnectionOpts,
) (
	c *PeerConn, err error,
) {
	c = cl.newConnection(nc, newConnOpts)
	c.headerEncrypted = encryptHeader
	ctx, cancel := context.WithTimeout(ctx, cl.config.HandshakesTimeout)
	defer cancel()
	dl, ok := ctx.Deadline()
	if !ok {
		panic(ctx)
	}
	err = nc.SetDeadline(dl)
	if err != nil {
		return
	}
	err = cl.initiateHandshakes(ctx, c, t)
	return
}

func doProtocolHandshakeOnDialResult(
	t *Torrent,
	obfuscatedHeader bool,
	addr PeerRemoteAddr,
	dr DialResult,
) (
	c *PeerConn, err error,
) {
	cl := t.cl
	nc := dr.Conn
	addrIpPort, _ := tryIpPortFromNetAddr(addr)

	c, err = cl.initiateProtocolHandshakes(
		context.Background(), nc, t, obfuscatedHeader,
		newConnectionOpts{
			outgoing:        true,
			remoteAddr:      addr,
			localPublicAddr: cl.publicAddr(addrIpPort.IP),
			network:         dr.Dialer.DialerNetwork(),
			connString:      regularNetConnPeerConnConnString(nc),
		})
	if err != nil {
		nc.Close()
	}
	return c, err
}

func (cl *Client) dialAndCompleteHandshake(opts outgoingConnOpts) (c *PeerConn, err error) {
	if !opts.receivedHolepunchConnect {
		dialReservation := cl.config.DialRateLimiter.Reserve()
		if !dialReservation.OK() {
			err = errors.New("can't make dial limit reservation")
			return
		}
		time.Sleep(dialReservation.Delay())
	}
	addr := opts.peerInfo.Addr
	dialPool := dialPool{
		resCh: make(chan DialResult),
		addr:  addr.String(),
	}
	defer dialPool.startDrainer()
	dialTimeout := opts.t.getDialTimeoutUnlocked()
	racing, stopRacing := context.WithTimeout(context.Background(), dialTimeout)
	defer stopRacing()
	for _, d := range cl.dialers {
		dialPool.add(racing, d)
	}
	holepunchAddr, holepunchAddrErr := addrPortFromPeerRemoteAddr(addr)
	headerObfuscationPolicy := opts.HeaderObfuscationPolicy
	obfuscatedHeaderFirst := headerObfuscationPolicy.Preferred
	firstDialResult := dialPool.getFirst()
	stopRacing()
	if firstDialResult.Conn == nil {
		if holepunchAddrErr == nil {
			cl.lock()
			if !opts.receivedHolepunchConnect {
				if len(cl.undialableWithoutHolepunch) >= maxUndialableRemembered {
					clear(cl.undialableWithoutHolepunch)
				}
				setAdd(&cl.undialableWithoutHolepunch, holepunchAddr)
			}
			if !opts.skipHolepunchRendezvous {
				opts.t.trySendHolepunchRendezvous(holepunchAddr)
			}
			cl.unlock()
		}
		err = fmt.Errorf("all initial dials failed")
		return
	}
	if opts.receivedHolepunchConnect && holepunchAddrErr == nil {
		cl.lock()
		if g.MapContains(cl.undialableWithoutHolepunch, holepunchAddr) {
			setAdd(&cl.dialableOnlyAfterHolepunch, holepunchAddr)
		}
		setAdd(&cl.dialedSuccessfullyAfterHolepunchConnect, holepunchAddr)
		cl.unlock()
	}
	c, err = doProtocolHandshakeOnDialResult(
		opts.t,
		obfuscatedHeaderFirst,
		addr,
		firstDialResult,
	)
	if err == nil {
		return
	}
	c.logger.Levelf(
		log.Debug,
		"error doing protocol handshake with header obfuscation %v",
		obfuscatedHeaderFirst,
	)
	firstDialResult.Conn.Close()
	if headerObfuscationPolicy.RequirePreferred {
		return
	}
	{
		ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
		defer cancel()
		dialPool.add(ctx, firstDialResult.Dialer)
	}
	secondDialResult := dialPool.getFirst()
	if secondDialResult.Conn == nil {
		return
	}
	c, err = doProtocolHandshakeOnDialResult(
		opts.t,
		!obfuscatedHeaderFirst,
		addr,
		secondDialResult,
	)
	if err == nil {
		return
	}
	c.logger.Levelf(
		log.Debug,
		"error doing protocol handshake with header obfuscation %v",
		!obfuscatedHeaderFirst,
	)
	secondDialResult.Conn.Close()
	return
}

type outgoingConnOpts struct {
	peerInfo                 PeerInfo
	t                        *Torrent
	requireRendezvous        bool
	skipHolepunchRendezvous  bool
	receivedHolepunchConnect bool
	HeaderObfuscationPolicy  HeaderObfuscationPolicy
}

func (cl *Client) outgoingConnection(
	opts outgoingConnOpts,
	attemptKey outgoingConnAttemptKey,
) {
	c, err := cl.dialAndCompleteHandshake(opts)
	if err == nil {
		c.conn.SetWriteDeadline(time.Time{})
	}
	cl.lock()
	defer cl.unlock()
	cl.noLongerHalfOpen(opts.t, opts.peerInfo.Addr.String(), attemptKey)
	if err != nil {
		if cl.config.Debug {
			cl.logger.Levelf(
				log.Debug,
				"error establishing outgoing connection to %v: %v",
				opts.peerInfo.Addr,
				err,
			)
		}
		return
	}
	defer c.close()
	c.Discovery = opts.peerInfo.Source
	c.trusted = opts.peerInfo.Trusted
	opts.t.runHandshookConnLoggingErr(c)
}

func (cl *Client) incomingPeerPort() int {
	return cl.LocalPort()
}

func (cl *Client) initiateHandshakes(ctx context.Context, c *PeerConn, t *Torrent) (err error) {
	if c.headerEncrypted {
		var rw io.ReadWriter
		rw, c.cryptoMethod, err = mse.InitiateHandshakeContext(
			ctx,
			struct {
				io.Reader
				io.Writer
			}{c.r, c.w},
			t.canonicalShortInfohash().Bytes(),
			nil,
			cl.config.CryptoProvides,
		)
		c.setRW(rw)
		if err != nil {
			return fmt.Errorf("header obfuscation handshake: %w", err)
		}
	}
	localReservedBits := cl.config.Extensions
	handshakeIh := *t.canonicalShortInfohash()
	localReservedBits.SetBit(pp.ExtensionBitV2Upgrade, g.Some(handshakeIh) == t.infoHash && t.infoHashV2.Ok)
	ih, err := cl.connBtHandshake(context.TODO(), c, &handshakeIh, localReservedBits)
	if err != nil {
		return fmt.Errorf("bittorrent protocol handshake: %w", err)
	}
	if g.Some(ih) == t.infoHash {
		return nil
	}
	if t.infoHashV2.Ok && *t.infoHashV2.Value.ToShort() == ih {
		c.v2 = true
		return nil
	}
	err = errors.New("bittorrent protocol handshake: peer infohash didn't match")
	return
}

func (cl *Client) forSkeys(f func([]byte) bool) {
	cl.rLock()
	defer cl.rUnlock()
	for ih := range cl.torrentsByShortHash {
		if !f(ih[:]) {
			break
		}
	}
}

func (cl *Client) handshakeReceiverSecretKeys() mse.SecretKeyIter {
	if ret := cl.config.Callbacks.ReceiveEncryptedHandshakeSkeys; ret != nil {
		return ret
	}
	return cl.forSkeys
}

func (cl *Client) receiveHandshakes(c *PeerConn) (t *Torrent, err error) {
	var rw io.ReadWriter
	rw, c.headerEncrypted, c.cryptoMethod, err = handleEncryption(
		c.rw(),
		cl.handshakeReceiverSecretKeys(),
		cl.config.HeaderObfuscationPolicy,
		cl.config.CryptoSelector,
	)
	c.setRW(rw)
	if err == nil || err == mse.ErrNoSecretKeyMatch {
		if c.headerEncrypted {
		} else {
		}
	} else {
	}
	if err != nil {
		if err == mse.ErrNoSecretKeyMatch {
			err = nil
		}
		return
	}
	if cl.config.HeaderObfuscationPolicy.RequirePreferred && c.headerEncrypted != cl.config.HeaderObfuscationPolicy.Preferred {
		err = errors.New("connection does not have required header obfuscation")
		return
	}
	ih, err := cl.connBtHandshake(context.TODO(), c, nil, cl.config.Extensions)
	if err != nil {
		return nil, fmt.Errorf("during bt handshake: %w", err)
	}

	cl.lock()
	t = cl.torrentsByShortHash[ih]
	if t != nil && t.infoHashV2.Ok && *t.infoHashV2.Value.ToShort() == ih {
		c.v2 = true
	}
	cl.unlock()

	return
}

func (cl *Client) connBtHandshake(ctx context.Context, c *PeerConn, ih *metainfo.Hash, reservedBits PeerExtensionBits) (ret metainfo.Hash, err error) {
	res, err := pp.Handshake(ctx, c.rw(), ih, cl.peerID, reservedBits)
	if err != nil {
		return
	}
	ret = res.Hash
	c.PeerExtensionBytes = res.PeerExtensionBits
	c.PeerID = res.PeerID
	c.completedHandshake = time.Now()
	if cb := cl.config.Callbacks.CompletedHandshake; cb != nil {
		cb(c, res.Hash)
	}
	return
}

func (cl *Client) runReceivedConn(c *PeerConn) {
	err := c.conn.SetDeadline(time.Now().Add(cl.config.HandshakesTimeout))
	if err != nil {
		return
	}
	t, err := cl.receiveHandshakes(c)
	if err != nil {
		cl.logger.LazyLog(log.Debug, func() log.Msg {
			return log.Fmsg(
				"error receiving handshakes on %v: %s", c, err,
			).Add(
				"network", c.Network,
			)
		})
		cl.lock()
		cl.onBadAccept(c.RemoteAddr)
		cl.unlock()
		return
	}
	if t == nil {
		cl.logger.LazyLog(log.Debug, func() log.Msg {
			return log.Fmsg("received handshake for unloaded torrent")
		})
		cl.lock()
		cl.onBadAccept(c.RemoteAddr)
		cl.unlock()
		return
	}
	c.conn.SetWriteDeadline(time.Time{})
	cl.lock()
	defer cl.unlock()
	t.runHandshookConnLoggingErr(c)
}

func (t *Torrent) runHandshookConn(pc *PeerConn) error {
	pc.setTorrent(t)
	cl := t.cl
	for i, b := range cl.config.MinPeerExtensions {
		if pc.PeerExtensionBytes[i]&b != b {
			return fmt.Errorf("peer did not meet minimum peer extensions: %x", pc.PeerExtensionBytes[:])
		}
	}
	if pc.PeerID == cl.peerID {
		if pc.outgoing {
			addr := pc.RemoteAddr.String()
			cl.dopplegangerAddrs[addr] = struct{}{}
		}
		t.logger.Levelf(log.Debug, "local and remote peer ids are the same")
		return nil
	}
	pc.r = deadlineReader{pc.conn, pc.r}
	if err := t.addPeerConn(pc); err != nil {
		return fmt.Errorf("adding connection: %w", err)
	}
	defer t.dropConnection(pc)
	pc.addBuiltinLtepProtocols()
	for _, cb := range pc.callbacks.PeerConnAdded {
		cb(pc)
	}
	pc.startMessageWriter()
	pc.sendInitialMessages()

	for _, cb := range pc.callbacks.StatusUpdated {
		cb(StatusUpdatedEvent{
			Event:  PeerConnected,
			PeerId: pc.PeerID,
		})
	}

	err := pc.mainReadLoop()
	if err != nil {
		for _, cb := range pc.callbacks.StatusUpdated {
			cb(StatusUpdatedEvent{
				Event:  PeerDisconnected,
				Error:  err,
				PeerId: pc.PeerID,
			})
		}
		return fmt.Errorf("main read loop: %w", err)
	}
	return nil
}

const localClientReqq = 1024

func (pc *PeerConn) sendInitialMessages() {
	t := pc.t
	cl := t.cl
	if pc.PeerExtensionBytes.SupportsExtended() && cl.config.Extensions.SupportsExtended() {
		pc.write(pp.Message{
			Type:       pp.Extended,
			ExtendedID: pp.HandshakeExtendedID,
			ExtendedPayload: func() []byte {
				msg := pp.ExtendedHandshakeMessage{
					V:            cl.config.ExtendedHandshakeClientVersion,
					Reqq:         localClientReqq,
					YourIp:       pp.CompactIp(pc.remoteIp()),
					Encryption:   cl.config.HeaderObfuscationPolicy.Preferred || !cl.config.HeaderObfuscationPolicy.RequirePreferred,
					Port:         cl.incomingPeerPort(),
					MetadataSize: t.metadataSize(),
				}
				msg.M = pc.LocalLtepProtocolMap.toSupportedExtensionDict()
				return bencode.MustMarshal(msg)
			}(),
		})
	}
	func() {
		if pc.fastEnabled() {
			if t.haveAllPieces() {
				pc.write(pp.Message{Type: pp.HaveAll})
				pc.sentHaves.AddRange(0, bitmap.BitRange(pc.t.NumPieces()))
				return
			} else if !t.haveAnyPieces() {
				pc.write(pp.Message{Type: pp.HaveNone})
				pc.sentHaves.Clear()
				return
			}
		}
		pc.postBitfield()
	}()
	if pc.PeerExtensionBytes.SupportsDHT() && cl.config.Extensions.SupportsDHT() && cl.haveDhtServer() {
		pc.write(pp.Message{
			Type: pp.Port,
			Port: cl.dhtPort(),
		})
	}
}

func (cl *Client) dhtPort() (ret uint16) {
	if len(cl.dhtServers) == 0 {
		return
	}
	return uint16(missinggo.AddrPort(cl.dhtServers[len(cl.dhtServers)-1].Addr()))
}

func (cl *Client) haveDhtServer() bool {
	return len(cl.dhtServers) > 0
}

func (cl *Client) gotMetadataExtensionMsg(payload []byte, t *Torrent, c *PeerConn) error {
	var d pp.ExtendedMetadataRequestMsg
	err := bencode.Unmarshal(payload, &d)
	if _, ok := err.(bencode.ErrUnusedTrailingBytes); ok {
	} else if err != nil {
		return fmt.Errorf("error unmarshalling bencode: %s", err)
	}
	piece := d.Piece
	switch d.Type {
	case pp.DataMetadataExtensionMsgType:
		if piece < 0 || piece >= t.metadataPieceCount() {
			return fmt.Errorf("metadata piece %d out of range", piece)
		}
		c.modifyRelevantConnStats(add(1, func(cs *ConnStats) *Count { return &cs.MetadataChunksRead }))
		if !c.requestedMetadataPiece(piece) {
			return fmt.Errorf("got unexpected piece %d", piece)
		}
		c.metadataRequests[piece] = false
		begin := len(payload) - t.metadataPieceSize(piece)
		if begin < 0 || begin >= len(payload) {
			return fmt.Errorf("data has bad offset in payload: %d", begin)
		}
		t.saveMetadataPiece(piece, payload[begin:])
		c.lastUsefulChunkReceived = time.Now()
		err = t.maybeCompleteMetadata()
		if err != nil {
			t.logger.WithDefaultLevel(log.Warning).Printf("error completing metadata: %v", err)
		}
		return err
	case pp.RequestMetadataExtensionMsgType:
		if c.messageWriter.backedUp() {
			return errors.New("metadata request while our writes to the peer are backed up")
		}
		if !t.haveMetadataPiece(piece) {
			c.protocolLogger.WithDefaultLevel(log.Debug).Printf("rejecting metadata piece %d", piece)
			c.write(t.newMetadataExtensionMessage(c, pp.RejectMetadataExtensionMsgType, d.Piece, nil))
			return nil
		}
		start := (1 << 14) * piece
		c.protocolLogger.WithDefaultLevel(log.Debug).Printf("sending metadata piece %d", piece)
		c.write(t.newMetadataExtensionMessage(c, pp.DataMetadataExtensionMsgType, piece, t.metadataBytes[start:start+t.metadataPieceSize(piece)]))
		return nil
	case pp.RejectMetadataExtensionMsgType:
		return nil
	default:
		return errors.New("unknown msg_type value")
	}
}

func (cl *Client) badPeerAddr(addr PeerRemoteAddr) bool {
	if ipa, ok := tryIpPortFromNetAddr(addr); ok {
		return cl.badPeerIPPort(ipa.IP, ipa.Port)
	}
	return false
}

func (cl *Client) badPeerIPPort(ip net.IP, port int) bool {
	if port == 0 || ip == nil {
		return true
	}
	if cl.dopplegangerAddr(net.JoinHostPort(ip.String(), strconv.FormatInt(int64(port), 10))) {
		return true
	}
	ipAddr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	if until, ok := cl.badPeerIPs[ipAddr.Unmap()]; ok && time.Now().Before(until) {
		return true
	}
	return false
}

const peerBanFor = 30 * time.Minute

func (cl *Client) newTorrent(ih metainfo.Hash, specStorage storage.ClientImpl) (t *Torrent) {
	return cl.newTorrentOpt(AddTorrentOpts{
		InfoHash: ih,
		Storage:  specStorage,
	})
}

func (cl *Client) newTorrentOpt(opts AddTorrentOpts) (t *Torrent) {
	var v1InfoHash g.Option[infohash.T]
	if !opts.InfoHash.IsZero() {
		v1InfoHash.Set(opts.InfoHash)
	}
	if !v1InfoHash.Ok && !opts.InfoHashV2.Ok {
		panic("v1 infohash must be nonzero or v2 infohash must be set")
	}
	storageClient := cl.defaultStorage
	if opts.Storage != nil {
		storageClient = storage.NewClient(opts.Storage)
	}

	t = &Torrent{
		cl:         cl,
		infoHash:   v1InfoHash,
		infoHashV2: opts.InfoHashV2,
		peers: prioritizedPeers{
			om: gbtree.New(32),
			getPrio: func(p PeerInfo) peerPriority {
				ipPort := p.addr()
				return bep40PriorityIgnoreError(cl.publicAddr(ipPort.IP), ipPort)
			},
		},
		conns: make(map[*PeerConn]struct{}, 2*cl.config.EstablishedConnsPerTorrent),

		storageOpener:       storageClient,
		maxEstablishedConns: cl.config.EstablishedConnsPerTorrent,

		metadataChanged: sync.Cond{
			L: cl.locker(),
		},
		gotMetainfoC: make(chan struct{}),

		ignoreUnverifiedPieceCompletion: opts.IgnoreUnverifiedPieceCompletion,
		initialPieceCheckDisabled:       opts.DisableInitialPieceCheck,
	}
	g.MakeMap(&t.webSeeds)
	t.closedCtx, t.closedCtxCancel = context.WithCancelCause(context.Background())
	t.getInfoCtx, t.getInfoCtxCancel = context.WithCancelCause(t.closedCtx)
	var salt [8]byte
	rand.Read(salt[:])
	t.smartBanCache.Hash = func(b []byte) uint64 {
		h := xxhash.New()
		h.Write(salt[:])
		h.Write(b)
		return h.Sum64()
	}
	t.smartBanCache.Init()
	t.networkingEnabled.Set()
	ihHex := t.InfoHash().HexString()
	t.logger = cl.logger.WithDefaultLevel(log.Debug).WithNames(ihHex).WithContextText(ihHex)
	t.name()
	t.baseSlogger = cl.slogger
	if opts.ChunkSize == 0 {
		opts.ChunkSize = defaultChunkSize
	}
	t.setChunkSize(opts.ChunkSize)
	cl.torrents[t] = struct{}{}
	return
}

type Handle interface {
	io.Reader
	io.Seeker
	io.Closer
	io.ReaderAt
}

func (cl *Client) AddTorrentInfoHash(infoHash metainfo.Hash) (t *Torrent, new bool) {
	return cl.AddTorrentInfoHashWithStorage(infoHash, nil)
}

func (cl *Client) AddTorrentInfoHashWithStorage(
	infoHash metainfo.Hash,
	specStorage storage.ClientImpl,
) (t *Torrent, new bool) {
	cl.lock()
	defer cl.unlock()
	t, ok := cl.torrentsByShortHash[infoHash]
	if ok {
		return
	}
	new = true

	t = cl.newTorrent(infoHash, specStorage)
	cl.eachDhtServer(func(s DhtServer) {
		if cl.config.PeriodicallyAnnounceTorrentsToDht {
			go t.dhtAnnouncer(s)
		}
	})
	cl.torrentsByShortHash[infoHash] = t
	cl.clearAcceptLimits()
	t.updateWantPeersEvent()
	cl.event.Broadcast()
	return
}

func (cl *Client) AddTorrentOpt(opts AddTorrentOpts) (t *Torrent, new bool) {
	infoHash := opts.InfoHash
	panicif.Zero(infoHash)
	cl.lock()
	defer cl.unlock()
	t, ok := cl.torrentsByShortHash[infoHash]
	if ok {
		return
	}
	if opts.InfoHashV2.Ok {
		t, ok = cl.torrentsByShortHash[*opts.InfoHashV2.Value.ToShort()]
		if ok {
			return
		}
	}
	new = true

	t = cl.newTorrentOpt(opts)
	cl.eachDhtServer(func(s DhtServer) {
		if cl.config.PeriodicallyAnnounceTorrentsToDht {
			go t.dhtAnnouncer(s)
		}
	})
	cl.torrentsByShortHash[infoHash] = t
	t.setInfoBytesLocked(opts.InfoBytes)
	cl.clearAcceptLimits()
	t.updateWantPeersEvent()
	cl.event.Broadcast()
	return
}

type AddTorrentOpts struct {
	InfoHash                        infohash.T
	InfoHashV2                      g.Option[infohash_v2.T]
	Storage                         storage.ClientImpl
	ChunkSize                       pp.Integer
	InfoBytes                       []byte
	DisableInitialPieceCheck        bool
	IgnoreUnverifiedPieceCompletion bool
	DisallowDataUpload              bool
	DisallowDataDownload            bool
}

func (cl *Client) AddTorrentSpec(spec *TorrentSpec) (t *Torrent, new bool, err error) {
	t, new = cl.AddTorrentOpt(spec.AddTorrentOpts)
	modSpec := *spec
	modSpec.ChunkSize = 0
	err = t.MergeSpec(&modSpec)
	if err != nil && new {
		t.Drop()
	}
	return
}

func (t *Torrent) MergeSpec(spec *TorrentSpec) error {
	if spec.DisplayName != "" {
		t.SetDisplayName(spec.DisplayName)
	}
	if spec.InfoBytes != nil {
		err := t.SetInfoBytes(spec.InfoBytes)
		if err != nil {
			return err
		}
	}
	cl := t.cl
	cl.AddDhtNodes(spec.DhtNodes)
	t.AddSources(spec.Sources)
	cl.lock()
	defer cl.unlock()
	for _, url := range spec.Webseeds {
		t.addWebSeed(url)
	}
	t.addPeersIter(func(yield func(PeerInfo) bool) {
		for _, peerAddr := range spec.PeerAddrs {
			if !yield(PeerInfo{
				Addr:    StringAddr(peerAddr),
				Source:  PeerSourceDirect,
				Trusted: true,
			}) {
				return
			}
		}
	})
	if spec.ChunkSize != 0 {
		panic("chunk size cannot be changed for existing Torrent")
	}
	t.addTrackers(spec.Trackers)
	t.maybeNewConns()
	return errors.Join(t.addPieceLayersLocked(spec.PieceLayers)...)
}

func (cl *Client) dropTorrent(t *Torrent, wg *sync.WaitGroup) {
	t.close(wg)
}

func (cl *Client) allTorrentsCompleted() bool {
	for t := range cl.torrents {
		if !t.haveInfo() {
			return false
		}
		if !t.haveAllPieces() {
			return false
		}
	}
	return true
}

func (cl *Client) WaitAll() bool {
	cl.lock()
	defer cl.unlock()
	for !cl.allTorrentsCompleted() {
		if cl.closed.IsSet() {
			return false
		}
		cl.event.Wait()
	}
	return true
}

func (cl *Client) Torrents() []*Torrent {
	cl.rLock()
	defer cl.rUnlock()
	return cl.torrentsAsSlice()
}

func (cl *Client) torrentsAsSlice() (ret []*Torrent) {
	ret = make([]*Torrent, 0, len(cl.torrents))
	for t := range cl.torrents {
		ret = append(ret, t)
	}
	return
}

func (cl *Client) AddMagnet(uri string) (T *Torrent, err error) {
	spec, err := TorrentSpecFromMagnetUri(uri)
	if err != nil {
		return
	}
	T, _, err = cl.AddTorrentSpec(spec)
	return
}

func (cl *Client) AddTorrent(mi *metainfo.MetaInfo) (T *Torrent, err error) {
	ts, err := TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		return
	}
	T, _, err = cl.AddTorrentSpec(ts)
	return
}

func (cl *Client) AddTorrentFromFile(filename string) (T *Torrent, err error) {
	mi, err := metainfo.LoadFromFile(filename)
	if err != nil {
		return
	}
	return cl.AddTorrent(mi)
}

func (cl *Client) DhtServers() []DhtServer {
	return cl.dhtServers
}

func (cl *Client) AddDhtNodes(nodes []string) {
	for _, n := range nodes {
		hmp := missinggo.SplitHostMaybePort(n)
		ip := net.ParseIP(hmp.Host)
		if ip == nil {
			cl.logger.Printf("won't add DHT node with bad IP: %q", hmp.Host)
			continue
		}
		ni := krpc.NodeInfo{
			Addr: krpc.NodeAddr{
				IP:   ip,
				Port: hmp.Port,
			},
		}
		cl.eachDhtServer(func(s DhtServer) {
			s.AddNode(ni)
		})
	}
}

func (cl *Client) banPeerIP(ip net.IP) {
	ipAddr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return
	}
	g.MakeMapIfNil(&cl.badPeerIPs)
	now := time.Now()
	for addr, until := range cl.badPeerIPs {
		if !now.Before(until) {
			delete(cl.badPeerIPs, addr)
		}
	}
	cl.badPeerIPs[ipAddr.Unmap()] = now.Add(peerBanFor)
	for t := range cl.torrents {
		t.iterPeers(func(p *Peer) {
			if p.remoteIp().Equal(ip) {
				t.slogger().Debug("dropping peer with banned ip", "peer", p, "ip", ip)
				p.drop()
			}
		})
	}
}

type newConnectionOpts struct {
	outgoing        bool
	remoteAddr      PeerRemoteAddr
	localPublicAddr peerLocalPublicAddr
	network         string
	connString      string
}

func (cl *Client) newConnection(nc net.Conn, opts newConnectionOpts) (c *PeerConn) {
	if opts.network == "" {
		panic(opts.remoteAddr)
	}
	c = &PeerConn{
		Peer: Peer{
			cl:          cl,
			outgoing:    opts.outgoing,
			choking:     true,
			peerChoking: true,

			RemoteAddr:      opts.remoteAddr,
			localPublicAddr: opts.localPublicAddr,
			Network:         opts.network,
			callbacks:       &cl.config.Callbacks,
		},
		PeerMaxRequests:   250,
		connString:        opts.connString,
		conn:              nc,
		holepunchConnects: rate.NewLimiter(rate.Every(10*time.Second), 3),
	}
	c.initRequestState()
	if opts.remoteAddr != nil {
		netipAddrPort, err := netip.ParseAddrPort(opts.remoteAddr.String())
		if err == nil {
			c.bannableAddr = Some(netipAddrPort.Addr())
		}
	}
	c.legacyPeerImpl = c
	c.peerImpl = c
	c.setPeerLoggers(cl.logger, cl.slogger)
	c.setRW(connStatsReadWriter{nc, c})
	c.r = cl.newDownloadRateLimitedReader(c.r)
	c.logger.Levelf(
		log.Debug,
		"inited with remoteAddr %v network %v outgoing %t",
		opts.remoteAddr, opts.network, opts.outgoing,
	)
	for _, f := range cl.config.Callbacks.NewPeer {
		f(&c.Peer)
	}
	return
}

func (cl *Client) newDownloadRateLimitedReader(r io.Reader) io.Reader {
	return newRateLimitedReader(r, cl.config.DownloadRateLimiter)
}

const maxUndialableRemembered = 4096

func (cl *Client) onDHTAnnouncePeer(ih metainfo.Hash, ip net.IP, port int, portOk bool) {
	if !portOk {
		return
	}
	cl.lock()
	defer cl.unlock()
	t := cl.torrentsByShortHash[ih]
	if t == nil {
		return
	}
	t.addPeers([]PeerInfo{{
		Addr:   ipPortAddr{ip, port},
		Source: PeerSourceDhtAnnouncePeer,
	}})
}

func (cl *Client) eachListener(f func(Listener) bool) {
	for _, s := range cl.listeners {
		if !f(s) {
			break
		}
	}
}

func (cl *Client) findListener(f func(Listener) bool) (ret Listener) {
	for i := 0; i < len(cl.listeners); i += 1 {
		if ret = cl.listeners[i]; f(ret) {
			return
		}
	}
	return nil
}

func (cl *Client) publicIp(peer net.IP) net.IP {
	if peer.To4() != nil {
		return cl.findListenerIp(func(ip net.IP) bool { return ip.To4() != nil })
	}

	return cl.findListenerIp(func(ip net.IP) bool { return ip.To4() == nil })
}

func (cl *Client) findListenerIp(f func(net.IP) bool) net.IP {
	l := cl.findListener(
		func(l Listener) bool {
			return f(addrIpOrNil(l.Addr()))
		},
	)
	if l == nil {
		return nil
	}
	return addrIpOrNil(l.Addr())
}

func (cl *Client) publicAddr(peer net.IP) IpPort {
	return IpPort{IP: cl.publicIp(peer), Port: uint16(cl.incomingPeerPort())}
}

func (cl *Client) ListenAddrs() (ret []net.Addr) {
	cl.lock()
	ret = make([]net.Addr, len(cl.listeners))
	for i := 0; i < len(cl.listeners); i += 1 {
		ret[i] = cl.listeners[i].Addr()
	}
	cl.unlock()
	return
}

func (cl *Client) onBadAccept(addr PeerRemoteAddr) {
	ipa, ok := tryIpPortFromNetAddr(addr)
	if !ok {
		return
	}
	ip := maskIpForAcceptLimiting(ipa.IP)
	if cl.acceptLimiter == nil {
		cl.acceptLimiter = make(map[ipStr]int)
	}
	cl.acceptLimiter[ipStr(ip.String())]++
}

func maskIpForAcceptLimiting(ip net.IP) net.IP {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.Mask(net.CIDRMask(24, 32))
	}
	return ip
}

func (cl *Client) clearAcceptLimits() {
	cl.acceptLimiter = nil
}

func (cl *Client) acceptLimitClearer() {
	for {
		select {
		case <-cl.closed.Done():
			return
		case <-time.After(15 * time.Minute):
			cl.lock()
			cl.clearAcceptLimits()
			cl.unlock()
		}
	}
}

func (cl *Client) rateLimitAccept(ip net.IP) bool {
	if cl.config.DisableAcceptRateLimiting {
		return false
	}
	return cl.acceptLimiter[ipStr(maskIpForAcceptLimiting(ip).String())] > 0
}

func (cl *Client) rLock() {
	cl._mu.RLock()
}

func (cl *Client) rUnlock() {
	cl._mu.RUnlock()
}

func (cl *Client) lock() {
	cl._mu.Lock()
}

func (cl *Client) unlock() {
	cl._mu.Unlock()
}

func (cl *Client) locker() *lockWithDeferreds {
	return &cl._mu
}

func (cl *Client) String() string {
	return fmt.Sprintf("<%[1]T %[1]p>", cl)
}

func (cl *Client) ConnStats() ConnStats {
	return cl.connStats.ConnStats.Copy()
}

func (cl *Client) Stats() ClientStats {
	cl.rLock()
	defer cl.rUnlock()
	return cl.statsLocked()
}

func (cl *Client) underWebSeedHttpRequestLimit(key webseedHostKeyHandle) bool {
	panicif.Zero(key)
	return cl.numWebSeedRequests[key] < webseedHostRequestConcurrency
}

func (cl *Client) checkConfig() error {
	if EffectiveDownloadRateLimit(cl.config.DownloadRateLimiter) == 0 {
		if len(cl.dialers) != 0 {
			return errors.New("download rate limit is zero, but dialers are set")
		}
		if len(cl.listeners) != 0 && cl.config.AcceptPeerConnections {
			return errors.New("download rate limit is zero, but listening for peer connections")
		}
	}
	return nil
}

var maxActivePieceHashers = initIntFromEnv("TORRENT_MAX_ACTIVE_PIECE_HASHERS", runtime.NumCPU(), 0)

func (cl *Client) maxActivePieceHashers() int {
	return maxActivePieceHashers
}

func (cl *Client) belowMaxActivePieceHashers() bool {
	return cl.activePieceHashers < cl.maxActivePieceHashers()
}

func (cl *Client) canStartPieceHashers() bool {
	return cl.belowMaxActivePieceHashers()
}

func (cl *Client) startPieceHashers() {
	if !cl.canStartPieceHashers() {
		return
	}
	ts := make([]*Torrent, 0, len(cl.torrents))
	for t := range cl.torrents {
		if !t.considerStartingHashers() {
			continue
		}
		ts = append(ts, t)
	}
	slices.SortFunc(ts, func(a, b *Torrent) int {
		return -cmp.Compare(a.length(), b.length())
	})
	for _, t := range ts {
		t.startPieceHashers()
		if !cl.canStartPieceHashers() {
			break
		}
	}
}
