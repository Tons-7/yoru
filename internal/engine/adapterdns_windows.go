//go:build windows

package engine

import (
	"errors"
	"net"
	"unsafe"

	"golang.org/x/sys/windows"
)

// adapterDNS is the named adapter's DNS servers, a VPN's own; Windows' fec0:: placeholders are skipped.
func adapterDNS(name string) []net.IP {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	const flags = windows.GAA_FLAG_SKIP_UNICAST | windows.GAA_FLAG_SKIP_ANYCAST | windows.GAA_FLAG_SKIP_MULTICAST
	size := uint32(16 << 10)
	for range 3 {
		buf := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, flags, 0, first, &size)
		if errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			continue
		}
		if err != nil {
			return nil
		}
		for aa := first; aa != nil; aa = aa.Next {
			if int(aa.IfIndex) != iface.Index && int(aa.Ipv6IfIndex) != iface.Index {
				continue
			}
			var out []net.IP
			for d := aa.FirstDnsServerAddress; d != nil; d = d.Next {
				ip := d.Address.IP()
				if ip == nil || ip.To4() == nil && ip[0] == 0xfe && ip[1]&0xc0 == 0xc0 {
					continue
				}
				out = append(out, ip)
			}
			return out
		}
		return nil
	}
	return nil
}
