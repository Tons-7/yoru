package server

import (
	"testing"

	"yoru/internal/transcode"
)

// A chapter start within reach wins, nearest first; the chapter at 0 starts no part.
func TestNearChapter(t *testing.T) {
	chapters := []transcode.Chapter{{Start: 0}, {Start: 238}, {Start: 325}, {Start: 1334}}
	for _, c := range []struct {
		at   float64
		want float64
		ok   bool
	}{{236.5, 238, true}, {327, 325, true}, {1.5, 1.5, false}, {300, 300, false}} {
		if got, ok := nearChapter(chapters, c.at); got != c.want || ok != c.ok {
			t.Errorf("nearChapter(%v) = %v, %v; want %v, %v", c.at, got, ok, c.want, c.ok)
		}
	}
}
