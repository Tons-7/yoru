package engine

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"yoru/internal/bt"
	"yoru/internal/bt/bencode"
	"yoru/internal/bt/metainfo"
	"yoru/internal/torrent"
)

// bigFile is one size-byte file of random data in pieces of pieceLen, served by a seeder at each rate.
func bigFile(t *testing.T, size, pieceLen int64, rates ...int) (metainfo.MetaInfo, []*net.TCPAddr) {
	t.Helper()
	mi, seeds, _ := bigFileSeeders(t, size, pieceLen, rates...)
	return mi, seeds
}

// bigFileSeeders is bigFile with the seeding clients, to look at their peers.
func bigFileSeeders(t *testing.T, size, pieceLen int64, rates ...int) (metainfo.MetaInfo, []*net.TCPAddr, []*bt.Client) {
	t.Helper()
	dir := t.TempDir()
	data := make([]byte, size)
	r := rand.New(rand.NewChaCha8([32]byte{1}))
	for i := range data {
		data[i] = byte(r.Uint32())
	}
	path := filepath.Join(dir, "Show - 01.mkv")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	info := metainfo.Info{PieceLength: pieceLen}
	if err := info.BuildFromFilePath(path); err != nil {
		t.Fatal(err)
	}
	var mi metainfo.MetaInfo
	var err error
	if mi.InfoBytes, err = bencode.Marshal(info); err != nil {
		t.Fatal(err)
	}
	var seeds []*net.TCPAddr
	var clients []*bt.Client
	for _, bps := range rates {
		s := seeder(t, dir, mi, func(cfg *bt.ClientConfig) {
			cfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(bps), 64<<10)
		})
		seeds = append(seeds, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: s.LocalPort()})
		clients = append(clients, s)
	}
	return mi, seeds, clients
}

// A far read (a seek, MKV Cues) waited for the next refill, then behind the peer's queue: about 1 s at 2 MiB/s.
func TestFarReadIsNotQueuedBehindOtherPieces(t *testing.T) { farReads(t, 2<<20) }

// Whichever peer hears of a seek first takes it; a quicker one takes it over rather than wait on a slow one.
func TestFarReadGoesToTheQuickerPeer(t *testing.T) { farReads(t, 64<<10, 2<<20) }

// farReads seeks three times while the file downloads from seeders at rates, each read timed to its chunk.
func farReads(t *testing.T, rates ...int) {
	const pieceLen, size, chunk = 1 << 20, 96 << 20, 16 << 10
	mi, seeds := bigFile(t, size, pieceLen, rates...)
	var outstanding, useful, wanted, asked atomic.Int64
	e, id := seedEngine(t, mi, seeds, nil, func(cfg *bt.ClientConfig) {
		cfg.Callbacks.SentRequest = append(cfg.Callbacks.SentRequest, func(ev bt.PeerRequestEvent) {
			outstanding.Add(1)
			if int64(ev.Request.Index)*pieceLen+int64(ev.Request.Begin) == wanted.Load() {
				asked.CompareAndSwap(0, time.Now().UnixNano())
			}
		})
		cfg.Callbacks.DeletedRequest = append(cfg.Callbacks.DeletedRequest, func(bt.PeerRequestEvent) { outstanding.Add(-1) })
		cfg.Callbacks.ReceivedUsefulData = append(cfg.Callbacks.ReceivedUsefulData, func(ev bt.ReceivedUsefulDataEvent) {
			useful.Add(int64(len(ev.Message.Piece)))
		})
	})

	// The file downloads at full speed, so the queues are as deep as they get.
	time.Sleep(4 * time.Second)
	en, _ := e.entryByID(id)
	var bound int64
	for _, bps := range rates {
		bound += int64(float64(bps)*bt.RequestQueueTime.Seconds()/chunk) + int64(bt.MinRequestQueue)
	}
	if outstanding.Load() > bound {
		t.Errorf("%d requests queued; %s of the peers' speed is %d", outstanding.Load(), bt.RequestQueueTime, bound)
	}
	// Mid-piece, as seeks land, and at uneven gaps so they fall at different points of the refill rhythm.
	for i, target := range []int64{80<<20 + 768<<10, 60<<20 + 304<<10, 88<<20 + 912<<10} {
		// Pieces come in a spread order, so the nearest one with nothing of it here yet, wrapping round.
		for tries := 0; ; tries++ {
			if s := en.t.Piece(int(target / pieceLen)).State(); !s.Complete && !s.Partial {
				break
			}
			if tries == size/pieceLen {
				t.Fatalf("seek %d: every piece has started downloading", i)
			}
			target = (target + pieceLen) % size
		}
		queued := outstanding.Load()
		wanted.Store(target)
		asked.Store(0)
		req, _ := http.NewRequest(http.MethodGet, e.Client().StreamURL(id, 0), nil)
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", target, target+chunk-1))
		before, start := useful.Load(), time.Now()
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, err = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		took, ahead := time.Since(start), useful.Load()-before
		at := "before the seek"
		if ns := asked.Load(); ns != 0 {
			at = fmt.Sprintf("at %.2fs", time.Duration(ns-start.UnixNano()).Seconds())
		}
		t.Logf("seek %d: %d queued; took %.2fs with %.2f MiB arriving first; asked %s",
			i, queued, took.Seconds(), float64(ahead)/(1<<20), at)
		// Asked at once, ahead of the queue and from the seek on: only what was already on its way comes first.
		if took > 250*time.Millisecond {
			t.Errorf("seek %d took %s", i, took)
		}
		time.Sleep(time.Duration(700+600*i) * time.Millisecond)
	}
}

