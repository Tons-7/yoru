package bt

import (
	"log/slog"
	"time"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/missinggo/v2/panicif"
)

type torrentUnlockActions struct {
	updateRegularTrackerAnnouncing bool
	updateComplete                 bool
}

type clientUnlockHandlers struct {
	torrentActions     map[*Torrent]torrentUnlockActions
	changedPieceStates map[*Piece]struct{}
}

func (me *clientUnlockHandlers) init() {
	g.MakeMap(&me.torrentActions)
	g.MakeMap(&me.changedPieceStates)
}

func (me *clientUnlockHandlers) deferUpdateTorrentRegularTrackerAnnouncing(t *Torrent) {
	g.MakeMapIfNil(&me.torrentActions)
	value := me.torrentActions[t]
	value.updateRegularTrackerAnnouncing = true
	me.torrentActions[t] = value
}

func (me *clientUnlockHandlers) addUpdateComplete(t *Torrent) {
	v := me.torrentActions[t]
	v.updateComplete = true
	me.torrentActions[t] = v
}

func (me *clientUnlockHandlers) run(logger *slog.Logger) {
	trackers := 0
	started := time.Now()
	for len(me.torrentActions) > 0 {
		for t, v := range me.torrentActions {
			delete(me.torrentActions, t)
			if v.updateRegularTrackerAnnouncing {
				trackers++
				t.updateRegularTrackerAnnouncing()
			}
			if v.updateComplete {
				t.updateComplete()
			}
		}
	}
	since := time.Since(started)
	if since > 20*time.Millisecond {
		logger.Warn("client unlock handlers took a long time", "duration", since, "trackers", trackers)
	}
	for p := range me.changedPieceStates {
		p.publishStateChange()
		delete(me.changedPieceStates, p)
	}
	panicif.NotEq(len(me.torrentActions), 0)
}
