package library

import (
	"context"
	"testing"
)

func TestAdoptLeavesAFetchInFlightAlone(t *testing.T) {
	c, st, engine := ownedFixture(t)
	engine.ids[1] = goodHash
	c.WithClaimed(func(hash string) bool { return hash == goodHash })

	if err := c.adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := remaining(t, st); n != 0 {
		t.Fatalf("adopted %d download(s) a fetch still had in hand", n)
	}

	c.WithClaimed(nil)
	if err := c.adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := remaining(t, st); n != 1 {
		t.Fatalf("the same download, no longer claimed, was not adopted: %d", n)
	}
}
