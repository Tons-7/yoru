package tracker

import (
	"context"
	"encoding/binary"

	"github.com/anacrolix/generics"

	trHttp "yoru/internal/bt/tracker/http"
	"yoru/internal/bt/tracker/udp"
	"yoru/internal/bt/types/infohash"
)

type udpClient struct {
	cl         *udp.ConnClient
	requestUri string
}

func (c *udpClient) Scrape(ctx context.Context, ihs []infohash.T) (out udp.ScrapeResponse, err error) {
	return c.cl.Client.Scrape(
		ctx,
		generics.SliceMap(ihs, func(from infohash.T) udp.InfoHash {
			return from
		}),
	)
}

func (c *udpClient) Close() error {
	return c.cl.Close()
}

func (c *udpClient) Announce(
	ctx context.Context,
	req AnnounceRequest,
	opts trHttp.AnnounceOpt,
) (res AnnounceResponse, err error) {
	if req.IPAddress == 0 && opts.ClientIp4 != nil {
		// Big-endian bytes into a native uint32, put right when the UDP client writes it; sent on IPv6
		// announces too.
		req.IPAddress = binary.BigEndian.Uint32(opts.ClientIp4.To4())
	}
	h, nas, err := c.cl.Announce(ctx, req, udp.Options{RequestUri: c.requestUri})
	if err != nil {
		return
	}
	res.Interval = h.Interval
	res.Leechers = h.Leechers
	res.Seeders = h.Seeders
	for _, cp := range nas.NodeAddrs() {
		res.Peers = append(res.Peers, trHttp.Peer{}.FromNodeAddr(cp))
	}
	return
}
