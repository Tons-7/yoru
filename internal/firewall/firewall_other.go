//go:build !windows

package firewall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Check only looks, never needing root: firewalld or ufw may block the port; macOS asks on its own.
func Check(ctx context.Context, port int, _ string) (Status, error) {
	switch runtime.GOOS {
	case "linux":
		if active(ctx, "firewalld") {
			return Status{
				Firewall: "firewalld",
				Hint:     "firewalld is running and blocks other devices until Yoru's port is opened.",
				Command:  fmt.Sprintf("sudo firewall-cmd --permanent --add-port=%d/tcp && sudo firewall-cmd --reload", port),
			}, nil
		}
		if active(ctx, "ufw") {
			return Status{
				Firewall: "ufw",
				Hint:     "ufw is running and blocks other devices until Yoru's port is allowed.",
				Command:  fmt.Sprintf("sudo ufw allow %d/tcp", port),
			}, nil
		}
	case "darwin":
		out, _ := exec.CommandContext(ctx, "/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate").Output()
		if strings.Contains(string(out), "enabled") {
			return Status{
				Firewall: "macOS firewall",
				Hint: "macOS asks the first time Yoru is opened to the network. If that was declined, " +
					"allow Yoru in System Settings › Network › Firewall › Options.",
			}, nil
		}
	}
	return Status{}, nil
}

// CheckPeers is Check for the torrent peer port, TCP and UDP from anywhere.
func CheckPeers(ctx context.Context, port int, _ string) (Status, error) {
	switch runtime.GOOS {
	case "linux":
		if active(ctx, "firewalld") {
			return Status{
				Firewall: "firewalld",
				Hint:     "firewalld is running and keeps torrent peers from connecting in; downloads find fewer peers.",
				Command: fmt.Sprintf("sudo firewall-cmd --permanent --add-port=%d/tcp --add-port=%d/udp && sudo firewall-cmd --reload",
					port, port),
			}, nil
		}
		if active(ctx, "ufw") {
			return Status{
				Firewall: "ufw",
				Hint:     "ufw is running and keeps torrent peers from connecting in; downloads find fewer peers.",
				Command:  fmt.Sprintf("sudo ufw allow %d", port),
			}, nil
		}
	case "darwin":
		return Check(ctx, port, "")
	}
	return Status{}, nil
}

func AllowPeers(context.Context, int, string) error {
	return errors.New("Yoru changes the firewall only on Windows; run the command shown instead")
}

func active(ctx context.Context, unit string) bool {
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", unit).Output()
	if err != nil || strings.TrimSpace(string(out)) != "active" {
		return false
	}
	// ufw's unit is active even with ufw disabled, Ubuntu's default; its own config says whether it filters.
	if unit == "ufw" {
		if conf, err := os.ReadFile("/etc/ufw/ufw.conf"); err == nil {
			for line := range strings.SplitSeq(string(conf), "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "ENABLED="); ok {
					return strings.EqualFold(strings.Trim(v, `"' `), "yes")
				}
			}
		}
	}
	return true
}

func Allow(context.Context, int, string, bool) error {
	return errors.New("Yoru changes the firewall only on Windows; run the command shown instead")
}

func OpenNetworkSettings() error {
	return errors.New("only on Windows")
}
