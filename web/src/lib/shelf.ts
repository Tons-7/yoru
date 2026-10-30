import { useState } from 'react'
import { useMediaQuery } from './hooks'
import { readKey, writeKey } from './storage'

export type Side = 'phone' | 'pc'
export type Sort = 'recent' | 'size' | 'name'

export const SORTS: ReadonlyArray<{ value: Sort; label: string }> = [
  { value: 'recent', label: 'Recent' },
  { value: 'size', label: 'Size' },
  { value: 'name', label: 'Name' },
]

const SIDE_KEY = 'yoru.downloads.side'
const SORT_KEY = 'yoru.downloads.sort'

export function lastSide(): Side | null {
  const v = readKey(SIDE_KEY)
  return v === 'phone' || v === 'pc' ? v : null
}

export const rememberSide = (side: Side) => writeKey(SIDE_KEY, side)

export function useSort(): [Sort, (sort: Sort) => void] {
  const [sort, setSort] = useState<Sort>(() => {
    const v = readKey(SORT_KEY)
    return v === 'size' || v === 'name' ? v : 'recent'
  })
  return [
    sort,
    (next) => {
      writeKey(SORT_KEY, next)
      setSort(next)
    },
  ]
}

export interface Shelf {
  recent: number
  size: number
  name: string
}

export function sortShelf<T>(items: T[], sort: Sort, facts: (item: T) => Shelf): T[] {
  const keyed = items.map((item) => ({ item, f: facts(item) }))
  keyed.sort((a, b) =>
    sort === 'size'
      ? b.f.size - a.f.size
      : sort === 'name'
        ? a.f.name.localeCompare(b.f.name)
        : b.f.recent - a.f.recent,
  )
  return keyed.map((k) => k.item)
}

export function useDeviceWord(): 'phone' | 'device' {
  const phone = useMediaQuery('(pointer: coarse) and (max-width: 40rem), (pointer: coarse) and (max-height: 30rem)')
  return phone ? 'phone' : 'device'
}

export function ordinal(n: number): string {
  const tens = n % 100
  if (tens >= 11 && tens <= 13) return `${n}th`
  return `${n}${['th', 'st', 'nd', 'rd'][n % 10] ?? 'th'}`
}

export const episodeCount = (n: number) => `${n} ${n === 1 ? 'episode' : 'episodes'}`

export function aboutLeft(seconds: number): string {
  if (!isFinite(seconds) || seconds <= 0) return ''
  if (seconds < 60) return 'under a minute left'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `about ${minutes} min left`
  const hours = Math.floor(minutes / 60)
  return `about ${hours} h ${minutes % 60} min left`
}

export const LOW_SHARE = 0.1
export const LOW_BYTES = 10 * 1024 ** 3
