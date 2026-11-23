package firewall

import (
	"fmt"
	"strconv"
	"strings"
)

type Network struct {
	Name      string `json:"name"`
	Interface string `json:"interface"`
	Category  string `json:"category"`
}

type Rule struct {
	Name      string `json:"name"`
	Action    string `json:"action"`
	Profiles  string `json:"profiles"`
	Enabled   bool   `json:"enabled"`
	LocalPort string `json:"localPort,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
}

type Status struct {
	Supported bool      `json:"supported"`
	Networks  []Network `json:"networks"`
	Port      int       `json:"port,omitempty"`
	PortRule  *Rule     `json:"portRule,omitempty"`
	Program   []Rule    `json:"program"`

	Firewall string `json:"firewall,omitempty"`
	Hint     string `json:"hint,omitempty"`
	Command  string `json:"command,omitempty"`

	Profiles []Profile `json:"profiles,omitempty"`
}

type Profile struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Inbound string `json:"inbound"`
}

func (s Status) profileOff(profile string) bool {
	for _, p := range s.Profiles {
		if strings.EqualFold(p.Name, profile) {
			return !p.Enabled
		}
	}
	return false
}

func (s Status) Keep(keep func(Network) bool) Status {
	var nets []Network
	for _, n := range s.Networks {
		if keep(n) {
			nets = append(nets, n)
		}
	}
	if len(nets) > 0 {
		s.Networks = nets
	}
	return s
}

func (s Status) Lists() Status {
	if s.Networks == nil {
		s.Networks = []Network{}
	}
	if s.Program == nil {
		s.Program = []Rule{}
	}
	return s
}

func (s Status) Blocked() bool {
	for _, n := range s.Networks {
		if !s.profileOff(profileOf(n)) && s.blocks(profileOf(n)) {
			return true
		}
	}
	return false
}

func (s Status) blocks(profile string) bool {
	for _, r := range s.Program {
		if r.Enabled && strings.EqualFold(r.Action, "Block") && s.covers(r) && onProfile(r, profile) {
			return true
		}
	}
	return false
}

func (s Status) covers(r Rule) bool {
	if r.Protocol != "" && !strings.EqualFold(r.Protocol, "TCP") && !strings.EqualFold(r.Protocol, "Any") {
		return false
	}
	if r.LocalPort == "" || strings.EqualFold(r.LocalPort, "Any") || s.Port == 0 {
		return true
	}
	for p := range strings.SplitSeq(r.LocalPort, ",") {
		p = strings.TrimSpace(p)
		if from, to, ranged := strings.Cut(p, "-"); ranged {
			low, lowErr := strconv.Atoi(strings.TrimSpace(from))
			high, highErr := strconv.Atoi(strings.TrimSpace(to))
			if lowErr == nil && highErr == nil && low <= s.Port && s.Port <= high {
				return true
			}
			continue
		}
		if p == strconv.Itoa(s.Port) {
			return true
		}
	}
	return false
}

func onProfile(r Rule, profile string) bool {
	return strings.EqualFold(r.Profiles, "Any") || strings.Contains(strings.ToLower(r.Profiles), strings.ToLower(profile))
}

func profileOf(n Network) string {
	if strings.EqualFold(n.Category, "DomainAuthenticated") {
		return "Domain"
	}
	return n.Category
}

func (s Status) OnPublic() bool {
	for _, n := range s.Networks {
		if strings.EqualFold(n.Category, "Public") {
			return true
		}
	}
	return false
}

func (s Status) Allows(profile string) bool {
	rules := append([]Rule{}, s.Program...)
	if s.PortRule != nil {
		rules = append(rules, *s.PortRule)
	}
	for _, r := range rules {
		if !r.Enabled || !strings.EqualFold(r.Action, "Allow") || !s.covers(r) {
			continue
		}
		if onProfile(r, profile) {
			return true
		}
	}
	return false
}

func (s Status) Reachable() bool {
	if !s.Supported {
		return true
	}
	if len(s.Networks) == 0 {
		return false
	}
	for _, n := range s.Networks {
		profile := profileOf(n)
		if s.profileOff(profile) {
			continue
		}
		if s.blocks(profile) || !s.Allows(profile) {
			return false
		}
	}
	return true
}

func RuleName(port int) string { return fmt.Sprintf("Yoru (LAN, TCP %d)", port) }

func PeerRuleName(port int) string { return fmt.Sprintf("Yoru (torrent peers, %d)", port) }

var ErrCancelled = fmt.Errorf("the administrator prompt was declined")
