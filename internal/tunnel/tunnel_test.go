package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	switch os.Getenv("FAKE_CLOUDFLARED") {
	case "ok":
		fmt.Fprintln(os.Stderr, "2026-11-10T10:00:00Z INF Requesting new quick Tunnel on trycloudflare.com...")
		fmt.Fprintln(os.Stderr, "2026-11-10T10:00:01Z INF |  https://quiet-river-bend.trycloudflare.com  |")
		fmt.Fprintln(os.Stderr, "2026-11-10T10:00:02Z INF Registered tunnel connection connIndex=0 protocol=quic")
		time.Sleep(time.Minute)
		os.Exit(0)
	case "fail":
		fmt.Fprintln(os.Stderr, "2026-11-10T10:00:00Z ERR failed to request quick Tunnel: 429 Too Many Requests")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func fake(t *testing.T, mode string) *Tunnel {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_CLOUDFLARED", mode)
	tun := New(func() string { return exe }, slog.New(slog.DiscardHandler))
	t.Cleanup(tun.Close)
	return tun
}

func TestOpenReturnsTheAddressOnceConnected(t *testing.T) {
	tun := fake(t, "ok")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	url, err := tun.Open(ctx, "http://127.0.0.1:1")
	if err != nil || url != "https://quiet-river-bend.trycloudflare.com" {
		t.Fatalf("url %q, err %v", url, err)
	}
	if again, err := tun.Open(ctx, "http://127.0.0.1:1"); err != nil || again != url {
		t.Fatalf("a second room got %q, %v; want the same tunnel", again, err)
	}
	if tun.URL() != url {
		t.Fatalf("URL() = %q", tun.URL())
	}
	tun.Close()
	if tun.URL() != "" {
		t.Fatal("still open after Close")
	}
}

func TestOpenSaysWhyCloudflaredStopped(t *testing.T) {
	tun := fake(t, "fail")
	_, err := tun.Open(context.Background(), "http://127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "429 Too Many Requests") || strings.Contains(err.Error(), "ERR") {
		t.Fatalf("err = %v; want cloudflared's own reason, without its log prefix", err)
	}
}
