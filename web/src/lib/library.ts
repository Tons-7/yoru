import { useState } from 'react'
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api, query, statusLabel, type LibraryItem, type ListStatus, type Page } from './api'
import { useMediaQuery } from './hooks'
import { libraryProgress, type LibraryProgress } from './libraryText'
import { refreshListState, useNow, type LibraryCounts } from './queries'
import { readKey, writeKey } from './storage'
import { toast } from './toast'
import { removeWithUndo } from './undo'

export function useProgress(item: LibraryItem): LibraryProgress {
  const now = useNow(600_000) / 1000
  return libraryProgress(item, now)
}

export const STATUS_ORDER: ListStatus[] = ['CURRENT', 'PLANNING', 'REPEATING', 'PAUSED', 'COMPLETED', 'DROPPED']

export const SORTS = [
  { value: 'watched', label: 'Last watched' },
  { value: 'new', label: 'New episodes first' },
  { value: 'added', label: 'Date added' },
  { value: 'title', label: 'Title A to Z' },
  { value: 'score', label: 'Your score' },
  { value: 'airing', label: 'Airing next' },
] as const

export const DEFAULT_SORT = 'watched'

const OTHER_SORTS: Record<string, string> = { progress: 'Progress', status: 'Status', updated: 'Recently updated' }

export function sortLabel(sort: string): string {
  return SORTS.find((s) => s.value === sort)?.label ?? OTHER_SORTS[sort] ?? 'Last watched'
}

export type Counts = LibraryCounts & { favouriteStatuses?: Record<string, number> }

export function countFor(counts: Counts | undefined, status: string, fav: boolean): number | undefined {
  if (!counts) return undefined
  if (fav) return status ? (counts.favouriteStatuses?.[status] ?? 0) : counts.favourites
  return status ? (counts.statuses[status] ?? 0) : counts.total
}

export type Layout = 'grid' | 'list'
const LAYOUT_KEY = 'yoru.library.layout'

export function useLayout(): [Layout, (layout: Layout) => void] {
  const [layout, setLayout] = useState<Layout>(() => (readKey(LAYOUT_KEY) === 'list' ? 'list' : 'grid'))
  const choose = (next: Layout) => {
    setLayout(next)
    writeKey(LAYOUT_KEY, next)
  }
  return [layout, choose]
}

export function useGridColumns(): number {
  const sm = useMediaQuery('(min-width: 40rem)')
  const md = useMediaQuery('(min-width: 48rem)')
  const lg = useMediaQuery('(min-width: 64rem)')
  const xl = useMediaQuery('(min-width: 80rem)')
  const xxl = useMediaQuery('(min-width: 1800px)')
  const huge = useMediaQuery('(min-width: 2200px)')
  return huge ? 10 : xxl ? 8 : xl ? 7 : lg ? 6 : md ? 5 : sm ? 4 : 3
}

export interface ListFilter {
  status: string
  q: string
  sort: string
  fav: boolean
}

export function useLibraryItems(f: ListFilter, page: number, perPage: number, enabled = true) {
  return useQuery({
    enabled,
    queryKey: ['library', 'view', f.status, f.q, f.sort, f.fav, page, perPage],
    queryFn: ({ signal }) =>
      api.get<Page<LibraryItem>>(
        `/api/library${query({ status: f.status, q: f.q, sort: f.sort, fav: f.fav ? 1 : undefined, page, perPage })}`,
        signal,
      ),
  })
}

export const hiddenKey = (id: number) => `library:${id}`

type PageData = Page<LibraryItem>

export function patchItem(qc: QueryClient, id: number, patch: Partial<LibraryItem>) {
  qc.setQueriesData<PageData>({ queryKey: ['library'] }, (old) =>
    old && Array.isArray(old.items)
      ? { ...old, items: old.items.map((it) => (it.id === id ? { ...it, ...patch } : it)) }
      : old,
  )
}

const ELSEWHERE = [
  'continue', 'discover', 'browse', 'schedule', 'anime', 'episodes', 'franchise', 'recommend', 'aired',
  'bookmarks', 'search', 'history', 'upnext', 'upnext-after', 'home',
]

export function refreshElsewhere(qc: QueryClient) {
  for (const key of ELSEWHERE) void qc.invalidateQueries({ queryKey: [key] })
  void qc.invalidateQueries({ queryKey: ['library', 'counts'] })
}

export function progressPatch(item: LibraryItem, target: number): Partial<LibraryItem> {
  const total = item.episodes ?? 0
  if (target > item.progress) {
    const status =
      total > 0 && target >= total && item.status !== 'COMPLETED'
        ? 'COMPLETED'
        : item.status === 'COMPLETED' || item.status === 'REPEATING'
          ? item.status
          : 'CURRENT'
    return { progress: target, status }
  }
  return { progress: target, status: item.status === 'COMPLETED' ? 'CURRENT' : item.status }
}

export function sendProgress(animeId: number, from: number, to: number) {
  if (to > from) return api.post('/api/watched', { animeId, episode: to, watched: true })
  if (to < from) return api.post('/api/watched', { animeId, episode: to + 1, watched: false })
  return Promise.resolve()
}

export function usePlusOne() {
  const qc = useQueryClient()
  const plusOne = (item: LibraryItem) => {
    const episode = item.progress + 1
    const before: Partial<LibraryItem> = { progress: item.progress, status: item.status }
    const after = progressPatch(item, episode)
    patchItem(qc, item.id, after)
    sendProgress(item.id, item.progress, episode).then(
      () => {
        refreshListState(qc)
        toast(`Episode ${episode} of ${item.title} watched`, 'info', {
          label: 'Undo',
          run: () => {
            patchItem(qc, item.id, before)
            const unwound = after.status === 'COMPLETED' ? 'CURRENT' : after.status
            const restore = !!before.status && before.status !== unwound && before.status !== 'REPEATING'
            sendProgress(item.id, episode, item.progress)
              .then(() => (restore ? api.post('/api/status', { animeId: item.id, status: before.status }) : undefined))
              .then(
                () => refreshListState(qc),
                (e: Error) => {
                  refreshListState(qc)
                  toast(`Couldn't undo episode ${episode} of ${item.title}: ${e.message}`)
                },
              )
          },
        })
      },
      (e: Error) => {
        patchItem(qc, item.id, before)
        toast(`Couldn't mark episode ${episode} of ${item.title} watched: ${e.message}`, 'error', {
          label: 'Try again',
          run: () => plusOne(item),
        })
      },
    )
  }
  return plusOne
}

export function useRemoveFromLibrary() {
  const qc = useQueryClient()
  return (item: LibraryItem) => {
    const listed = !!item.status
    removeWithUndo({
      keys: [hiddenKey(item.id)],
      message: listed ? `Removed ${item.title} from your library` : `Removed ${item.title} from favourites`,
      failed: listed ? `Couldn't remove ${item.title}` : `Couldn't take ${item.title} out of favourites`,
      run: () =>
        (listed
          ? api.del(`/api/status/${item.id}`)
          : api.post('/api/bookmarks', { animeId: item.id, favourite: false })
        ).then(() => {
          refreshListState(qc)
          return qc.invalidateQueries({ queryKey: ['library'] })
        }),
    })
  }
}

export function statusWord(status?: string | null): string {
  return statusLabel(status) || 'Not in a list'
}
