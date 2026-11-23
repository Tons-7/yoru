package bt

import (
	"fmt"
	"log/slog"
	"sync/atomic"

	"yoru/internal/bt/webseed"
)

type webseedRequest struct {
	request   webseed.Request
	logger    *slog.Logger
	begin     RequestIndex
	next      RequestIndex
	end       RequestIndex
	cancelled atomic.Bool
}

func (me *webseedRequest) Close() {
	me.request.Close()
}

func (me *webseedRequest) Cancel(cause string, t *Torrent) bool {
	me.request.Cancel(stringError(cause))
	if !me.cancelled.Swap(true) {
		if webseed.PrintDebug {
			me.logger.Debug("webseed request cancelled", "cause", cause)
		}
		t.deferUpdateRegularTrackerAnnouncing()
		return true
	}
	return false
}

func (me *webseedRequest) String() string {
	s := fmt.Sprintf("%v of [%v-%v)", me.next, me.begin, me.end)
	if me.cancelled.Load() {
		s += " (cancelled)"
	}
	return s
}
