package skipfind

import (
	"math"
	"math/rand/v2"
	"testing"
)

func noise(r *rand.Rand, n int) []uint32 {
	out := make([]uint32, n)
	for i := range out {
		out[i] = r.Uint32()
	}
	return out
}

func around(r *rand.Rand, song []uint32) []uint32 {
	out := noise(r, 300)
	out = append(out, song...)
	return append(out, noise(r, 300)...)
}

func TestSharedRejectsWhatIsNotASong(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for _, c := range []struct {
		name  string
		items int
	}{
		{"longer than any song", 1700},
		{"too short for a song", 120},
	} {
		song := noise(r, c.items)
		if _, ok := Shared(around(r, song), around(r, song)); ok {
			t.Errorf("%s (%d items) was taken as a song", c.name, c.items)
		}
	}

	silence := make([]uint32, 600)
	if _, ok := Shared(append(noise(r, 100), silence...), append(noise(r, 100), silence...)); ok {
		t.Error("shared silence was taken as a song")
	}

	song := noise(r, 730)
	m, ok := Shared(around(r, song), around(r, song))
	if !ok || math.Abs(m.Seconds()-730*Point) > 2 {
		t.Fatalf("a 90 s song: ok=%v, %.1f s", ok, m.Seconds())
	}
	if _, ok := Covers(song, append(noise(r, 200), song[:len(song)*6/10]...)); ok {
		t.Error("a window holding 60% of the song covers it")
	}
	if _, ok := Covers(song, around(r, song)); !ok {
		t.Error("a window holding the whole song does not cover it")
	}
}
