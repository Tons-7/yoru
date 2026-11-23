package httpTracker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

type Client struct {
	hc   *http.Client
	url_ *url.URL
}

type (
	ProxyFunc       func(*http.Request) (*url.URL, error)
	DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)
)

type NewClientOpts struct {
	Proxy          ProxyFunc
	DialContext    DialContextFunc
	AllowKeepAlive bool
}

func NewClient(url_ *url.URL, opts NewClientOpts) Client {
	dial := opts.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	allowLocal := localHost(url_.Hostname())
	return Client{
		url_: url_,
		hc: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					conn, err := dial(ctx, network, addr)
					if err != nil {
						return conn, err
					}
					ap, err := netip.ParseAddrPort(conn.RemoteAddr().String())
					if err != nil {
						return conn, nil
					}
					remote := ap.Addr().Unmap()
					if (remote.IsLoopback() && !loopbackAllowed) || (!allowLocal && localAddr(remote)) {
						conn.Close()
						return nil, fmt.Errorf("tracker %s led to local address %s", url_.Host, remote)
					}
					return conn, nil
				},
				Proxy:             opts.Proxy,
				DisableKeepAlives: !opts.AllowKeepAlive,
			},
		},
	}
}

var loopbackAllowed = testing.Testing()

func localHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(host)
	return err == nil && localAddr(a)
}

func localAddr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsUnspecified()
}

func (cl Client) Close() error {
	cl.hc.CloseIdleConnections()
	return nil
}
