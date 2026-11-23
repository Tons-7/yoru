import type { Episode } from './api'

export function isUnaired(episode: Episode, now: number) {
  return !!episode.airDate && episode.airDate * 1000 > now
}
