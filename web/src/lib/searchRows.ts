import type { DiscoverItem, LibraryItem } from './api'

export interface SearchRow {
  id: number
  title: string
  thumb: string | null
  color: string | null
  format: string | null
  year: number | null
  episodes: number | null
  airing: string | null
  listStatus: string | null
}

const FORMATS: Record<string, string> = {
  TV: 'TV',
  TV_SHORT: 'TV short',
  MOVIE: 'Movie',
  SPECIAL: 'Special',
  OVA: 'OVA',
  ONA: 'ONA',
  MUSIC: 'Music',
}

export function fromDiscover(item: DiscoverItem, listStatus?: string | null): SearchRow {
  const started = Number(item.startDate?.slice(0, 4))
  return {
    id: item.id,
    title: item.title,
    thumb: item.thumb ?? item.cover ?? null,
    color: item.color ?? null,
    format: item.format ?? null,
    year: item.seasonYear ?? (started || null),
    episodes: item.episodes ?? null,
    airing: item.status ?? null,
    listStatus: listStatus ?? item.listStatus ?? null,
  }
}

export function fromLocal(hit: LibraryItem): SearchRow {
  return {
    id: hit.id,
    title: hit.title,
    thumb: hit.cover,
    color: hit.color ?? null,
    format: hit.format ?? null,
    year: hit.seasonYear ?? null,
    episodes: hit.episodes,
    airing: hit.nextAiringAt ? 'RELEASING' : null,
    listStatus: hit.status,
  }
}

export function rowFacts(row: SearchRow): { text: string; airing?: boolean }[] {
  const facts: { text: string; airing?: boolean }[] = []
  if (row.format) facts.push({ text: FORMATS[row.format] ?? row.format })
  if (row.year) facts.push({ text: String(row.year) })
  if (row.episodes) facts.push({ text: `${row.episodes.toLocaleString()} ${row.episodes === 1 ? 'ep' : 'eps'}` })
  else if (row.airing === 'RELEASING') facts.push({ text: 'Airing', airing: true })
  else if (row.airing === 'NOT_YET_RELEASED') facts.push({ text: 'Not yet aired' })
  return facts
}

export function libraryFirst(items: DiscoverItem[], local: LibraryItem[] | undefined, page: number) {
  const byId = new Map(items.map((a) => [a.id, a]))
  const mine: SearchRow[] = []
  const seen = new Set<number>()
  if (page === 1) {
    for (const hit of local ?? []) {
      if (!hit.status || seen.has(hit.id)) continue
      const known = byId.get(hit.id)
      mine.push(known ? fromDiscover(known, hit.status) : fromLocal(hit))
      seen.add(hit.id)
    }
    for (const a of items) {
      if (!a.listStatus || seen.has(a.id)) continue
      mine.push(fromDiscover(a))
      seen.add(a.id)
    }
  }
  const rest = items.filter((a) => !seen.has(a.id)).map((a) => fromDiscover(a))
  return { mine, rest }
}
