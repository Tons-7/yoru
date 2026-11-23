package store

import "context"

const (
	SpoilersOff       = "off"
	SpoilersAfterNext = "after_next"
	SpoilersUnwatched = "unwatched"
)

func (s *Store) SpoilerMode(ctx context.Context, animeID int) string {
	prefs, err := s.Prefs(ctx, animeID)
	if err != nil {
		return SpoilersOff
	}
	mode := prefs.String("display.spoilers")
	if mode == SpoilersOff {
		return mode
	}
	if entry, err := s.ListEntry(ctx, animeID); err == nil && (entry.Status == "COMPLETED" || entry.Status == StatusRepeating) {
		return SpoilersOff
	}
	return mode
}

func HidesSpoilers(mode string, number, next int, watched bool) bool {
	switch {
	case watched || mode == SpoilersOff:
		return false
	case mode == SpoilersAfterNext:
		return number > next
	default:
		return mode == SpoilersUnwatched
	}
}
