//go:build !windows

package engine

import "net"

func adapterDNS(string) []net.IP { return nil }
