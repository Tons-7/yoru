package utp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	backlog = 50

	minMTU         = 1438
	maxHeaderSize  = 20 + 2 + (((maxUnackedInbound+7)/8)+3)/4*4
	maxPayloadSize = minMTU - maxHeaderSize
	maxRecvSize    = 0x2000

	maxUnackedInbound = 256
	maxUnackedSends   = 256

	readBufferLen = 1 << 20

	pendingSendStateDelay = 500 * time.Microsecond
)

var (
	sendBufferPool = sync.Pool{
		New: func() interface{} { return make([]byte, minMTU) },
	}
	initialLatency    time.Duration
	writeTimeout      time.Duration
	packetReadTimeout time.Duration
)

func setDefaultDurations() {
	initialLatency = 400 * time.Millisecond
	writeTimeout = 15 * time.Second
	packetReadTimeout = 2 * time.Minute
}

func init() {
	setDefaultDurations()
}

type resolvedAddrStr string

type read struct {
	data []byte
	from net.Addr
}

type syn struct {
	seq_nr, conn_id uint16
	addr            string
}

var (
	mu                         sync.RWMutex
	sockets                    = map[*Socket]struct{}{}
	logLevel                   = 0
	artificialPacketDropChance = 0.0
)

func init() {
	logLevel, _ = strconv.Atoi(os.Getenv("GO_UTP_LOGGING"))
	fmt.Sscanf(os.Getenv("GO_UTP_PACKET_DROP"), "%f", &artificialPacketDropChance)
}

var (
	errClosed               = errors.New("closed")
	errTimeout    net.Error = timeoutError{"i/o timeout"}
	errAckTimeout           = timeoutError{"timed out waiting for ack"}
)

type timeoutError struct {
	msg string
}

func (me timeoutError) Timeout() bool   { return true }
func (me timeoutError) Error() string   { return me.msg }
func (me timeoutError) Temporary() bool { return false }

type st int

func (me st) String() string {
	switch me {
	case stData:
		return "stData"
	case stFin:
		return "stFin"
	case stState:
		return "stState"
	case stReset:
		return "stReset"
	case stSyn:
		return "stSyn"
	default:
		panic(fmt.Sprintf("%d", me))
	}
}

const (
	stData  st = 0
	stFin      = 1
	stState    = 2
	stReset    = 3
	stSyn      = 4

	stMax = stSyn
)

type recv struct {
	seen bool
	data []byte
	Type st
}

func Dial(addr string) (net.Conn, error) {
	return DialContext(context.Background(), addr)
}

func DialContext(ctx context.Context, addr string) (nc net.Conn, err error) {
	s, err := NewSocket("udp", ":0")
	if err != nil {
		return
	}
	defer s.Close()
	return s.DialContext(ctx, "", addr)
}

func Listen(laddr string) (net.Listener, error) {
	return NewSocket("udp", laddr)
}

func nowTimestamp() uint32 {
	return uint32(time.Now().UnixNano() / int64(time.Microsecond))
}

func seqLess(a, b uint16) bool {
	if b < 0x8000 {
		return a < b || a >= b-0x8000
	} else {
		return a < b && a >= b-0x8000
	}
}
