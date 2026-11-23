package udp

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/anacrolix/log"
	"github.com/protolambda/ctxlock"
	"yoru/internal/bt/dht/krpc"
)

type Client struct {
	mu             ctxlock.Lock
	connId         ConnectionId
	connIdIssued   time.Time
	forceReconnect atomic.Bool

	shouldReconnectOverride func() bool

	Dispatcher *Dispatcher
	Writer     io.Writer
}

func (cl *Client) Announce(
	ctx context.Context, req AnnounceRequest, opts Options,
	ipv6 func(net.Addr) bool,
) (
	respHdr AnnounceResponseHeader,
	peers AnnounceResponsePeers,
	err error,
) {
	respBody, addr, err := cl.request(ctx, ActionAnnounce, append(mustMarshal(req), opts.Encode()...))
	if err != nil {
		return
	}
	r := bytes.NewBuffer(respBody)
	err = Read(r, &respHdr)
	if err != nil {
		err = fmt.Errorf("reading response header: %w", err)
		return
	}
	if ipv6(addr) {
		peers = &krpc.CompactIPv6NodeAddrs{}
	} else {
		peers = &krpc.CompactIPv4NodeAddrs{}
	}
	err = peers.UnmarshalBinary(r.Bytes())
	if err != nil {
		err = fmt.Errorf("reading response peers: %w", err)
	}
	return
}

func (cl *Client) shouldReconnectDefault() bool {
	return cl.forceReconnect.Load() || cl.connIdIssued.IsZero() || time.Since(cl.connIdIssued) >= time.Minute
}

func (cl *Client) shouldReconnect() bool {
	if cl.shouldReconnectOverride != nil {
		return cl.shouldReconnectOverride()
	}
	return cl.shouldReconnectDefault()
}

func (cl *Client) connect(ctx context.Context) (err error) {
	if !cl.shouldReconnect() {
		return nil
	}
	return cl.doConnectRoundTrip(ctx)
}

func (cl *Client) doConnectRoundTrip(ctx context.Context) (err error) {
	respBody, _, err := cl.request(ctx, ActionConnect, nil)
	if err != nil {
		return err
	}
	var connResp ConnectionResponse
	err = binary.Read(bytes.NewReader(respBody), binary.BigEndian, &connResp)
	if err != nil {
		return
	}
	cl.connId = connResp.ConnectionId
	cl.connIdIssued = time.Now()
	cl.forceReconnect.Store(false)
	return
}

func (cl *Client) connIdForRequest(ctx context.Context, action Action) (id ConnectionId, err error) {
	if action == ActionConnect {
		id = ConnectRequestConnectionId
		return
	}
	err = cl.connect(ctx)
	if err != nil {
		return
	}
	id = cl.connId
	return
}

func (cl *Client) writeRequest(
	ctx context.Context, action Action, body []byte, tId TransactionId, buf *bytes.Buffer,
) (
	err error,
) {
	var connId ConnectionId
	if action == ActionConnect {
		connId = ConnectRequestConnectionId
	} else {
		err = cl.mu.LockCtx(ctx)
		if err != nil {
			return fmt.Errorf("locking connection id: %w", err)
		}
		defer cl.mu.Unlock()
		connId, err = cl.connIdForRequest(ctx, action)
		if err != nil {
			return
		}
	}
	buf.Reset()
	err = Write(buf, RequestHeader{
		ConnectionId:  connId,
		Action:        action,
		TransactionId: tId,
	})
	if err != nil {
		panic(err)
	}
	buf.Write(body)
	_, err = cl.Writer.Write(buf.Bytes())
	return
}

func (cl *Client) requestWriter(
	ctx context.Context,
	action Action,
	body []byte,
	tId TransactionId,
) (err error) {
	var buf bytes.Buffer
	for n := 0; ; n++ {
		err = cl.writeRequest(ctx, action, body, tId, &buf)
		if err != nil {
			return
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(resendAfter(n)):
		}
	}
}

func resendAfter(n int) time.Duration {
	return min(timeout(n), 4*time.Second<<min(n, 4))
}

const ConnectionIdMissmatchNul = "Connection ID missmatch.\x00"

type ErrorResponse struct {
	Message string
}

func (me ErrorResponse) Error() string {
	return fmt.Sprintf("error response: %#q", me.Message)
}

func (cl *Client) request(
	ctx context.Context,
	action Action,
	body []byte,
) (respBody []byte, addr net.Addr, err error) {
	respChan := make(chan DispatchedResponse, 1)
	t := cl.Dispatcher.NewTransaction(func(dr DispatchedResponse) {
		select {
		case respChan <- dr:
		default:
		}
	})
	defer t.End()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	writeErr := make(chan error, 1)
	go func() {
		writeErr <- cl.requestWriter(ctx, action, body, t.Id())
	}()
	select {
	case dr := <-respChan:
		if dr.Header.Action == action {
			respBody = dr.Body
			addr = dr.Addr
		} else if dr.Header.Action == ActionError {
			stringBody := string(dr.Body)
			err = ErrorResponse{Message: stringBody}
			if stringBody == ConnectionIdMissmatchNul {
				err = log.WithLevel(log.Debug, err)
			}
			cl.forceReconnect.Store(true)
		} else {
			err = fmt.Errorf("unexpected response action %v", dr.Header.Action)
		}
	case err = <-writeErr:
		err = fmt.Errorf("write error: %w", err)
	case <-ctx.Done():
		err = context.Cause(ctx)
	}
	return
}
