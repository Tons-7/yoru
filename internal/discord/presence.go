package discord

import (
	"log/slog"
	"math"
	"sync"
	"time"
)

// Watching is what's on screen, for the card.
type Watching struct {
	Title string
	// Episode is the line under the title: "Episode 5 of 28".
	Episode string
	// Cover is the show's image (https), Link its AniList page; either may be empty.
	Cover, Link        string
	Position, Duration float64
	Paused             bool
}

// Discord allows 5 activity updates in 20 s; one per sendGap stays inside that whatever the player does.
const (
	sendGap   = 4 * time.Second
	retryGap  = 30 * time.Second
	idleClear = 45 * time.Second
	// A start time that moved less than this (a report's own jitter) isn't worth an update.
	drift = 10 * time.Second
)

// Presence keeps the Discord card in step with what plays, sending only changes; it clears once reports stop.
// One goroutine talks to Discord, so a stuck or missing app never holds a caller up.
type Presence struct {
	log *slog.Logger
	// dial and now are swapped by tests.
	dial func() (activitySetter, error)
	now  func() time.Time
	// kick wakes the sender; barrier lets a test wait for it.
	kick    chan struct{}
	barrier chan chan struct{}

	mu      sync.Mutex
	on      bool
	tried   time.Time
	sent    time.Time
	shown   *card
	want    *card
	pending *time.Timer
	idle    *time.Timer

	// conn is the sender's alone.
	conn activitySetter
}

type activitySetter interface {
	setActivity(any) error
	Close() error
}

// card is what Discord shows; start is when the episode would have begun, for its elapsed and left counters.
type card struct {
	w     Watching
	start time.Time
}

func New(log *slog.Logger) *Presence {
	p := &Presence{
		log:     log,
		dial:    func() (activitySetter, error) { return dial() },
		now:     time.Now,
		kick:    make(chan struct{}, 1),
		barrier: make(chan chan struct{}),
	}
	go p.run()
	return p
}

// SetEnabled turns the card on or off, from Settings; off lets go of the Discord app, which takes the card down.
func (p *Presence) SetEnabled(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.on = on
	if !on {
		p.stopTimers()
		p.want, p.shown = nil, nil
	}
	p.wake()
}

// Show puts w on the card, or keeps it there; reports that change nothing visible send nothing.
func (p *Presence) Show(w Watching) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.on {
		return
	}
	c := &card{w: w, start: p.now().Add(-time.Duration(w.Position * float64(time.Second)))}
	p.resetIdle()
	if !p.same(c) {
		p.want = c
	}
	// Even unchanged: a card Discord never got (it wasn't running) goes once it is.
	p.wake()
}

// Clear takes the card down: the episode ended, or the player closed.
func (p *Presence) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.idle != nil {
		p.idle.Stop()
		p.idle = nil
	}
	p.want = nil
	p.wake()
}

func (p *Presence) stopTimers() {
	for _, t := range []**time.Timer{&p.idle, &p.pending} {
		if *t != nil {
			(*t).Stop()
			*t = nil
		}
	}
}

func (p *Presence) wake() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *Presence) run() {
	for {
		select {
		case <-p.kick:
			p.step()
		case done := <-p.barrier:
			p.step()
			close(done)
		}
	}
}

// step sends the wanted card if it isn't up, as the rate limit allows; turned off, it lets go of Discord.
func (p *Presence) step() {
	p.mu.Lock()
	if !p.on {
		p.mu.Unlock()
		if p.conn != nil {
			p.conn.Close()
			p.conn = nil
		}
		return
	}
	if p.want == p.shown {
		p.mu.Unlock()
		return
	}
	now := p.now()
	if wait := p.sent.Add(sendGap).Sub(now); wait > 0 {
		if p.pending == nil {
			p.pending = time.AfterFunc(wait, func() {
				p.mu.Lock()
				p.pending = nil
				p.mu.Unlock()
				p.wake()
			})
		}
		p.mu.Unlock()
		return
	}
	if p.conn == nil {
		if !p.tried.IsZero() && now.Sub(p.tried) < retryGap {
			p.mu.Unlock()
			return
		}
		p.tried = now
	}
	want := p.want
	p.mu.Unlock()

	if p.conn == nil {
		c, err := p.dial()
		if err != nil {
			p.log.Debug("discord presence", "err", err)
			return
		}
		p.conn = c
	}
	var activity any
	if want != nil {
		activity = want.activity()
	}
	if err := p.conn.setActivity(activity); err != nil {
		// Discord quit or restarted: try again with a later report.
		p.log.Debug("discord presence", "err", err)
		p.conn.Close()
		p.conn = nil
		return
	}
	p.mu.Lock()
	// Turned off meanwhile: the card goes with the connection, at the next step.
	if p.on {
		p.sent, p.shown = now, want
	}
	p.mu.Unlock()
}

// same reports a card already showing (or about to) what c would.
func (p *Presence) same(c *card) bool {
	o := p.want
	if o == nil {
		o = p.shown
	}
	if o == nil || o.w.Title != c.w.Title || o.w.Episode != c.w.Episode || o.w.Paused != c.w.Paused {
		return false
	}
	// A paused card has no counter to keep right.
	return c.w.Paused || math.Abs(o.start.Sub(c.start).Seconds()) < drift.Seconds()
}

// resetIdle clears the card once reports stop, as when a tab closes without saying so.
func (p *Presence) resetIdle() {
	if p.idle != nil {
		p.idle.Stop()
	}
	p.idle = time.AfterFunc(idleClear, p.Clear)
}

// activity is the card as Discord takes it: "Watching <title>", the episode, elapsed and left, the cover.
func (c *card) activity() map[string]any {
	w := c.w
	state := w.Episode
	if w.Paused {
		state = "Paused · " + w.Episode
	}
	a := map[string]any{
		"type": 3, // Watching
		// The member list says the show, not the app.
		"status_display_type": 2,
		"details":             fit(w.Title),
		"state":               fit(state),
		"assets": map[string]any{
			"large_image": w.Cover, "large_text": fit(w.Title),
			"small_image": "yoru", "small_text": "Yoru",
		},
	}
	if w.Cover == "" {
		a["assets"] = map[string]any{"large_image": "yoru", "large_text": "Yoru"}
	}
	if !w.Paused && w.Duration > 0 {
		a["timestamps"] = map[string]any{
			"start": c.start.UnixMilli(),
			"end":   c.start.Add(time.Duration(w.Duration * float64(time.Second))).UnixMilli(),
		}
	}
	if w.Link != "" {
		a["buttons"] = []map[string]any{{"label": "View on AniList", "url": w.Link}}
	}
	return a
}

// fit keeps a line within what Discord takes: 2 to 128 characters.
func fit(s string) string {
	r := []rune(s)
	if len(r) > 128 {
		return string(r[:127]) + "…"
	}
	if len(r) < 2 {
		return s + "  "[:2-len(r)]
	}
	return s
}
