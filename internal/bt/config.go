package bt

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2"
	"yoru/internal/bt/dht"

	"golang.org/x/time/rate"

	"yoru/internal/bt/metainfo"

	"yoru/internal/bt/mse"
	"yoru/internal/bt/storage"
	"yoru/internal/bt/version"
)

type ClientTrackerConfig struct {
	DisableTrackers     bool `long:"disable-trackers"`
	TrackerDialContext  func(ctx context.Context, network, addr string) (net.Conn, error)
	TrackerListenPacket func(network, addr string) (net.PacketConn, error)
	TrackerResolver     *net.Resolver
}

type ClientDhtConfig struct {
	NoDHT                             bool `long:"disable-dht"`
	DhtStartingNodes                  func(network string) dht.StartingNodesGetter
	DhtNodesFile                      string
	PeriodicallyAnnounceTorrentsToDht bool
}

type ClientConfig struct {
	ClientTrackerConfig
	ClientDhtConfig
	MetainfoSourcesConfig

	DataDir                 string `long:"data-dir" description:"directory to store downloaded torrent data"`
	ListenHost              func(network string) string
	ListenPort              int
	NoDefaultPortForwarding bool
	UpnpID                  string
	UpnpDiscovered          func(routers int)
	UpnpMapped              func(proto string, internal, external int, err error)
	DisablePEX              bool `long:"disable-pex"`

	NoUpload                bool `long:"no-upload"`
	DisableAggressiveUpload bool `long:"disable-aggressive-upload"`
	Seed                    bool `long:"seed"`
	StopSeedingWhenComplete bool
	UploadRateLimiter       *rate.Limiter
	DownloadRateLimiter     *rate.Limiter
	MaxUnverifiedBytes      int64

	PeerID         string
	DisableUTP     bool
	DisableTCP     bool `long:"disable-tcp"`
	DefaultStorage storage.ClientImpl

	HeaderObfuscationPolicy HeaderObfuscationPolicy
	CryptoProvides          mse.CryptoMethod
	CryptoSelector          mse.CryptoSelector

	DisableIPv6 bool `long:"disable-ipv6"`
	DisableIPv4 bool
	Debug       bool `help:"enable debugging"`
	Logger      log.Logger
	Slogger     *slog.Logger

	HTTPDialContext                func(ctx context.Context, network, addr string) (net.Conn, error)
	HTTPUserAgent                  string
	ExtendedHandshakeClientVersion string
	Bep20                          string

	NominalDialTimeout         time.Duration
	MinDialTimeout             time.Duration
	EstablishedConnsPerTorrent int
	HalfOpenConnsPerTorrent    int
	TotalHalfOpenConns         int
	TorrentPeersHighWater      int
	TorrentPeersLowWater       int

	HandshakesTimeout              time.Duration
	KeepAliveTimeout               time.Duration
	MaxAllocPeerRequestDataPerConn int

	DisableAcceptRateLimiting bool
	DropDuplicatePeerIds      bool
	DropMutuallyCompletePeers bool
	DialForPeerConns          bool
	AcceptPeerConnections     bool
	AlwaysWantConns           bool

	Extensions        PeerExtensionBits
	MinPeerExtensions PeerExtensionBits

	DisableWebseeds bool

	Callbacks Callbacks

	DialRateLimiter *rate.Limiter

	PieceHashersPerTorrent int
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
		spec, err := TorrentSpecFromMetaInfoErr(info)
		if err != nil {
			return err
		}
		return t.MergeSpec(spec)
	}
	return cc
}

type HeaderObfuscationPolicy struct {
	RequirePreferred bool
	Preferred        bool
}

func (cfg *ClientConfig) setRateLimiterBursts() {
	if cfg.UploadRateLimiter != unlimited && cfg.UploadRateLimiter.Burst() == 0 {
		cfg.UploadRateLimiter.SetBurst(cfg.MaxAllocPeerRequestDataPerConn)
	}
	if cfg.DownloadRateLimiter != unlimited {
		setDefaultDownloadRateLimiterBurstIfZero(cfg.DownloadRateLimiter)
	}
}

func EffectiveDownloadRateLimit(l *rate.Limiter) rate.Limit {
	if l == nil {
		return rate.Inf
	}
	return l.Limit()
}

type MetainfoSourcesConfig struct {
	MetainfoSourcesClient *http.Client
	MetainfoSourcesMerger func(t *Torrent, info *metainfo.MetaInfo) error
}
