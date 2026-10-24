package httpTracker

import (
	"context"
	"net"
	"net/http"
	"net/url"
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

// NewClient checks an HTTPS tracker's certificate against the host each request goes to, a redirect's too,
// so no one in the way can hand out a peer list of their own.
func NewClient(url_ *url.URL, opts NewClientOpts) Client {
	return Client{
		url_: url_,
		hc: &http.Client{
			Transport: &http.Transport{
				DialContext: opts.DialContext,
				Proxy:       opts.Proxy,
				// This is for S3 trackers that hold connections open.
				DisableKeepAlives: !opts.AllowKeepAlive,
			},
		},
	}
}

func (cl Client) Close() error {
	cl.hc.CloseIdleConnections()
	return nil
}
