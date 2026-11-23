package bt

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"
	"weak"

	g "github.com/anacrolix/generics"
	analog "github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2/panicif"
	"yoru/internal/bt/internal/extracmp"
	"yoru/internal/bt/internal/indexed"
	"yoru/internal/bt/internal/mytimer"
	"yoru/internal/bt/tracker"
	trHttp "yoru/internal/bt/tracker/http"
)

type regularTrackerAnnounceDispatcher struct {
	torrentClient *Client
	logger        *slog.Logger

	trackerClients             map[trackerAnnouncerKey]*trackerClientsValue
	announceStates             map[torrentTrackerAnnouncerKey]*announceState
	torrentForAnnounceRequests map[shortInfohash]weak.Pointer[Torrent]

	announceData  indexed.Map[torrentTrackerAnnouncerKey, nextAnnounceInput]
	announceIndex indexed.Index[nextAnnounceRecord]
	overdueIndex  indexed.Index[nextAnnounceRecord]

	trackerAnnounceHead indexed.Table[trackerAnnounceHeadRecord]
	nextAnnounce        indexed.Index[trackerAnnounceHeadRecord]

	infohashAnnouncing indexed.Map[shortInfohash, infohashConcurrency]
	trackerAnnouncing  indexed.Map[trackerAnnouncerKey, int]
	timer              mytimer.Timer
}

type trackerAnnounceHeadRecord struct {
	trackerRequests int
	nextAnnounceRecord
}

type trackerClientsValue struct {
	client tracker.Client
}

func nextAnnounceMinRecord() (ret nextAnnounceRecord) {
	ret.nextAnnounceInput = nextAnnounceInputMin()
	return
}

func nextAnnounceInputMin() (ret nextAnnounceInput) {
	ret.overdue = true
	ret.torrent.Ok = true
	ret.torrent.Value.NeedData = true
	ret.torrent.Value.WantPeers = true
	ret.AnnounceEvent = tracker.Started
	return
}

func (me *regularTrackerAnnounceDispatcher) init(client *Client) {
	me.torrentClient = client
	me.logger = client.slogger
	me.announceData.Init(torrentTrackerAnnouncerKey.Compare)
	me.announceData.SetMinRecord(torrentTrackerAnnouncerKey{})
	me.announceData.OnChange(func(old, new g.Option[indexed.Pair[torrentTrackerAnnouncerKey, nextAnnounceInput]]) {
		if !new.Ok {
			return
		}
		actual := new.Value.Right.infohashActive
		key := new.Value.Left
		expected := g.OptionFromTuple(me.infohashAnnouncing.Get(key.ShortInfohash)).Value.count
		if actual != expected {
			me.logger.Debug(
				"announceData.infohashActive != infohashAnnouncing.count",
				"key", key,
				"actual", actual,
				"expected", expected)
		}
	})
	me.announceIndex = indexed.NewFullMappedIndex(
		&me.announceData,
		announceIndexCompare,
		nextAnnounceRecordFromPair,
		nextAnnounceMinRecord(),
	)
	me.overdueIndex = indexed.NewFullMappedIndex(
		&me.announceData,
		overdueIndexCompare,
		nextAnnounceRecordFromPair,
		func() (ret nextAnnounceRecord) {
			ret.overdue = true
			return
		}(),
	)
	me.trackerAnnounceHead.Init(func(a, b trackerAnnounceHeadRecord) int {
		return cmp.Compare(a.url, b.url)
	})
	me.trackerAnnounceHead.SetMinRecord(trackerAnnounceHeadRecord{})
	me.nextAnnounce = indexed.NewFullIndex(
		&me.trackerAnnounceHead,
		func(a, b trackerAnnounceHeadRecord) int {
			return cmp.Or(
				-extracmp.CompareBool(a.overdue, b.overdue),
				cmp.Compare(a.trackerRequests, b.trackerRequests),
				compareNextAnnounce(a.nextAnnounceInput, b.nextAnnounceInput),
				a.torrentTrackerAnnouncerKey.Compare(b.torrentTrackerAnnouncerKey),
			)
		},
		func() (ret trackerAnnounceHeadRecord) {
			ret.nextAnnounceInput = nextAnnounceInputMin()
			return
		}(),
	)
	me.announceData.OnChange(func(old, new g.Option[indexed.Pair[torrentTrackerAnnouncerKey, nextAnnounceInput]]) {
		if old.Ok {
			me.updateTrackerAnnounceHead(old.Value.Left.url)
		}
		if new.Ok {
			me.updateTrackerAnnounceHead(new.Value.Left.url)
		}
	})
	me.infohashAnnouncing.Init(shortInfohash.Compare)
	me.infohashAnnouncing.OnValueChange(func(shortIh shortInfohash, old, new g.Option[infohashConcurrency]) {
		start := me.announceData.MinRecord()
		start.Left.ShortInfohash = shortIh
		keys := make([]torrentTrackerAnnouncerKey, 0, len(me.trackerClients))
		var expectedCount g.Option[int]
		for r := range indexed.IterClusteredWhere(
			me.announceData,
			start,
			func(p indexed.Pair[torrentTrackerAnnouncerKey, nextAnnounceInput]) bool {
				return p.Left.ShortInfohash == shortIh
			},
		) {
			if expectedCount.Ok {
				panicif.NotEq(r.Right.infohashActive, expectedCount.Value)
			} else {
				expectedCount.Set(r.Right.infohashActive)
			}
			if r.Right.infohashActive != new.Value.count {
				keys = append(keys, r.Left)
			}
		}
		for _, key := range keys {
			panicif.False(me.announceData.Update(
				key,
				func(input nextAnnounceInput) nextAnnounceInput {
					input.infohashActive = new.Value.count
					return input
				},
			).Exists)
		}
	})
	me.trackerAnnouncing.Init(cmp.Compare)
	me.trackerAnnouncing.OnValueChange(func(key trackerAnnouncerKey, old, new g.Option[int]) {
		panicif.GreaterThan(new.Value, maxConcurrentAnnouncesPerTracker)
		me.updateTrackerAnnounceHead(key)
		if new.Value == 0 {
			me.trackerAnnouncing.Delete(key)
		}
	})
	me.timer.Init(time.Now(), me.timerFunc)
}

