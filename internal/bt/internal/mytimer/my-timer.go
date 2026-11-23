package mytimer

import (
	"math"
	"sync"
	"time"

	"github.com/anacrolix/missinggo/v2/panicif"
)

type Timer struct {
	mu           sync.RWMutex
	when         time.Time
	f            Func
	t            *time.Timer
	inited       bool
	stopped      bool
	missedUpdate bool
}

func (me *Timer) Stop() {
	me.mu.Lock()
	defer me.mu.Unlock()
	me.stopped = true
	if me.t != nil {
		me.t.Stop()
	}
}

func (me *Timer) Init(first TimeValue, f Func) {
	panicif.True(me.inited)
	me.inited = true
	me.f = f
	me.when = first
	d := time.Until(first)
	if first.IsZero() {
		d = math.MaxInt64
	}
	me.t = time.AfterFunc(d, me.innerCallback)
}

func (me *Timer) update(when time.Time) {
	me.mu.Lock()
	defer me.mu.Unlock()
	if me.stopped {
		return
	}
	if when.Equal(me.when) {
		return
	}
	if !me.t.Stop() {
		if !me.when.IsZero() {
			me.missedUpdate = true
			return
		}
	}
	me.reset(when)
}

func (me *Timer) reset(when time.Time) {
	me.when = when
	if !me.when.IsZero() {
		panicif.True(me.t.Reset(time.Until(me.when)))
	}
}
func (me *Timer) When() time.Time {
	return me.when
}

func (me *Timer) innerCallback() {
	me.mu.RLock()
	stopped := me.stopped
	me.mu.RUnlock()
	if stopped {
		return
	}
	panicif.True(me.when.IsZero())
	panicif.True(time.Now().Before(me.when))
	when := me.f()
	me.mu.Lock()
	if !me.stopped {
		if me.missedUpdate {
			me.missedUpdate = false
			when = time.Now()
		}
		me.reset(when)
	}
	me.mu.Unlock()
}

func (me *Timer) Update(when time.Time) {
	me.update(when)
}
