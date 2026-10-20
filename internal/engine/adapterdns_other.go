//go:build !windows

package engine

import "net"

// adapterDNS: VPNs on macOS and Linux set the system's DNS while they're up, so lookups stay with the system.
func adapterDNS(string) []net.IP { return nil }
