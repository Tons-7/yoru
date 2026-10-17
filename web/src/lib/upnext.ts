import { useMemo } from 'react'
import type { UpNextItem } from './api'
import { useUpNext } from './queries'

/** "3 episodes · about 1 h 12 min", from what the catalogue knows of each episode's length. */
export function upNextSummary(items: UpNextItem[]): string {
  if (items.length === 0) return 'Empty'
  const count = items.length === 1 ? '1 episode' : `${items.length} episodes`
  const minutes = items.reduce((sum, it) => sum + Math.max(0, (it.minutes ?? 0) - (it.position ?? 0) / 60), 0)
  if (minutes < 1) return count
  const h = Math.floor(minutes / 60)
  const m = Math.round(minutes % 60)
  const length = h ? (m ? `${h} h ${m} min` : `${h} h`) : `${m} min`
  return `${count} · about ${length}`
}

/** The episodes of a show on the list, for the + toggles on its rows. */
export function useInUpNext(animeId: number): Set<number> {
  const list = useUpNext()
  return useMemo(
    () => new Set((list.data?.items ?? []).filter((it) => it.animeId === animeId).map((it) => it.episode)),
    [list.data, animeId],
  )
}