func (me *regularTrackerAnnounceDispatcher) close() {
	me.timer.Stop()
	for _, v := range me.trackerClients {
		v.client.Close()
	}
	clear(me.trackerClients)
}

func (me *regularTrackerAnnounceDispatcher) updateTrackerAnnounceHead(url trackerAnnouncerKey) {
	new := me.getTrackerNextAnnounce(url)
	if new.Ok {
		tr := g.OptionFromTuple(me.trackerAnnouncing.Get(url)).Value
		me.trackerAnnounceHead.CreateOrReplace(trackerAnnounceHeadRecord{
			trackerRequests:    tr,
			nextAnnounceRecord: new.Unwrap(),
		})
	} else {
		key := me.trackerAnnounceHead.MinRecord()
		key.url = url
		me.trackerAnnounceHead.Delete(key)
	}
	panicif.NotEq(me.trackerAnnounceHead.Len(), me.nextAnnounce.Len())
	panicif.GreaterThan(me.trackerAnnounceHead.Len(), len(me.trackerClients))
	me.updateTimer()
}

func nextAnnounceRecordFromParts(key torrentTrackerAnnouncerKey, input nextAnnounceInput) nextAnnounceRecord {
	return nextAnnounceRecord{
		torrentTrackerAnnouncerKey: key,
		nextAnnounceInput:          input,
	}
}

func nextAnnounceRecordFromPair(from indexed.Pair[torrentTrackerAnnouncerKey, nextAnnounceInput]) nextAnnounceRecord {
	return nextAnnounceRecordFromParts(from.Left, from.Right)
}

func announceIndexCompare(a, b nextAnnounceRecord) int {
	return cmp.Or(
		cmp.Compare(a.url, b.url),
		compareNextAnnounce(a.nextAnnounceInput, b.nextAnnounceInput),
		a.ShortInfohash.Compare(b.ShortInfohash),
	)
}

type infohashConcurrency struct {
	count int
}

func (me *regularTrackerAnnounceDispatcher) getTrackerNextAnnounce(key trackerAnnouncerKey) (_ g.Option[nextAnnounceRecord]) {
	panicif.NotEq(me.announceIndex.Len(), me.announceData.Len())
	gte := me.announceIndex.MinRecord()
	gte.url = key
	return indexed.IterClusteredWhere(me.announceIndex, gte, func(r nextAnnounceRecord) bool {
		return r.url == key
	}).First()
}

