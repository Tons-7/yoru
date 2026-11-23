package power

import (
	"log/slog"
	"sync"
)

type Keeper struct {
	mu      sync.Mutex
	release func()
	why     string
	failed  bool
	log     *slog.Logger
	hold    func(why string) (func(), error)
}

func New(log *slog.Logger) *Keeper { return &Keeper{log: log, hold: hold} }

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
		k.failed = true
		k.log.Warn("keep this computer awake", "err", err)
		return
	}
	k.release, k.why = release, why
	k.log.Info("keeping this computer awake", "while", why)
}

func (k *Keeper) Awake() (bool, string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.release != nil, k.why
}
