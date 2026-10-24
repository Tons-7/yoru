package power

import (
	"errors"
	"log/slog"
	"testing"
)

// One request however often it's asked for, one release, and a failing system isn't asked every check.
func TestKeeperHoldsOnceAndLetsGo(t *testing.T) {
	var held, released, asked int
	k := New(slog.New(slog.DiscardHandler))
	k.hold = func(string) (func(), error) {
		asked++
		held++
		return func() { released++ }, nil
	}
	k.Set("another device is watching")
	k.Set("downloads are running")
	if on, why := k.Awake(); !on || why != "downloads are running" || held != 1 {
		t.Fatalf("awake %v (%q) after %d holds, want one hold, renamed", on, why, held)
	}
	k.Set("")
	k.Set("")
	if on, _ := k.Awake(); on || released != 1 {
		t.Fatalf("awake %v after %d releases, want let go once", on, released)
	}

	asked = 0
	k.hold = func(string) (func(), error) { asked++; return nil, errors.New("no tool") }
	k.Set("downloads are running")
	k.Set("downloads are running")
	if on, _ := k.Awake(); on || asked != 1 {
		t.Fatalf("awake %v after %d tries, want one failed try", on, asked)
	}
	k.Set("")
	k.Set("downloads are running")
	if asked != 2 {
		t.Errorf("a new busy spell didn't try again: %d tries", asked)
	}
}

// The real request on this system: taken and let go without error.
func TestHoldTakesAndReleases(t *testing.T) {
	release, err := hold("a test")
	if err != nil {
		t.Skip("no power request here:", err)
	}
	release()
}
