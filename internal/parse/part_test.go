package parse

import "testing"

func TestPartOf(t *testing.T) {
	for title, want := range map[string]int{
		"Show Part 2":                   2,
		"Show Part 6 - Arc Part 2 - 05": 2,
		"Show - Recap Part 1":           0,
		"Show part II":                  2,
		"Show Part IV":                  4,
		"Show Cour 2":                   2,
		"Show cour 3":                   3,
		"Show":                          0,
		"Partners":                      0,
		"Show Season 2":                 0,
		"Show Part I - Next":            1,
	} {
		if got := PartOf(title); got != want {
			t.Errorf("%q = %d, want %d", title, got, want)
		}
	}
}
