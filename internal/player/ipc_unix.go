//go:build !windows

package player

import (
	"context"
	"net"
	"os"
	"path/filepath"
)

func dialIPC(ctx context.Context, socket string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", socket)
}

func defaultSocket() string { return filepath.Join(os.TempDir(), "yoru-mpv.sock") }

func dropSocket(socket string) { os.Remove(socket) }
