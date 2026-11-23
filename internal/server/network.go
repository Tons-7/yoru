package server

import (
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"yoru/internal/torrent"
)

type netAdapter struct {
	Name      string   `json:"name"`
	Addresses []string `json:"addresses"`
	VPN       bool     `json:"vpn"`
}

var vpnName = regexp.MustCompile(`(?i)vpn|wireguard|\bwg\d|\btun|\btap|nordlynx|proton|mullvad|windscribe|surfshark|cyberghost|ipvanish|expressvpn`)

func adapters() []netAdapter {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []netAdapter
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		a := netAdapter{Name: iface.Name, VPN: vpnName.MatchString(iface.Name)}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.IsGlobalUnicast() {
				a.Addresses = append(a.Addresses, ipnet.IP.String())
			}
		}
		if len(a.Addresses) > 0 {
			out = append(out, a)
		}
	}
	slices.SortStableFunc(out, func(x, y netAdapter) int {
		if x.VPN != y.VPN {
			if x.VPN {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
	})
	return out
}

func (s *Server) network(w http.ResponseWriter, r *http.Request) {
	var status torrent.Network
	if s.engine != nil {
		status = s.engine.Network()
	}
	send(w, http.StatusOK, map[string]any{"adapters": adapters(), "network": status})
}
