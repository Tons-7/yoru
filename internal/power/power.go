// Package power keeps the computer from sleeping while Yoru is serving or downloading. It asks the way media
// servers do: the screen may still turn off, and closing the lid or pressing the power button still sleeps.
package power

import (
	"log/slog"
	"sync"
)

// Keeper holds one request to stay awake, taken and let go as Yoru's work comes and goes.
type Keeper struct {
	mu      sync.Mutex
	release func()
	why     string
	failed  bool
	log     *slog.Logger
	// hold is the system's call; replaced in tests.
	hold func(why string) (func(), error)
}

func New(log *slog.Logger) *Keeper { return &Keeper{log: log, hold: hold} }

// Set takes the request with why, or lets it go when why is empty. Asking again while held only renames it.
func (k *Keeper) Set(why string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	switch {
	case why != "" && k.release != nil:
		k.why = why
		return
	case why == "" && k.release == nil:
		k.failed = false
		return
	case why == "":
		k.release()
		k.release, k.why = nil, ""
		k.log.Info("this computer may sleep again")
		return
	case k.failed:
		return
	}
	release, err := k.hold(why)
	if err != nil {
		// Retried in the next busy spell: a missing tool is logged one time, not every check.
		k.failed = true
		k.log.Warn("keep this computer awake", "err", err)
		return
	}
	k.release, k.why = release, why
	k.log.Info("keeping this computer awake", "while", why)
}

// Awake reports whether the request is held, and why.
func (k *Keeper) Awake() (bool, string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.release != nil, k.why
}
