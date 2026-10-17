package discord

import (
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

// fakeApp stands in for the Discord app: it keeps every activity sent, nil for a clear.
type fakeApp struct {
	sent       []any
	fail, down bool
	closed     int
	// stuck, when set, holds every send until it's closed.
	stuck chan struct{}
}

func (f *fakeApp) setActivity(a any) error {
	if f.stuck != nil {
		<-f.stuck
	}
	if f.fail {
		return errors.New("pipe closed")
	}
	f.sent = append(f.sent, a)
	return nil
}

func (f *fakeApp) Close() error {
	f.closed++
	return nil
}

// settle waits until the sender has dealt with everything asked of it so far.
func settle(p *Presence) {
	done := make(chan struct{})
	p.barrier <- done
	<-done
}

// newTest is a presence on a clock the test moves; Discord counts as not running while app.down.
func newTest(t *testing.T) (*Presence, *fakeApp, *time.Time, *int) {
	t.Helper()
	app := &fakeApp{}
	clock := time.Date(2026, 10, 17, 21, 0, 0, 0, time.UTC)
	dials := 0
	p := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	p.now = func() time.Time { return clock }
	p.dial = func() (activitySetter, error) {
		dials++
		if app.down {
			return nil, errors.New("discord isn't running")
		}
		return app, nil
	}
	t.Cleanup(func() { p.SetEnabled(false) })
	return p, app, &clock, &dials
}

func frieren(position float64, paused bool) Watching {
	return Watching{Title: "Frieren", Episode: "Episode 5 of 28", Cover: "https://img/cover.jpg",
		Link: "https://anilist.co/anime/154587", Position: position, Duration: 1440, Paused: paused}
}

// The card follows what plays, but only what changes is sent: Discord allows five updates in 20 s.
func TestPresenceSendsOnlyChanges(t *testing.T) {
	p, app, clock, _ := newTest(t)
	p.Show(frieren(0, false))
	settle(p)
	if len(app.sent) != 0 {
		t.Fatal("sent while presence is off")
	}

	p.SetEnabled(true)
	p.Show(frieren(60, false))
	settle(p)
	if len(app.sent) != 1 {
		t.Fatalf("sent %d, want the card", len(app.sent))
	}
	a := app.sent[0].(map[string]any)
	if a["details"] != "Frieren" || a["state"] != "Episode 5 of 28" || a["type"] != 3 {
		t.Errorf("card %v", a)
	}
	ts := a["timestamps"].(map[string]any)
	if start := time.UnixMilli(ts["start"].(int64)); !start.Equal(clock.Add(-time.Minute)) {
		t.Errorf("started %s, want a minute ago", start)
	}

	// Ten seconds on, ten seconds further: nothing new to show.
	*clock = clock.Add(10 * time.Second)
	p.Show(frieren(70, false))
	settle(p)
	if len(app.sent) != 1 {
		t.Errorf("a report that changes nothing was sent")
	}

	*clock = clock.Add(5 * time.Second)
	p.Show(frieren(75, true))
	settle(p)
	if len(app.sent) != 2 || app.sent[1].(map[string]any)["state"] != "Paused · Episode 5 of 28" {
		t.Fatalf("pausing: %v", app.sent)
	}
	if _, ok := app.sent[1].(map[string]any)["timestamps"]; ok {
		t.Error("a paused card still counts down")
	}

	// A seek moves the counter.
	*clock = clock.Add(5 * time.Second)
	p.Show(frieren(600, false))
	settle(p)
	if len(app.sent) != 3 {
		t.Fatalf("a seek wasn't shown: %d sent", len(app.sent))
	}

	*clock = clock.Add(5 * time.Second)
	p.Clear()
	settle(p)
	if len(app.sent) != 4 || app.sent[3] != nil {
		t.Fatalf("clearing: %v", app.sent)
	}
}

// Without the Discord app nothing breaks; it's asked again only now and then, and picked up when it's back.
func TestPresenceWithoutDiscord(t *testing.T) {
	p, app, clock, dials := newTest(t)
	app.down = true
	p.SetEnabled(true)
	p.Show(frieren(10, false))
	settle(p)
	*clock = clock.Add(5 * time.Second)
	p.Show(Watching{Title: "Another", Episode: "Episode 1", Position: 1, Duration: 1400})
	settle(p)
	if *dials != 1 || len(app.sent) != 0 {
		t.Fatalf("asked again within %s, or sent with Discord shut: dials %d, sent %v", retryGap, *dials, app.sent)
	}

	// Discord opened mid-episode: the same card, unchanged, goes with the next report.
	app.down = false
	*clock = clock.Add(retryGap)
	p.Show(Watching{Title: "Another", Episode: "Episode 1", Position: 1 + retryGap.Seconds(), Duration: 1400})
	settle(p)
	if *dials != 2 || len(app.sent) != 1 {
		t.Fatalf("Discord back: dials %d, sent %d", *dials, len(app.sent))
	}

	// Discord quits mid-episode: the next change finds it gone and asks again later.
	app.fail = true
	*clock = clock.Add(5 * time.Second)
	p.Show(Watching{Title: "Third", Episode: "Episode 4", Position: 1, Duration: 1400})
	settle(p)
	app.fail = false
	*clock = clock.Add(retryGap)
	p.Show(Watching{Title: "Third", Episode: "Episode 5", Position: 1, Duration: 1400})
	settle(p)
	if *dials != 3 || len(app.sent) != 2 {
		t.Errorf("after Discord restarted: dials %d, sent %d", *dials, len(app.sent))
	}
}

// A Discord that stops answering holds up no caller, and the newest card is what's sent.
func TestPresenceNeverHoldsUpCallers(t *testing.T) {
	p, app, clock, _ := newTest(t)
	p.SetEnabled(true)
	p.Show(frieren(10, false))
	settle(p)

	app.stuck = make(chan struct{})
	*clock = clock.Add(5 * time.Second)
	p.Show(frieren(10, true))
	returned := make(chan struct{})
	go func() {
		for i := range 5 {
			*clock = clock.Add(5 * time.Second)
			p.Show(frieren(float64(100*i), false))
		}
		p.Clear()
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("a report waited on Discord")
	}
	close(app.stuck)
	settle(p)
	if last := app.sent[len(app.sent)-1]; last != nil {
		t.Errorf("the card left up was %v, want the clear asked for last", last)
	}
}

// Turned off while an update waits out the rate limit: the update is dropped, Discord not called again.
func TestPresenceOffStaysOff(t *testing.T) {
	p, app, clock, dials := newTest(t)
	p.SetEnabled(true)
	p.Show(frieren(10, false))
	settle(p)
	*clock = clock.Add(time.Second)
	p.Show(frieren(10, true))
	settle(p)
	p.SetEnabled(false)
	settle(p)
	time.Sleep(sendGap + 500*time.Millisecond)
	settle(p)
	if *dials != 1 || len(app.sent) != 1 || app.closed != 1 {
		t.Errorf("after turning off: dials %d, sent %d, closed %d; want one card, one dial, let go once",
			*dials, len(app.sent), app.closed)
	}
}

// Lines fit Discord's limits, and a show without a cover gets Yoru's image.
func TestPresenceCardLimits(t *testing.T) {
	long := ""
	for range 200 {
		long += "a"
	}
	c := card{w: Watching{Title: long, Episode: "E", Duration: 100}}
	a := c.activity()
	if n := len([]rune(a["details"].(string))); n != 128 {
		t.Errorf("details %d characters, want 128", n)
	}
	if n := len([]rune(a["state"].(string))); n < 2 {
		t.Errorf("state %q is under Discord's two characters", a["state"])
	}
	if a["assets"].(map[string]any)["large_image"] != "yoru" {
		t.Errorf("assets %v, want Yoru's image without a cover", a["assets"])
	}
	if _, ok := a["buttons"]; ok {
		t.Error("a button without a link")
	}
}