var nextAnnounceRecordCols = []any{
	"Tracker",
	"ShortInfohash",
	"active",
	"Overdue",
	"UntilWhen",
	"|ih|",
	"WantPeers",
	"NeedData",
	"Progress",
	"Webseeds",
	"Event",
	"status line",
}

func (me *regularTrackerAnnounceDispatcher) printNextAnnounceRecordTable(
	sw statusWriter,
	table indexed.Index[nextAnnounceRecord],
) {
	tab := sw.tab()
	tab.cols(nextAnnounceRecordCols...)
	tab.row()
	for r := range table.Iter {
		me.putNextAnnounceRecordCols(tab, r)
		tab.row()
	}
	tab.end()
}

func (me *regularTrackerAnnounceDispatcher) printNextAnnounceTable(
	sw statusWriter,
	table indexed.Index[trackerAnnounceHeadRecord],
) {
	tab := sw.tab()
	tab.cols("#tr")
	tab.cols(nextAnnounceRecordCols...)
	tab.row()
	for r := range table.Iter {
		tab.cols(r.trackerRequests)
		me.putNextAnnounceRecordCols(tab, r.nextAnnounceRecord)
		tab.row()
	}
	tab.end()
}

func (me *regularTrackerAnnounceDispatcher) putNextAnnounceRecordCols(
	tab *tableWriter,
	r nextAnnounceRecord,
) {
	t := me.torrentFromShortInfohash(r.ShortInfohash)
	progress := "dropped"
	if t != nil {
		progress = fmt.Sprintf("%d%%", int(100*t.progressUnitFloat()))
	}
	tab.cols(
		r.url,
		r.ShortInfohash,
		r.active,
		r.overdue,
		time.Until(r.When),
		r.infohashActive,
		r.torrent.Value.WantPeers,
		r.torrent.Value.NeedData,
		progress,
		r.torrent.Value.HasActiveWebseedRequests,
		r.AnnounceEvent,
		regularTrackerScraperStatusLine(*me.announceStates[r.torrentTrackerAnnouncerKey]),
	)
}

func (me *regularTrackerAnnounceDispatcher) writeStatus(w io.Writer) {
	sw := statusWriter{w: w}
	sw.f("timer next: %v\n", time.Until(me.timer.When()))
	sw.f("Next announces:\n")
	for sw := range indented(sw) {
		me.printNextAnnounceRecordTable(sw, me.announceIndex)
	}
	fmt.Fprintln(sw, "Next announces")
	for sw := range sw.indented() {
		me.printNextAnnounceTable(sw, me.nextAnnounce)
	}
}

func (me *regularTrackerAnnounceDispatcher) updateOverdue() {
	now := time.Now()
	start := me.overdueIndex.MinRecord()
	start.When = now.Add(1)
	end := me.overdueIndex.MinRecord()
	end.overdue = false
	end.When = now.Add(1)

	var updateKeys []torrentTrackerAnnouncerKey
	for r := range indexed.IterRange(me.overdueIndex, start, end) {
		updateKeys = append(updateKeys, r.torrentTrackerAnnouncerKey)
	}
	for _, key := range updateKeys {
		panicif.False(me.announceData.Update(
			key,
			func(value nextAnnounceInput) nextAnnounceInput {
				value.overdue = !value.When.IsZero() && value.When.Compare(time.Now()) <= 0
				return value
			},
		).Exists)
	}
}

func (me *regularTrackerAnnounceDispatcher) timerFunc() mytimer.TimeValue {
	me.torrentClient.lock()
	ret := me.step()
	me.torrentClient.unlock()
	return ret
}

func (me *regularTrackerAnnounceDispatcher) step() mytimer.TimeValue {
	me.dispatchAnnounces()
	return me.nextTimerDelay()
}