// The queue must cost no speed: a peer 150 ms away still gives nearly all its rate.
func TestFarPeerKeepsItsSpeed(t *testing.T) {
	const bps = 3 << 20
	mi, seeds := bigFile(t, 64<<20, 256<<10, bps)
	var useful atomic.Int64
	seedEngine(t, mi, seeds, delayDialer{delay: 150 * time.Millisecond}, func(cfg *bt.ClientConfig) {
		cfg.Callbacks.ReceivedUsefulData = append(cfg.Callbacks.ReceivedUsefulData, func(ev bt.ReceivedUsefulDataEvent) {
			useful.Add(int64(len(ev.Message.Piece)))
		})
	})
	time.Sleep(3 * time.Second)
	from, at := useful.Load(), time.Now()
	time.Sleep(6 * time.Second)
	got := float64(useful.Load()-from) / time.Since(at).Seconds()
	t.Logf("%.2f MiB/s from a 150 ms peer serving %.2f MiB/s", got/(1<<20), float64(bps)/(1<<20))
	if got < 0.9*bps {
		t.Errorf("a far peer gave %.2f MiB/s of %.2f", got/(1<<20), float64(bps)/(1<<20))
	}
}

// Settings' download cap holds mid-download, and lifting it gives the speed back.
func TestDownloadCap(t *testing.T) {
	const bps, limit = 4 << 20, 1 << 20
	mi, seeds := bigFile(t, 96<<20, 256<<10, bps)
	var useful atomic.Int64
	e, _ := seedEngine(t, mi, seeds, nil, func(cfg *bt.ClientConfig) {
		cfg.Callbacks.ReceivedUsefulData = append(cfg.Callbacks.ReceivedUsefulData, func(ev bt.ReceivedUsefulDataEvent) {
			useful.Add(int64(len(ev.Message.Piece)))
		})
	})
	speed := func(over time.Duration) float64 {
		// What was already on its way when the setting changed is not the new speed.
		time.Sleep(time.Second)
		from, at := useful.Load(), time.Now()
		time.Sleep(over)
		return float64(useful.Load()-from) / time.Since(at).Seconds()
	}
	e.SetLimits(torrent.Limits{DownloadBytes: limit, Upload: torrent.UploadWhileDownloading})
	capped := speed(4 * time.Second)
	e.SetLimits(torrent.Limits{Upload: torrent.UploadWhileDownloading})
	free := speed(3 * time.Second)
	t.Logf("%.2f MiB/s under a %.2f MiB/s cap, %.2f MiB/s without", capped/(1<<20), float64(limit)/(1<<20), free/(1<<20))
	// The cap counts message overhead too, so useful data sits a little under it.
	if capped > 1.1*limit || capped < 0.7*limit {
		t.Errorf("%.2f MiB/s under a %.2f MiB/s cap", capped/(1<<20), float64(limit)/(1<<20))
	}
	if free < 2*limit {
		t.Errorf("only %.2f MiB/s once the cap was lifted", free/(1<<20))
	}
}

// seedEngine adds the file to an engine whose only peers are the seeders; dialer replaces the plain ones, and
// setup runs before the engine starts.
func seedEngine(t *testing.T, mi metainfo.MetaInfo, seeds []*net.TCPAddr, dialer bt.Dialer,
	configure func(*bt.ClientConfig), setup ...func(*Engine)) (*Engine, int) {
	t.Helper()
	e, err := New(Options{CacheDir: t.TempDir(), SessionDir: t.TempDir()}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range setup {
		f(e)
	}
	e.configure = func(cfg *bt.ClientConfig) {
		quietConfig(cfg)
		configure(cfg)
		if dialer != nil {
			cfg.DialForPeerConns = false
		}
	}
	e.onNew = func(tt *bt.Torrent) {
		for _, s := range seeds {
			tt.AddPeers([]bt.PeerInfo{{Addr: s, Trusted: true}})
		}
	}
	t.Cleanup(e.Stop)
	cx := ctx(t)
	if err := e.Ensure(cx); err != nil {
		t.Fatal(err)
	}
	if dialer != nil {
		e.cl.AddDialer(dialer)
	}
	in, err := e.Client().Inspect(cx, magnetOf(t, &mi))
	if err != nil {
		t.Fatal(err)
	}
	added, err := e.Client().Add(cx, magnetOf(t, &mi), in.Details.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	return e, added.ID
}

// delayDialer connects over TCP and holds every byte from the peer back by delay, as a distant line would.
type delayDialer struct{ delay time.Duration }

func (d delayDialer) DialerNetwork() string { return "tcp" }

func (d delayDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	c, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return newDelayConn(c, d.delay), nil
}

type delayed struct {
	at   time.Time
	data []byte
	err  error
}

// delayConn reads ahead into a line that releases each read only once delay has passed since it arrived.
type delayConn struct {
	net.Conn
	line chan delayed
	rest []byte
}

func newDelayConn(c net.Conn, delay time.Duration) *delayConn {
	d := &delayConn{Conn: c, line: make(chan delayed, 4096)}
	go func() {
		for {
			buf := make([]byte, 32<<10)
			n, err := c.Read(buf)
			d.line <- delayed{time.Now().Add(delay), buf[:n], err}
			if err != nil {
				close(d.line)
				return
			}
		}
	}()
	return d
}

func (d *delayConn) Read(p []byte) (int, error) {
	if len(d.rest) == 0 {
		next, ok := <-d.line
		if !ok {
			return 0, io.EOF
		}
		time.Sleep(time.Until(next.at))
		if len(next.data) == 0 {
			return 0, next.err
		}
		d.rest = next.data
	}
	n := copy(p, d.rest)
	d.rest = d.rest[n:]
	return n, nil
}
