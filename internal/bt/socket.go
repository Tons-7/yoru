package bt

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2"
)

type Listener interface {
	Accept() (net.Conn, error)

	Addr() net.Addr
}

type socket interface {
	Listener
	Dialer
	Close() error
}

func listen(n network, addr string, logger log.Logger) (socket, error) {
	switch {
	case n.Tcp:
		return listenTcp(n.String(), addr)
	case n.Udp:
		return listenUtp(n.String(), addr, logger)
	default:
		panic(n)
	}
}

const dialTcpFromListenPort = false

var SocketIPTypeOfService = 0

var tcpListenConfig = net.ListenConfig{
	Control: func(network, address string, c syscall.RawConn) (err error) {
		controlErr := c.Control(func(fd uintptr) {
			if dialTcpFromListenPort {
				err = setReusePortSockOpts(fd)
			}
			if err == nil && SocketIPTypeOfService != 0 {
				err = setSockIPTOS(fd, SocketIPTypeOfService)
			}
		})
		if err != nil {
			return
		}
		err = controlErr
		return
	},
	KeepAlive: -1,
}

func listenTcp(network, address string) (s socket, err error) {
	l, err := tcpListenConfig.Listen(context.Background(), network, address)
	if err != nil {
		return
	}
	netDialer := net.Dialer{
		FallbackDelay: -1,
		KeepAlive:     tcpListenConfig.KeepAlive,
		Control: func(network, address string, c syscall.RawConn) (err error) {
			controlErr := c.Control(func(fd uintptr) {
				err = setSockNoLinger(fd)
				if err != nil {
					log.Levelf(log.Debug, "error setting linger socket option on tcp socket: %v", err)
					err = nil
				}
				if dialTcpFromListenPort {
					err = setReusePortSockOpts(fd)
				}
				if err == nil && SocketIPTypeOfService != 0 {
					err = setSockIPTOS(fd, SocketIPTypeOfService)
				}
			})
			if err == nil {
				err = controlErr
			}
			return
		},
	}
	if dialTcpFromListenPort {
		netDialer.LocalAddr = l.Addr()
	} else if a, ok := l.Addr().(*net.TCPAddr); ok && !a.IP.IsUnspecified() {
		netDialer.LocalAddr = &net.TCPAddr{IP: a.IP}
	}
	s = tcpSocket{
		Listener: l,
		NetworkDialer: NetworkDialer{
			Network: network,
			Dialer:  &netDialer,
		},
	}
	return
}

type tcpSocket struct {
	net.Listener
	NetworkDialer
}

func listenAll(
	networks []network,
	getHost func(string) string,
	port int,
	logger log.Logger,
) ([]socket, error) {
	if len(networks) == 0 {
		return nil, nil
	}
	var nahs []networkAndHost
	for _, n := range networks {
		nahs = append(nahs, networkAndHost{n, getHost(n.String())})
	}
	for attempt := 1; ; attempt++ {
		ss, retry, err := listenAllRetry(nahs, port, logger)
		if !retry || attempt >= 100 {
			return ss, err
		}
	}
}

type networkAndHost struct {
	Network network
	Host    string
}

func isUnsupportedNetworkError(err error) bool {
	sysErr, ok := errors.AsType[*os.SyscallError](err)
	if !ok {
		return false
	}
	if sysErr.Syscall != "bind" {
		return false
	}
	text := strings.ToLower(sysErr.Err.Error())
	return strings.Contains(text, "assign requested address") || strings.Contains(text, "requested address is not valid")
}

func listenAllRetry(
	nahs []networkAndHost,
	port int,
	logger log.Logger,
) (ss []socket, retry bool, err error) {
	defer func() {
		if err != nil || retry {
			for _, s := range ss {
				s.Close()
			}
			ss = nil
		}
	}()
	g.MakeSliceWithCap(&ss, len(nahs))
	portStr := strconv.FormatInt(int64(port), 10)
	for _, nah := range nahs {
		var s socket
		s, err = listen(nah.Network, net.JoinHostPort(nah.Host, portStr), logger)
		if err != nil {
			if isUnsupportedNetworkError(err) {
				err = nil
				continue
			}
			if len(ss) == 0 {
				err = fmt.Errorf("first listen: %w", err)
			} else {
				err = fmt.Errorf("subsequent listen: %w", err)
			}
			retry = (missinggo.IsAddrInUse(err) || addrTakenOS(err)) && port == 0
			return
		}
		ss = append(ss, s)
		portStr = strconv.FormatInt(int64(missinggo.AddrPort(ss[0].Addr())), 10)
	}
	return
}

func listenUtp(network, addr string, logger log.Logger) (socket, error) {
	us, err := NewUtpSocket(network, addr, logger)
	return utpSocketSocket{us, network}, err
}

type utpSocketSocket struct {
	utpSocket
	network string
}

func (me utpSocketSocket) DialerNetwork() string {
	return me.network
}

func (me utpSocketSocket) Dial(ctx context.Context, addr string) (conn net.Conn, err error) {
	return me.utpSocket.DialContext(ctx, me.network, addr)
}
