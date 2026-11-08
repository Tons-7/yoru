package library

import "testing"

func TestOwnNumberTurnsSeasonNumbersIntoThePartsOwn(t *testing.T) {
	offsets := []int{12, 39}
	for _, c := range []struct{ n, limit, slack, want int }{
		{3, 6, 0, 3},
		{14, 6, 0, 2},
		{41, 6, 0, 2},
		{30, 6, 0, 0},
		{14, 0, 0, 14},
		{8, 6, 3, 8},
		{8, 6, 0, 0},
	} {
		if got := ownNumber(c.n, c.limit, c.slack, offsets); got != c.want {
			t.Errorf("ownNumber(%d, %d, %d) = %d, want %d", c.n, c.limit, c.slack, got, c.want)
		}
	}
}
