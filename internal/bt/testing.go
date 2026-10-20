package bt

import (
	"time"

	"github.com/anacrolix/log"

	pp "yoru/internal/bt/peer_protocol"
)

// TestingConfig is a loopback client for a test; t is its *testing.T, by method so this file stays out of the
// testing package (tests in other packages use it, so it can't be a _test.go file).
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
	// Just over the tests' chunk size, so the limit is reached (the greeting test needs 2, others 5).
	cfg.MaxAllocPeerRequestDataPerConn = 5
	return cfg
}