func (me *regularTrackerAnnounceDispatcher) addKey(key torrentTrackerAnnouncerKey) {
	t := me.torrentFromShortInfohash(key.ShortInfohash)
	if t == nil {
		return
	}
	g.MakeMapIfNil(&me.torrentForAnnounceRequests)
	if me.announceData.ContainsKey(key) {
		if w, ok := me.torrentForAnnounceRequests[key.ShortInfohash]; ok && w.Value() == t {
			return
		}
		me.torrentForAnnounceRequests[key.ShortInfohash] = weak.Make(t)
		state := new(announceState)
		me.announceStates[key] = state
		t.regularTrackerAnnounceState[key] = state
		me.announceData.Update(key, func(v nextAnnounceInput) nextAnnounceInput {
			v.torrent = me.makeTorrentInput(t)
			v.nextAnnounceStateInput = me.makeAnnounceStateInput(key)
			return v
		})
		me.updateTimer()
		return
	}
	me.torrentForAnnounceRequests[key.ShortInfohash] = weak.Make(t)
	if !g.MapContains(me.announceStates, key) {
		g.MakeMapIfNil(&me.announceStates)
		g.MapMustAssignNew(me.announceStates, key, new(announceState{}))
	}
	t.regularTrackerAnnounceState[key] = g.MapMustGet(me.announceStates, key)
	me.announceData.Create(key, nextAnnounceInput{
		torrent:                me.makeTorrentInput(t),
		nextAnnounceStateInput: me.makeAnnounceStateInput(key),
		infohashActive:         g.OptionFromTuple(me.infohashAnnouncing.Get(key.ShortInfohash)).Value.count,
	})
	me.updateTimer()
}

func (me *regularTrackerAnnounceDispatcher) torrentFromShortInfohash(short shortInfohash) *Torrent {
	return me.torrentClient.torrentsByShortHash[short]
}

const (
	maxConcurrentAnnouncesPerTracker = 2
	maxTrackerReplyPeers             = 500
	minAnnounceInterval              = time.Minute
)

func (me *regularTrackerAnnounceDispatcher) dispatchAnnounces() {
	for {
		next := me.getNextAnnounce()
		if !next.Ok {
			break
		}
		t := me.torrentFromShortInfohash(next.Value.ShortInfohash)
		{
			actual := next.Value.torrent
			expected := me.makeTorrentInput(t)
			if actual != expected {
				me.logger.Debug("announce dispatcher torrent input is not synced",
					"expected", fmt.Sprintf("%#v", expected),
					"actual", fmt.Sprintf("%#v", actual))
			}
		}
		if !next.Value.overdue {
			break
		}
		panicif.True(next.Value.When.After(time.Now()))
		panicif.True(next.Value.active)
		me.startAnnounce(next.Value.torrentTrackerAnnouncerKey)
	}
}

func (me *regularTrackerAnnounceDispatcher) startAnnounce(key torrentTrackerAnnouncerKey) {
	next, ok := me.announceData.Get(key)
	panicif.False(ok)
	panicif.False(me.announceData.Update(key, func(r nextAnnounceInput) nextAnnounceInput {
		panicif.True(r.active)
		r.active = true
		return r
	}).Exists)
	me.alterInfohashConcurrency(key.ShortInfohash, func(existing int) int {
		return existing + 1
	})
	me.trackerAnnouncing.UpdateOrCreate(key.url, func(i int) int {
		return i + 1
	})
	me.updateTrackerAnnounceHead(key.url)
	go me.singleAnnounceAttempter(key, next.AnnounceEvent)
}

func (me *regularTrackerAnnounceDispatcher) alterInfohashConcurrency(ih shortInfohash, update func(existing int) int) {
	me.infohashAnnouncing.Alter(
		ih,
		func(ic infohashConcurrency, b bool) (infohashConcurrency, bool) {
			ic.count = update(ic.count)
			panicif.LessThan(ic.count, 0)
			return ic, ic.count > 0
		})
}

func (me *regularTrackerAnnounceDispatcher) finishedAnnounce(key torrentTrackerAnnouncerKey) {
	me.alterInfohashConcurrency(key.ShortInfohash, func(existing int) int { return existing - 1 })
	me.announceData.Update(key, func(r nextAnnounceInput) nextAnnounceInput {
		panicif.False(r.active)
		r.active = false
		r.torrent = me.makeTorrentInput(me.torrentFromShortInfohash(key.ShortInfohash))
		return r
	})
	me.trackerAnnouncing.Update(key.url, func(i int) int {
		return i - 1
	})
	me.forgetIfDone(key)
	me.updateTimer()
}

