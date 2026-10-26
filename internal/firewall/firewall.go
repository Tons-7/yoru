// Package firewall checks, and on request adds, the rule that lets other devices at home reach Yoru.
package firewall

import (
	"fmt"
	"strconv"
	"strings"
)

// Network is one connection as Windows classifies it; Public is its pick for an untrusted network.
type Network struct {
	Name      string `json:"name"`
	Interface string `json:"interface"`
	Category  string `json:"category"`
}

// Rule is an inbound rule touching Yoru: its port rule, or one Windows made for yoru.exe at its prompt.
type Rule struct {
	Name     string `json:"name"`
	Action   string `json:"action"`
	Profiles string `json:"profiles"`
	Enabled  bool   `json:"enabled"`
	// LocalPort and Protocol are the rule's scope: a rule for another port says nothing about this one.
	LocalPort string `json:"localPort,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
}

type Status struct {
	Supported bool      `json:"supported"`
	Networks  []Network `json:"networks"`
	// Port is the one these rules are judged for.
	Port int `json:"port,omitempty"`
	// PortRule is Yoru's own rule for its port, if added.
	PortRule *Rule `json:"portRule,omitempty"`
	// Program lists the rules for yoru.exe; a Block among them beats any allow.
	Program []Rule `json:"program"`

	// Off Windows Yoru only reports which firewall runs and what to do; changing it there needs root and
	// differs per distribution.
	Firewall string `json:"firewall,omitempty"`
	Hint     string `json:"hint,omitempty"`
	Command  string `json:"command,omitempty"`
}

// Keep judges only the networks keep accepts (phones reach Yoru on the home network, not a VPN's or
// Hyper-V's); when it accepts none, the status stays whole rather than judged on nothing.
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

// Lists: empty, not nil; a nil list goes out as null, which crashed Settings (macOS and Linux fill neither).
func (s Status) Lists() Status {
	if s.Networks == nil {
		s.Networks = []Network{}
	}
	if s.Program == nil {
		s.Program = []Rule{}
	}
	return s
}

// Blocked reports a block rule for yoru.exe on a connected network's profile; it wins over every allow there.
func (s Status) Blocked() bool {
	for _, n := range s.Networks {
		if s.blocks(profileOf(n)) {
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

// covers: the rule speaks for this port; one for another port (the torrent peers') does not.
func (s Status) covers(r Rule) bool {
	if strings.EqualFold(r.Protocol, "UDP") {
		return false
	}
	if r.LocalPort == "" || strings.EqualFold(r.LocalPort, "Any") || s.Port == 0 {
		return true
	}
	for p := range strings.SplitSeq(r.LocalPort, ",") {
		if strings.TrimSpace(p) == strconv.Itoa(s.Port) {
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

// OnPublic reports a connected network Windows treats as Public.
func (s Status) OnPublic() bool {
	for _, n := range s.Networks {
		if strings.EqualFold(n.Category, "Public") {
			return true
		}
	}
	return false
}

// Allows reports whether an enabled allow rule covers the profile.
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

// Reachable reports whether a phone on each connected network would get in.
func (s Status) Reachable() bool {
	if !s.Supported {
		return true
	}
	if len(s.Networks) == 0 {
		return false
	}
	for _, n := range s.Networks {
		profile := profileOf(n)
		if s.blocks(profile) || !s.Allows(profile) {
			return false
		}
	}
	return true
}

// RuleName carries the port, so a changed port gets a rule of its own.
func RuleName(port int) string { return fmt.Sprintf("Yoru (LAN, TCP %d)", port) }

// PeerRuleName names the pair of rules (TCP, UDP) for the torrent peer port.
func PeerRuleName(port int) string { return fmt.Sprintf("Yoru (torrent peers, %d)", port) }

// ErrCancelled: the administrator prompt was declined.
var ErrCancelled = fmt.Errorf("the administrator prompt was declined")
