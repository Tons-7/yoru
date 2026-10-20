package server

import (
	"slices"
	"testing"
)

// The default route's address leads the pairing list, even on a virtual switch, but never a VPN's.
func TestOrderAddrs(t *testing.T) {
	for _, c := range []struct {
		name          string
		real, virtual []string
		vpn           map[string]bool
		main          string
		want          []string
	}{
		{"real first", []string{"10.0.0.5", "192.168.1.10"}, nil, nil, "192.168.1.10", []string{"192.168.1.10", "10.0.0.5"}},
		{"LAN on a Hyper-V external switch", nil, []string{"172.20.0.1", "192.168.1.10"}, nil, "192.168.1.10",
			[]string{"192.168.1.10", "172.20.0.1"}},
		{"full-tunnel VPN owns the route", []string{"192.168.1.10"}, []string{"10.8.0.2"}, map[string]bool{"10.8.0.2": true}, "10.8.0.2",
			[]string{"192.168.1.10", "10.8.0.2"}},
		{"route unknown", []string{"192.168.1.10"}, []string{"172.20.0.1"}, nil, "", []string{"192.168.1.10", "172.20.0.1"}},
	} {
		if got := orderAddrs(c.real, c.virtual, c.vpn, c.main); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
