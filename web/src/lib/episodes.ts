import type { Episode } from './api'

// Not out yet, so shown as scheduled rather than offered to play or download; now (ms) is from useNow.
export function isUnaired(episode: Episode, now: number) {
  return !!episode.airDate && episode.airDate * 1000 > now
}
