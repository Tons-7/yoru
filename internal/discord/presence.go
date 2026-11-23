package discord

import (
	"log/slog"
	"math"
	"sync"
	"time"
)

type Watching struct {
	Title              string
	Episode            string
	Cover, Link        string
	Position, Duration float64
	Paused             bool
}

var idleClear = 45 * time.Second

const (
	sendGap     = 4 * time.Second
	retryGap    = 30 * time.Second
	pausedClear = 20 * time.Minute
	resendAfter = time.Minute
	drift       = 10 * time.Second
)

type Presence struct {
	log     *slog.Logger
	dial    func() (activitySetter, error)
	now     func() time.Time
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

	conn activitySetter
}

type activitySetter interface {
	setActivity(any) error
	Close() error
}

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

func (p *Presence) Show(w Watching) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.on {
		return
	}
	c := &card{w: w, start: p.now().Add(-time.Duration(w.Position * float64(time.Second)))}
	p.resetIdle(w.Paused)
	if !p.same(c) {
		p.want = c
	}
	p.wake()
}

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
	now := p.now()
	if p.want == p.shown && (p.want == nil || now.Sub(p.sent) < resendAfter) {
		p.mu.Unlock()
		return
	}
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
		p.log.Debug("discord presence", "err", err)
		p.conn.Close()
		p.conn = nil
		p.mu.Lock()
		p.shown = nil
		p.mu.Unlock()
		return
	}
	p.mu.Lock()
	if p.on {
		p.sent, p.shown = now, want
	}
	p.mu.Unlock()
}

func (p *Presence) same(c *card) bool {
	o := p.want
	if o == nil || o.w.Title != c.w.Title || o.w.Episode != c.w.Episode || o.w.Paused != c.w.Paused {
		return false
	}
	return c.w.Paused || math.Abs(o.start.Sub(c.start).Seconds()) < drift.Seconds()
}

func (p *Presence) resetIdle(paused bool) {
	if p.idle != nil {
		p.idle.Stop()
	}
	after := idleClear
	if paused {
		after = pausedClear
	}
	p.idle = time.AfterFunc(after, p.Clear)
}

func (c *card) activity() map[string]any {
	w := c.w
	state := w.Episode
	if w.Paused {
		state = "Paused · " + w.Episode
	}
	a := map[string]any{
		"type":                3,
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

func fit(s string) string {
	r := []rune(s)
	if len(r) > 128 {
		return string(r[:127]) + "…"
	}
	if len(r) < 2 {
		return s + "··"[:len("·")*(2-len(r))]
	}
	return s
}
