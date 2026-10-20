package bt

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2"
	"yoru/internal/bt/dht"
	"yoru/internal/bt/dht/krpc"

	"golang.org/x/time/rate"

	"yoru/internal/bt/iplist"
	"yoru/internal/bt/metainfo"

	"yoru/internal/bt/mse"
	"yoru/internal/bt/storage"
	"yoru/internal/bt/version"
)

// Contains config elements that are exclusive to tracker handling. There may be other fields in
// ClientConfig that are also relevant.
type ClientTrackerConfig struct {
	// Don't announce to trackers. This only leaves DHT to discover peers.
	DisableTrackers bool `long:"disable-trackers"`
	// Defines DialContext func to use for HTTP tracker announcements
	TrackerDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// Defines ListenPacket func to use for UDP tracker announcements
	TrackerListenPacket func(network, addr string) (net.PacketConn, error)
	// Looks UDP trackers' names up; nil is the system resolver. HTTP trackers resolve in TrackerDialContext.
	TrackerResolver *net.Resolver
}

type ClientDhtConfig struct {
	// Don't create a DHT.
	NoDHT            bool `long:"disable-dht"`
	DhtStartingNodes func(network string) dht.StartingNodesGetter
	// Called for each DHT Server created for the Client.
	ConfigureDhtServer                func(*dht.ServerConfig)
	PeriodicallyAnnounceTorrentsToDht bool
	// OnQuery hook func
	DHTOnQuery func(query *krpc.Msg, source net.Addr) (propagate bool)
}

// Probably not safe to modify this after it's given to a Client, or to pass it to multiple Clients.
type ClientConfig struct {
	ClientTrackerConfig
	ClientDhtConfig
	MetainfoSourcesConfig

	// Store torrent file data in this directory unless DefaultStorage is specified.
	DataDir string `long:"data-dir" description:"directory to store downloaded torrent data"`
	// The address to listen for new uTP and TCP BitTorrent protocol connections. DHT shares a UDP
	// socket with uTP unless configured otherwise.
	ListenHost func(network string) string
	ListenPort int
	// cfg.NoDefaultPortForwarding aka cfg.DisableUpnp
	NoDefaultPortForwarding bool
	UpnpID                  string
	// UpnpDiscovered hears how many routers answered; UpnpMapped each mapping's port, or why it was refused.
	UpnpDiscovered func(routers int)
	UpnpMapped     func(proto string, internal, external int, err error)
	DisablePEX     bool `long:"disable-pex"`

	// Never send chunks to peers.
	NoUpload bool `long:"no-upload"`
	// Disable uploading even when it isn't fair.
	DisableAggressiveUpload bool `long:"disable-aggressive-upload"`
	// Upload even after there's nothing in it for us. By default uploading is
	// not altruistic, we'll only upload to encourage the peer to reciprocate.
	Seed bool `long:"seed"`
	// StopSeedingWhenComplete limits Seed to while a torrent still wants data: done, it uploads nothing.
	StopSeedingWhenComplete bool
	// Bytes of chunks uploaded (other messages pass, to stay responsive); the burst must fit a chunk
	// (16 KiB usually), chosen for you when 0 under a finite limit.
	UploadRateLimiter *rate.Limiter
	// Bytes read from peers, nil for none; the burst must top the largest Read less one (a chunk,
	// ~16 KiB), chosen for you when 0 under a finite limit.
	DownloadRateLimiter *rate.Limiter
	// Maximum unverified bytes across all torrents. Not used if zero.
	MaxUnverifiedBytes int64

	// User-provided Client peer ID. If not present, one is generated automatically.
	PeerID string
	// For the bittorrent protocol.
	DisableUTP bool
	// For the bittorrent protocol.
	DisableTCP bool `long:"disable-tcp"`
	// Storage for each added torrent (backends in package storage); unset, "file" storage, Closed with
	// the Client.
	DefaultStorage storage.ClientImpl

	HeaderObfuscationPolicy HeaderObfuscationPolicy
	// The crypto methods to offer when initiating connections with header obfuscation.
	CryptoProvides mse.CryptoMethod
	// Chooses the crypto method to use when receiving connections with header obfuscation.
	CryptoSelector mse.CryptoSelector

	IPBlocklist      iplist.Ranger
	DisableIPv6      bool `long:"disable-ipv6"`
	DisableIPv4      bool
	DisableIPv4Peers bool
	// Perform logging and any other behaviour that will help debug.
	Debug bool `help:"enable debugging"`
	// If Slogger is set, will be forwarded automatically to Slogger. This is recommended.
	Logger log.Logger
	// If unset, falls back to deferring to the old analog Logger.
	Slogger *slog.Logger

	// Used for torrent sources and webseeding if set.
	WebTransport http.RoundTripper
	// Defines proxy for HTTP requests, such as for trackers. It's commonly set from the result of
	// "net/http".ProxyURL(HTTPProxy).
	HTTPProxy func(*http.Request) (*url.URL, error)
	// Defines DialContext func to use for HTTP requests, such as for fetching metainfo and webseeds
	HTTPDialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	// HTTPUserAgent changes default UserAgent for HTTP requests
	HTTPUserAgent string
	// HttpRequestDirector modifies the request before it's sent.
	// Useful for adding authentication headers, for example
	HttpRequestDirector func(*http.Request) error
	// Changed with client behaviour, in case others rely on it; see also Bep20.
	ExtendedHandshakeClientVersion string
	// Peer ID prefix, changed with client behaviour others may rely on; see ExtendedHandshakeClientVersion.
	Bep20 string

	// Peer dial timeout to use when there are limited peers.
	NominalDialTimeout time.Duration
	// Minimum peer dial timeout to use (even if we have lots of peers).
	MinDialTimeout             time.Duration
	EstablishedConnsPerTorrent int
	HalfOpenConnsPerTorrent    int
	TotalHalfOpenConns         int
	// Maximum number of peer addresses in reserve.
	TorrentPeersHighWater int
	// Minumum number of peers before effort is made to obtain more peers.
	TorrentPeersLowWater int

	// Caps handshakes so bad peers don't linger: 4s loses 1% of those 60s would finish.
	HandshakesTimeout time.Duration
	// How long between writes before sending a keep alive message on a peer connection that we want
	// to maintain.
	KeepAliveTimeout time.Duration
	// Maximum bytes to buffer per peer connection for peer request data before it is sent. This
	// must be >= the request chunk size from peers.
	MaxAllocPeerRequestDataPerConn int

	// The IP addresses as our peers should see them. May differ from the
	// local interfaces due to NAT or other network configurations.
	PublicIp4 net.IP
	PublicIp6 net.IP

	// Accept rate limiting affects excessive connection attempts from IPs that fail during
	// handshakes or request torrents that we don't have.
	DisableAcceptRateLimiting bool
	// Don't add connections that have the same peer ID as an existing one for a Torrent.
	DropDuplicatePeerIds bool
	// Drop complete peers once we're complete too (other useless peers, like uninterested ones, stay).
	DropMutuallyCompletePeers bool
	// Use dialers to obtain connections to regular peers.
	DialForPeerConns bool
	// Whether to accept peer connections at all.
	AcceptPeerConnections bool
	// Whether a Client should want conns without delegating to any attached Torrents. This is
	// useful when torrents might be added dynamically in callbacks for example.
	AlwaysWantConns bool

	Extensions PeerExtensionBits
	// Bits that peers must have set to proceed past handshakes.
	MinPeerExtensions PeerExtensionBits

	DisableWebseeds bool

	Callbacks Callbacks

	DialRateLimiter *rate.Limiter

	PieceHashersPerTorrent int // default: 2
}

