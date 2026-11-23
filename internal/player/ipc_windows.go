//go:build windows

package player

import (
	"context"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

func dialIPC(ctx context.Context, socket string) (net.Conn, error) {
	timeout := 2 * time.Second
	return winio.DialPipe(socket, &timeout)
}

func defaultSocket() string { return `\\.\pipe\yoru-mpv` }

func dropSocket(string) {}