func (me *regularTrackerAnnounceDispatcher) forgetIfDone(key torrentTrackerAnnouncerKey) {
	if me.torrentFromShortInfohash(key.ShortInfohash) != nil {
		return
	}
	r, ok := me.announceData.Get(key)
	if !ok || r.active {
		return
	}
	if _, when := me.nextAnnounceEvent(key); !when.IsZero() {
		return
	}
	me.announceData.Delete(key)
	delete(me.announceStates, key)
	for other := range me.announceStates {
		if other.ShortInfohash == key.ShortInfohash {
			return
		}
	}
	delete(me.torrentForAnnounceRequests, key.ShortInfohash)
}

func (me *regularTrackerAnnounceDispatcher) syncAnnounceState(key torrentTrackerAnnouncerKey) {
	input := me.makeAnnounceStateInput(key)
	me.announceData.UpdateOrCreate(key, func(old nextAnnounceInput) nextAnnounceInput {
		old.nextAnnounceStateInput = input
		return old
	})
}

func (me *regularTrackerAnnounceDispatcher) updateTorrentInput(t *Torrent) {
	input := me.makeTorrentInput(t)
	changed := false
	for key, state := range t.regularTrackerAnnounceState {
		panicif.Zero(key.url)
		panicif.Zero(key.ShortInfohash)
		if me.announceStates[key] != state {
			delete(t.regularTrackerAnnounceState, key)
			continue
		}
		res := me.announceData.Update(
			key,
			func(av nextAnnounceInput) nextAnnounceInput {
				av.torrent = input
				av.nextAnnounceStateInput = me.makeAnnounceStateInput(key)
				return av
			},
		)
		panicif.False(res.Exists)
		changed = changed || res.Changed
	}
	if t.isDropped() {
		for key := range t.regularTrackerAnnounceState {
			me.forgetIfDone(key)
		}
		changed = true
	}
	if changed {
		me.updateTimer()
	}
}

func (me *regularTrackerAnnounceDispatcher) nextTimerDelay() mytimer.TimeValue {
	next := me.getNextAnnounce()
	return next.Value.When
}

func (me *regularTrackerAnnounceDispatcher) updateTimer() {
	me.timer.Update(me.nextTimerDelay())
}

func (me *regularTrackerAnnounceDispatcher) singleAnnounceAttempter(key torrentTrackerAnnouncerKey, event tracker.AnnounceEvent) {
	me.torrentClient.lock()
	defer me.torrentClient.unlock()
	defer me.finishedAnnounce(key)
	ih := key.ShortInfohash
	logger := me.logger.With(
		"short infohash", ih,
		"url", key.url,
	)
	t := me.getTorrentForAnnounceRequest(key.ShortInfohash)
	if t == nil {
		logger.Debug("skipping announce for GCed torrent")
		me.updateAnnounceState(key, func(state *announceState) {
			state.Err = errors.New("announce skipped: Torrent GCed")
			state.lastAttemptCompleted = time.Now()
		})
	} else {
		me.singleAnnounce(key, event, logger, t)
	}
}

func (me *regularTrackerAnnounceDispatcher) singleAnnounce(
	key torrentTrackerAnnouncerKey,
	event tracker.AnnounceEvent,
	logger *slog.Logger,
	t *Torrent,
) {
	logger = logger.With(t.slogGroup())
	req := t.announceRequest(event, key.ShortInfohash)
	v, ok := me.trackerClients[key.url]
	if !ok {
		return
	}
	tc := v.client
	opts := me.getAnnounceOpts()
	stateAtStart := me.announceStates[key]
	me.torrentClient.unlock()
	ctx, cancel := context.WithTimeout(context.TODO(), tracker.DefaultTrackerAnnounceTimeout)
	defer cancel()
	logger.Debug("announcing", "req", req)
	resp, err := tc.Announce(ctx, req, opts)
	now := time.Now()
	if len(resp.Peers) > maxTrackerReplyPeers {
		resp.Peers = resp.Peers[:maxTrackerReplyPeers]
	}
	{
		level := slog.LevelDebug
		if err != nil {
			level = analog.SlogErrorLevel(err).UnwrapOr(level)
		}
		logger.Log(context.Background(), level, "announced", "resp", resp, "err", err)
	}

	me.torrentClient.lock()
	if me.announceStates[key] != stateAtStart {
		return
	}
	me.updateAnnounceState(key, func(state *announceState) {
		state.Err = err
		state.lastAttemptCompleted = now
		if err == nil {
			state.lastOk = lastAnnounceOk{
				AnnouncedEvent: req.Event,
				Interval:       max(time.Duration(resp.Interval)*time.Second, minAnnounceInterval),
				NumPeers:       len(resp.Peers),
				Completed:      now,
			}
			if req.Event == tracker.Completed {
				state.sentCompleted = true
			}
		}
	})
	t.addPeers(peerInfos(nil).AppendFromTracker(resp.Peers))
}