func (cfg *ClientConfig) SetListenAddr(addr string) *ClientConfig {
	host, port, err := missinggo.ParseHostPort(addr)
	if err != nil {
		panic(err)
	}
	cfg.ListenHost = func(string) string { return host }
	cfg.ListenPort = port
	return cfg
}

func NewDefaultClientConfig() *ClientConfig {
	cc := &ClientConfig{
		HTTPUserAgent:                  version.DefaultHttpUserAgent,
		ExtendedHandshakeClientVersion: version.DefaultExtendedHandshakeClientVersion,
		Bep20:                          version.DefaultBep20Prefix,
		UpnpID:                         version.DefaultUpnpId,
		NominalDialTimeout:             20 * time.Second,
		MinDialTimeout:                 3 * time.Second,
		EstablishedConnsPerTorrent:     50,
		HalfOpenConnsPerTorrent:        25,
		TotalHalfOpenConns:             100,
		TorrentPeersHighWater:          500,
		TorrentPeersLowWater:           50,
		HandshakesTimeout:              4 * time.Second,
		KeepAliveTimeout:               time.Minute,
		MaxAllocPeerRequestDataPerConn: 1 << 20,
		ListenHost:                     func(string) string { return "" },
		UploadRateLimiter:              unlimited,
		DisableAcceptRateLimiting:      true,
		DropMutuallyCompletePeers:      true,
		HeaderObfuscationPolicy: HeaderObfuscationPolicy{
			Preferred:        true,
			RequirePreferred: false,
		},
		CryptoSelector:         mse.DefaultCryptoSelector,
		CryptoProvides:         mse.AllSupportedCrypto,
		ListenPort:             42069,
		Extensions:             defaultPeerExtensionBytes(),
		DialForPeerConns:       true,
		AcceptPeerConnections:  true,
		MaxUnverifiedBytes:     64 << 20,
		DialRateLimiter:        rate.NewLimiter(10, 10),
		PieceHashersPerTorrent: 2,
	}
	cc.DhtStartingNodes = func(network string) dht.StartingNodesGetter {
		return func() ([]dht.Addr, error) { return dht.GlobalBootstrapAddrs(network) }
	}
	cc.PeriodicallyAnnounceTorrentsToDht = true
	cc.MetainfoSourcesMerger = func(t *Torrent, info *metainfo.MetaInfo) error {
		return t.MergeSpec(TorrentSpecFromMetaInfo(info))
	}
	return cc
}

type HeaderObfuscationPolicy struct {
	RequirePreferred bool // Whether the value of Preferred is a strict requirement.
	Preferred        bool // Whether header obfuscation is preferred.
}

func (cfg *ClientConfig) setRateLimiterBursts() {
	// What about chunk size?
	if cfg.UploadRateLimiter.Burst() == 0 {
		cfg.UploadRateLimiter.SetBurst(cfg.MaxAllocPeerRequestDataPerConn)
	}
	setDefaultDownloadRateLimiterBurstIfZero(cfg.DownloadRateLimiter)
}

// Returns the download rate.Limit handling the special nil case.
func EffectiveDownloadRateLimit(l *rate.Limiter) rate.Limit {
	if l == nil {
		return rate.Inf
	}
	return l.Limit()
}

type MetainfoSourcesConfig struct {
	// For metainfo sources only; else the http.Client made around WebTransport.
	MetainfoSourcesClient *http.Client
	// Applies metainfo a source fetched for t (the fetcher's own, so no race); an error kills that source.
	MetainfoSourcesMerger func(t *Torrent, info *metainfo.MetaInfo) error
}
