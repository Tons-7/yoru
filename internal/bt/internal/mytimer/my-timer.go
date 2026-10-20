package mytimer

import (
	"math"
	"sync"
	"time"

	"github.com/anacrolix/missinggo/v2/panicif"
)

// A timer reset from its callback or outside, knowing when it's due; one callback runs at a time and
// returns the next delay.
type Timer struct {
	mu      sync.RWMutex
	when    time.Time
	f       Func
	t       *time.Timer
	inited  bool
	stopped bool
}

// Stop ends the timer for good; later updates and a callback already due do nothing.
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
	// Should we Stop the timer if there's no initial delay set? We can't update the timer
	// externally unless we know if the timer is scheduled.
	me.t = time.AfterFunc(d, me.innerCallback)
}

func (me *Timer) update(when time.Time) {
	me.mu.Lock()
	defer me.mu.Unlock()
	if me.stopped {
		return
	}
	// Avoid hammering the scheduler with changes. I think this will work, and is probably cheaper
	// than avoiding our object's lock.
	if when.Equal(me.when) {
		return
	}
	if !me.t.Stop() {
		// Timer callback might be active.
		if !me.when.IsZero() {
			// Timer callback is running.
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
	// Nobody else sets it while this runs (when is non-zero and the timer has fired).
	when := me.f()
	me.mu.Lock()
	if !me.stopped {
		me.reset(when)
	}
	me.mu.Unlock()
}

// Resets if the timer func hasn't fired.
func (me *Timer) Update(when time.Time) {
	me.update(when)
}
