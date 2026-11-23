package udp

import (
	"context"
	"net"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func fakeResolver(t *testing.T) *net.Resolver {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var p dnsmessage.Parser
			h, err := p.Start(buf[:n])
			if err != nil {
				continue
			}
			q, err := p.Question()
			if err != nil {
				continue
			}
			b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, RecursionAvailable: true})
			b.StartQuestions()
			b.Question(q)
			b.StartAnswers()
			hdr := dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 60}
			switch q.Type {
			case dnsmessage.TypeA:
				b.AResource(hdr, dnsmessage.AResource{A: [4]byte{127, 0, 0, 1}})
			case dnsmessage.TypeAAAA:
				b.AAAAResource(hdr, dnsmessage.AAAAResource{AAAA: [16]byte{15: 1}})
			}
			if msg, err := b.Finish(); err == nil {
				pc.WriteTo(msg, addr)
			}
		}
	}()
	server := pc.LocalAddr().String()
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "udp", server)
	}}
}

func TestClientWriterResolveVia(t *testing.T) {
	r := fakeResolver(t)
	for _, c := range []struct {
		network, address, want string
	}{
		{"udp", "tracker.test:6969", "127.0.0.1:6969"},
		{"udp4", "tracker.test:6969", "127.0.0.1:6969"},
		{"udp6", "tracker.test:6969", "[::1]:6969"},
		{"udp", "tracker.test:domain", "127.0.0.1:53"},
	} {
		got, err := clientWriter{network: c.network, address: c.address, resolver: r}.resolve()
		if err != nil {
			t.Errorf("%s %s: %v", c.network, c.address, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("%s %s resolved to %s, want %s", c.network, c.address, got, c.want)
		}
	}
}
