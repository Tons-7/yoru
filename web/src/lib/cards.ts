import type { DiscoverItem, LibraryItem } from './api'

export interface CardAnime {
  id: number
  title: string
  cover?: string | null
  coverLarge?: string | null
  color?: string | null
  episodes?: number | null
  /** Episodes aired, where no total has been announced. */
  aired?: number | null
  format?: string | null
  score?: number | null
  progress?: number
  status?: string | null
  badge?: string
  percent?: number
  /** Your own rating, 0-100; shown as /10 on the card. */
  myScore?: number
  romaji?: string | null
  english?: string | null
  seasonYear?: number | null
  genres?: string[] | null
  description?: string | null
  /** Airing state, which the list status field shadows on a library item. */
  airing?: string | null
  banner?: string | null
  malId?: number | null
  nextEpisode?: number | null
  nextAiringAt?: number | null
  startDate?: string | null
  endDate?: string | null
  duration?: number | null
  studios?: { id: number; name: string }[] | null
}

export function toCard(item: DiscoverItem | LibraryItem): CardAnime {
  const discover = item as DiscoverItem
  const library = item as LibraryItem
  // "status" is the airing state on a search result but the list tag on a library row.
  const fromDiscover = 'onList' in item
  const nextEpisode = (fromDiscover ? discover.nextEpisode : library.nextEpisode) ?? null
  return {
    id: item.id,
    title: item.title,
    cover: item.cover ?? null,
    coverLarge: discover.coverLarge ?? null,
    color: (discover.color ?? library.color) ?? null,
    episodes: item.episodes ?? null,
    // Out so far while airing: "10/13", or "10" for a run with no total yet.
    aired: nextEpisode ? nextEpisode - 1 : null,
    format: discover.format ?? null,
    score: discover.score ?? null,
    progress: item.progress,
    status: fromDiscover ? (discover.listStatus ?? null) : (library.status ?? null),
    romaji: discover.romaji ?? null,
    english: discover.english ?? null,
    seasonYear: discover.seasonYear ?? null,
    genres: discover.Genres ?? null,
    description: discover.description ?? null,
    airing: fromDiscover ? (discover.status ?? null) : null,
    banner: discover.banner ?? null,
    malId: discover.malId ?? null,
    nextEpisode,
    nextAiringAt: (fromDiscover ? discover.nextAiringAt : library.nextAiringAt) ?? null,
    startDate: discover.startDate ?? null,
    endDate: discover.endDate ?? null,
    duration: discover.duration ?? null,
    studios: discover.studios ?? null,
  }
}
