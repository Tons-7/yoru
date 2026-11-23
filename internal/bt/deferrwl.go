package bt

import (
	"fmt"
	"sync"

	"github.com/anacrolix/missinggo/v2/panicif"
)

type lockWithDeferreds struct {
	internal      sync.RWMutex
	unlockActions []func()
	allowDefers   bool
	client        *Client
}

func (me *lockWithDeferreds) Lock() {
	me.internal.Lock()
	panicif.True(me.allowDefers)
	me.allowDefers = true
}

func (me *lockWithDeferreds) Unlock() {
	defer me.internal.Unlock()
	panicif.False(me.allowDefers)
	me.allowDefers = false
	me.client.unlockHandlers.run(me.client.slogger)
	startLen := len(me.unlockActions)
	var i int
	for i = 0; i < len(me.unlockActions); i++ {
		me.unlockActions[i]()
	}
	if i != len(me.unlockActions) {
		panic(fmt.Sprintf("num deferred changed while running: %v -> %v", startLen, len(me.unlockActions)))
	}
	me.unlockActions = me.unlockActions[:0]
}

func (me *lockWithDeferreds) RLock() {
	me.internal.RLock()
}

func (me *lockWithDeferreds) RUnlock() {
	me.internal.RUnlock()
}

func (me *lockWithDeferreds) Defer(action func()) {
	me.deferInner(action)
}

func (me *lockWithDeferreds) deferInner(action func()) {
	panicif.False(me.allowDefers)
	me.unlockActions = append(me.unlockActions, action)
}
