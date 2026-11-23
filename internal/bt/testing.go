package bt

import (
	"time"

	"github.com/anacrolix/log"

	pp "yoru/internal/bt/peer_protocol"
)

func TestingConfig(t interface {
	TempDir() string
	Name() string
}) *ClientConfig {
	cfg := NewDefaultClientConfig()
	cfg.ListenHost = LoopbackListenHost
	cfg.NoDHT = true
	cfg.DataDir = t.TempDir()
	cfg.DisableTrackers = true
	cfg.NoDefaultPortForwarding = true
	cfg.DisableAcceptRateLimiting = true
	cfg.ListenPort = 0
	cfg.KeepAliveTimeout = time.Millisecond
	cfg.MinPeerExtensions.SetBit(pp.ExtensionBitFast, true)
	cfg.Logger = log.Default.WithNames(t.Name())
	cfg.MaxAllocPeerRequestDataPerConn = 5
	return cfg
}