func (me *regularTrackerAnnounceDispatcher) updateAnnounceState(
	key torrentTrackerAnnouncerKey,
	update func(state *announceState),
) {
	as := me.announceStates[key]
	update(as)
	me.syncAnnounceState(key)
}

func (me *regularTrackerAnnounceDispatcher) getAnnounceOpts() trHttp.AnnounceOpt {
	cfg := me.torrentClient.config
	return trHttp.AnnounceOpt{
		UserAgent: cfg.HTTPUserAgent,
	}
}

func (me *regularTrackerAnnounceDispatcher) getNextAnnounce() (_ g.Option[nextAnnounceRecord]) {
	me.updateOverdue()
	v, ok := me.nextAnnounce.GetFirst()
	ok = ok && !v.active && v.trackerRequests < maxConcurrentAnnouncesPerTracker
	return g.OptionFromTuple(v.nextAnnounceRecord, ok)
}

func (me *regularTrackerAnnounceDispatcher) makeAnnounceStateInput(key torrentTrackerAnnouncerKey) nextAnnounceStateInput {
	panicif.Zero(me.torrentClient)
	state := me.announceStates[key]
	event, when := me.nextAnnounceEvent(key)
	return nextAnnounceStateInput{
		AnnounceEvent:      event,
		When:               when,
		LastAnnounceFailed: state.Err != nil,
	}
}

func (me *regularTrackerAnnounceDispatcher) makeTorrentInput(t *Torrent) (_ g.Option[nextAnnounceTorrentInput]) {
	if t == nil || t.isDropped() {
		return
	}
	return g.Some(nextAnnounceTorrentInput{
		NeedData:                 t.needData(),
		WantPeers:                t.wantPeers(),
		HasActiveWebseedRequests: t.hasActiveWebseedRequests(),
	})
}

var eventOrdering = map[tracker.AnnounceEvent]int{
	tracker.Started:   -4,
	tracker.Stopped:   -3,
	tracker.Completed: -2,
	tracker.None:      -1,
}

func overdueIndexCompare(a, b nextAnnounceRecord) int {
	return cmp.Or(
		compareOverdue(a.nextAnnounceInput, b.nextAnnounceInput),
		a.torrentTrackerAnnouncerKey.Compare(b.torrentTrackerAnnouncerKey),
	)
}

func compareOverdue(a, b nextAnnounceInput) int {
	return cmp.Or(
		-extracmp.CompareBool(a.overdue, b.overdue),
		a.When.Compare(b.When),
	)
}

func compareNextAnnounce(ar, br nextAnnounceInput) (ret int) {

	ret = cmp.Or(
		extracmp.CompareBool(ar.active, br.active),
		-extracmp.CompareBool(ar.overdue, br.overdue),
	)
	if ret != 0 {
		return
	}
	panicif.NotEq(ar.overdue, br.overdue)
	overdue := ar.overdue
	whenCmp := ar.When.Compare(br.When)
	if !overdue {
		if az, bz := ar.When.IsZero(), br.When.IsZero(); az != bz {
			return extracmp.CompareBool(az, bz)
		}
		ret = whenCmp
		if ret != 0 {
			return
		}
	}
	return cmp.Or(
		cmp.Compare(ar.infohashActive, br.infohashActive),
		-extracmp.CompareBool(ar.torrent.Ok, br.torrent.Ok),
		-extracmp.CompareBool(ar.torrent.Value.WantPeers, br.torrent.Value.WantPeers),
		-extracmp.CompareBool(ar.torrent.Value.NeedData, br.torrent.Value.NeedData),
		extracmp.CompareBool(ar.torrent.Value.HasActiveWebseedRequests, br.torrent.Value.HasActiveWebseedRequests),
		cmp.Compare(eventOrdering[ar.AnnounceEvent], eventOrdering[br.AnnounceEvent]),
		whenCmp,
	)
}

