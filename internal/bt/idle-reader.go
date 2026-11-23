package bt

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

func newIdleTimeoutReader(r io.Reader, timeout time.Duration, interrupt func()) io.Reader {
	tr := &idleTimeoutReader{
		r:         r,
		interrupt: interrupt,
		timeout:   timeout,
	}
	tr.timer = time.AfterFunc(timeout, tr.timerFunc)
	tr.timer.Stop()
	return tr
}

type idleTimeoutReader struct {
	r         io.Reader
	interrupt func()
	timer     *time.Timer
	timeout   time.Duration
	fired     atomic.Bool
}

func (me *idleTimeoutReader) Read(p []byte) (n int, err error) {
	me.fired.Store(false)
	me.timer.Reset(me.timeout)
	n, err = me.r.Read(p)
	me.timer.Stop()
	if err != nil && me.fired.Load() {
		err = fmt.Errorf("idle timeout, original error: %w", err)
	}
	return
}

func (me *idleTimeoutReader) timerFunc() {
	me.fired.Store(true)
	me.interrupt()
}
