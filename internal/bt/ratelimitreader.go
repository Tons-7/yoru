package bt

import (
	"io"
	"time"

	"github.com/anacrolix/missinggo/v2/panicif"
	"golang.org/x/time/rate"
)

func newRateLimitedReader(r io.Reader, l *rate.Limiter) io.Reader {
	if l == nil {
		return r
	}
	return rateLimitedReader{
		l: l,
		r: r,
	}
}

type rateLimitedReader struct {
	l *rate.Limiter
	r io.Reader
}

func (me rateLimitedReader) Read(b []byte) (n int, err error) {
	if me.l.Limit() == rate.Inf {
		return me.r.Read(b)
	}
	if me.l.Burst() != 0 {
		b = b[:min(len(b), me.l.Burst())]
	}
	n, err = me.r.Read(b)
	r := me.l.ReserveN(time.Now(), n)
	panicif.False(r.OK())
	time.Sleep(r.Delay())
	return
}