type nextAnnounceRecord struct {
	torrentTrackerAnnouncerKey
	nextAnnounceInput
}

type nextAnnounceInput struct {
	torrent g.Option[nextAnnounceTorrentInput]
	nextAnnounceStateInput
	infohashActive int
	overdue        bool
	active         bool
}

type nextAnnounceStateInput struct {
	AnnounceEvent      tracker.AnnounceEvent
	When               time.Time
	LastAnnounceFailed bool
}

type nextAnnounceTorrentInput struct {
	NeedData                 bool
	WantPeers                bool
	HasActiveWebseedRequests bool
}

func (me *regularTrackerAnnounceDispatcher) nextAnnounceEvent(key torrentTrackerAnnouncerKey) (event tracker.AnnounceEvent, when time.Time) {
	state := g.MapMustGet(me.announceStates, key)
	lastOk := state.lastOk
	t := me.torrentFromShortInfohash(key.ShortInfohash)
	if t == nil {
		if state.Err != nil {
			return
		}
		if lastOk.Completed.IsZero() {
			return
		}
		if lastOk.AnnouncedEvent == tracker.Stopped {
			return
		}
		return tracker.Stopped, announceDueNow
	}
	defer func() {
		if state.Err == nil || when.IsZero() {
			return
		}
		minWhen := state.lastAttemptCompleted.Add(time.Minute)
		if when.Before(minWhen) {
			when = minWhen
		}
	}()
	if !state.sentCompleted && t.sawInitiallyIncompleteData && t.haveAllPieces() {
		return tracker.Completed, announceDueNow
	}
	if lastOk.Completed.IsZero() {
		return tracker.Started, announceDueNow
	}
	next := lastOk.Completed.Add(lastOk.Interval)
	if soon := lastOk.Completed.Add(starvingReannounceAfter); lastOk.NumPeers == 0 && soon.Before(next) && t.wantPeers() && t.needData() {
		next = soon
	}
	return tracker.None, next
}

const starvingReannounceAfter = 5 * time.Minute

var announceDueNow = time.Unix(1, 0)

type lastAnnounceOk struct {
	AnnouncedEvent tracker.AnnounceEvent
	Interval       time.Duration
	Completed      time.Time
	NumPeers       int
}

type announceState struct {
	lastOk               lastAnnounceOk
	Err                  error
	lastAttemptCompleted time.Time
	sentCompleted        bool
}

func (cl *Client) startTrackerAnnouncer(u *url.URL, urlStr trackerAnnouncerKey) error {
	return cl.regularTrackerAnnounceDispatcher.initTrackerClient(u, urlStr, cl.config, cl.logger)
}

func (me *regularTrackerAnnounceDispatcher) initTrackerClient(
	u *url.URL,
	urlStr trackerAnnouncerKey,
	config *ClientConfig,
	logger analog.Logger,
) error {
	panicif.NotEq(u.String(), string(urlStr))
	if g.MapContains(me.trackerClients, urlStr) {
		return nil
	}
	tc, err := tracker.NewClient(string(urlStr), tracker.NewClientOpts{
		Http: trHttp.NewClientOpts{
			DialContext: config.TrackerDialContext,
		},
		UdpNetwork:   u.Scheme,
		Logger:       logger.WithContextValue(fmt.Sprintf("tracker client for %q", urlStr)),
		ListenPacket: config.TrackerListenPacket,
		Resolver:     config.TrackerResolver,
	})
	if err != nil {
		return err
	}
	panicif.NotNil(u.User)
	value := trackerClientsValue{
		client: tc,
	}

	g.MakeMapIfNil(&me.trackerClients)
	g.MapMustAssignNew(me.trackerClients, urlStr, &value)
	return nil
}

func (me *regularTrackerAnnounceDispatcher) getTorrentForAnnounceRequest(ih shortInfohash) *Torrent {
	return g.MapMustGet(me.torrentForAnnounceRequests, ih).Value()
}
