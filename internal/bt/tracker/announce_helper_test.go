package tracker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/anacrolix/log"

	"yoru/internal/bt/dht/krpc"
	trHttp "yoru/internal/bt/tracker/http"
)

type Announce struct {
	TrackerUrl          string
	Request             AnnounceRequest
	HostHeader          string
	HttpProxy           func(*http.Request) (*url.URL, error)
	HttpRequestDirector func(*http.Request) error
	DialContext         func(ctx context.Context, network, addr string) (net.Conn, error)
	ListenPacket        func(network, addr string) (net.PacketConn, error)
	ServerName          string
	UserAgent           string
	UdpNetwork          string
	ClientIp4           krpc.NodeAddr
	ClientIp6           krpc.NodeAddr
	Context             context.Context
	Logger              log.Logger
}

func (me Announce) Do() (res AnnounceResponse, err error) {
	cl, err := NewClient(me.TrackerUrl, NewClientOpts{
		Http: trHttp.NewClientOpts{
			Proxy:       me.HttpProxy,
			DialContext: me.DialContext,
		},
		UdpNetwork:   me.UdpNetwork,
		Logger:       me.Logger.WithContextValue(fmt.Sprintf("tracker client for %q", me.TrackerUrl)),
		ListenPacket: me.ListenPacket,
	})
	if err != nil {
		return
	}
	defer cl.Close()
	if me.Context == nil {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTrackerAnnounceTimeout)
		defer cancel()
		me.Context = ctx
	}
	return cl.Announce(me.Context, me.Request, trHttp.AnnounceOpt{
		UserAgent:           me.UserAgent,
		HostHeader:          me.HostHeader,
		ClientIp4:           me.ClientIp4.IP,
		ClientIp6:           me.ClientIp6.IP,
		HttpRequestDirector: me.HttpRequestDirector,
	})
}
