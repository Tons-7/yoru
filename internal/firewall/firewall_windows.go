package firewall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"yoru/internal/proc"
)

var (
	powershell = proc.PowerShell
	encoded    = proc.Encoded
	quote      = proc.Quote
)

const statusScript = `$ErrorActionPreference = 'SilentlyContinue'
$nets = @(Get-NetConnectionProfile | ForEach-Object {
  [pscustomobject]@{ name = $_.Name; interface = $_.InterfaceAlias; category = $_.NetworkCategory.ToString() } })
function Shape($r) { $pf = $r | Get-NetFirewallPortFilter; [pscustomobject]@{ name = $r.DisplayName; action = $r.Action.ToString(); profiles = $r.Profile.ToString(); enabled = ($r.Enabled.ToString() -eq 'True'); localPort = (@($pf.LocalPort) -join ','); protocol = [string]$pf.Protocol } }
$port = @(Get-NetFirewallRule -DisplayName %s | ForEach-Object { Shape $_ })
$prog = @(Get-NetFirewallApplicationFilter -Program %s | Get-NetFirewallRule | Where-Object { $_.Direction.ToString() -eq 'Inbound' } | ForEach-Object { Shape $_ })
$profiles = @(Get-NetFirewallProfile | ForEach-Object {
  [pscustomobject]@{ name = $_.Name; enabled = ($_.Enabled.ToString() -eq 'True'); inbound = $_.DefaultInboundAction.ToString() } })
[pscustomobject]@{ networks = $nets; port = $port; program = $prog; profiles = $profiles } | ConvertTo-Json -Depth 4 -Compress`

func Check(ctx context.Context, port int, exe string) (Status, error) {
	return check(ctx, port, []string{RuleName(port)}, exe)
}

func CheckPeers(ctx context.Context, port int, exe string) (Status, error) {
	return check(ctx, port, []string{PeerRuleName(port)}, exe)
}

func check(ctx context.Context, port int, rules []string, exe string) (Status, error) {
	quoted := make([]string, len(rules))
	for i, r := range rules {
		quoted[i] = quote(r)
	}
	out, err := powershell(ctx, "-EncodedCommand",
		encoded(fmt.Sprintf(statusScript, strings.Join(quoted, ","), quote(exe)))).Output()
	if err != nil {
		return Status{Supported: true}, fmt.Errorf("read firewall: %w", err)
	}
	var raw struct {
		Networks []Network `json:"networks"`
		Port     []Rule    `json:"port"`
		Program  []Rule    `json:"program"`
		Profiles []Profile `json:"profiles"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Status{Supported: true}, fmt.Errorf("read firewall: %w", err)
	}
	st := Status{Supported: true, Port: port, Networks: raw.Networks, Program: raw.Program, Profiles: raw.Profiles}
	for i := range raw.Port {
		if st.PortRule == nil || (raw.Port[i].Enabled && !st.PortRule.Enabled) {
			st.PortRule = &raw.Port[i]
		}
	}
	return st, nil
}

const allowScript = `$ErrorActionPreference = 'Stop'
Remove-NetFirewallRule -DisplayName 'Yoru (LAN, TCP *)' -ErrorAction SilentlyContinue
New-NetFirewallRule -DisplayName %[1]s -Description 'Lets devices on this network open Yoru.' -Direction Inbound -Action Allow -Protocol TCP -LocalPort %[2]d -RemoteAddress LocalSubnet -Profile %[3]s | Out-Null
Get-NetFirewallApplicationFilter -Program %[4]s -ErrorAction SilentlyContinue | Get-NetFirewallRule | Where-Object { $_.Direction.ToString() -eq 'Inbound' -and $_.Action.ToString() -eq 'Block' -and (%[5]s -or $_.Profile.ToString() -ne 'Public') } | Remove-NetFirewallRule`

func Allow(ctx context.Context, port int, exe string, public bool) error {
	profiles, publicToo := "Private,Domain", "$false"
	if public {
		profiles, publicToo = "Private,Domain,Public", "$true"
	}
	return elevated(ctx, fmt.Sprintf(allowScript, quote(RuleName(port)), port, profiles, quote(exe), publicToo))
}

const allowPeersScript = `$ErrorActionPreference = 'Stop'
Remove-NetFirewallRule -DisplayName %[1]s -ErrorAction SilentlyContinue
foreach ($proto in 'TCP', 'UDP') {
  New-NetFirewallRule -DisplayName %[1]s -Description 'Lets torrent peers connect to Yoru.' -Direction Inbound -Action Allow -Protocol $proto -LocalPort %[2]d -Program %[3]s -Profile Any | Out-Null
}
Get-NetFirewallApplicationFilter -Program %[3]s -ErrorAction SilentlyContinue | Get-NetFirewallRule | Where-Object { $_.Direction.ToString() -eq 'Inbound' -and $_.Action.ToString() -eq 'Block' } | Remove-NetFirewallRule`

func AllowPeers(ctx context.Context, port int, exe string) error {
	return elevated(ctx, fmt.Sprintf(allowPeersScript, quote(PeerRuleName(port)), port, quote(exe)))
}

func elevated(ctx context.Context, script string) error {
	launcher := fmt.Sprintf(`try { $p = Start-Process powershell -Verb RunAs -Wait -PassThru -WindowStyle Hidden -ArgumentList '-NoProfile','-NonInteractive','-EncodedCommand','%s'; exit $p.ExitCode } catch { $e = $_.Exception; while ($e) { if ($e.NativeErrorCode -eq 1223) { exit 1223 }; $e = $e.InnerException }; exit 1 }`,
		encoded(script))
	err := powershell(ctx, "-Command", launcher).Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1223 {
		return ErrCancelled
	}
	if err != nil {
		return fmt.Errorf("add firewall rule: %w", err)
	}
	return nil
}

func OpenNetworkSettings() error {
	cmd := exec.Command("explorer", "ms-settings:network-status")
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
