package store

import "context"

// display.spoilers: what of an unwatched episode (title, synopsis, still) is held back until it's watched or
// tapped open. The next one shows under the default, so Up next and the end card read as before.
const (
	SpoilersOff       = "off"
	SpoilersAfterNext = "after_next"
	SpoilersUnwatched = "unwatched"
)

// SpoilerMode is the show's rule; one completed or being rewatched hides nothing, as it has been seen.
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

// HidesSpoilers says whether an episode's details are held back, next being the first one not yet watched.
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
