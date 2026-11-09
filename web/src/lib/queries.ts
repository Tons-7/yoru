import { useEffect, useState } from 'react'
import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
  type UseQueryOptions,
} from '@tanstack/react-query'
import {
  api,
  query,
  type DiscoverItem,
  type Episode,
  type FollowState,
  type LibraryItem,
  type ListStatus,
  type Page,
  type ScheduleDay,
  type ScheduleItem,
  type Tracker,
  type UpNextItem,
} from './api'
import type { SpeedUnit } from './format'
import { toast } from './toast'

export const tzOffset = () => -new Date().getTimezoneOffset()

export function useHome() {
  return useQuery({
    queryKey: ['continue'],
    queryFn: () => api.get<Page<LibraryItem>>('/api/continue?perPage=20'),
    refetchInterval: 2 * 60_000,
  })
}

export function useHomePicks(kind: 'sequels' | 'for-you', enabled = true) {
  return useQuery({
    queryKey: ['home', kind],
    queryFn: () => api.get<{ items: DiscoverItem[] }>(`/api/home/${kind}`),
    staleTime: 30 * 60_000,
    enabled,
  })
}

export const discoverQuery = (sort: string, perPage = 24) => ({
  queryKey: ['discover', sort, perPage],
  queryFn: () => api.get<{ items: DiscoverItem[] }>(`/api/discover${query({ sort, perPage })}`),
  staleTime: 10 * 60_000,
})

export function useDiscover(sort: string, perPage = 24, keep = false) {
  return useQuery({ ...discoverQuery(sort, perPage), placeholderData: keep ? keepPreviousData : undefined })
}

export function useSetNotify(animeId: number) {
  const qc = useQueryClient()
  type Week = { days: ScheduleDay[]; items: ScheduleItem[] }
  const mark = (on: boolean) => (item: ScheduleItem) => (item.animeId === animeId ? { ...item, alerts: on } : item)
  return useMutation({
    meta: { inline: true },
    mutationFn: (on: boolean) => api.post<FollowState>(`/api/anime/${animeId}/notify`, { on }),
    onMutate: async (on) => {
      await qc.cancelQueries({ queryKey: ['schedule'] })
      const before = qc.getQueriesData<Week>({ queryKey: ['schedule'] })
      qc.setQueriesData<Week>({ queryKey: ['schedule'] }, (old) =>
        old && { ...old, items: old.items.map(mark(on)), days: old.days.map((d) => ({ ...d, items: d.items.map(mark(on)) })) },
      )
      return { before }
    },
    onError: (_error, _on, context) => {
      for (const [key, data] of context?.before ?? []) qc.setQueryData(key, data)
    },
    onSuccess: (state) => qc.setQueryData<{ notify?: FollowState }>(['anime', animeId], (old) => (old ? { ...old, notify: state } : old)),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['schedule'] }),
  })
}

export function useSchedule(days = 7, mine = false, back = 0, enabled = true) {
  return useQuery({
    enabled,
    queryKey: ['schedule', days, mine, back],
    queryFn: () =>
      api.get<{ days: ScheduleDay[]; items: ScheduleItem[] }>(
        `/api/schedule${query({
          tz: tzOffset(),
          days: days + back,
          mine: mine || undefined,
          start: back ? Math.floor(Date.now() / 1000) - back * 86_400 : undefined,
        })}`,
      ),
    staleTime: 5 * 60_000,
    refetchInterval: 5 * 60_000,
  })
}

export function useNow(every = 30_000) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), every)
    return () => window.clearInterval(id)
  }, [every])
  return now
}

export function useAired(hours = 48) {
  const now = useNow(60_000)
  const start = Math.floor(now / 3_600_000) * 3600 - hours * 3600
  return useQuery({
    queryKey: ['aired', hours, start],
    placeholderData: keepPreviousData,
    queryFn: () =>
      api.get<{ items: ScheduleItem[] }>(
        `/api/schedule${query({ start, days: Math.min(28, Math.ceil(hours / 24) + 1) })}`,
      ),
    staleTime: 5 * 60_000,
    refetchInterval: 5 * 60_000,
  })
}

export interface Season {
  id: number
  ordinal: number
  romaji: string
  english?: string | null
  cover?: string | null
  episodes?: number | null
  year?: number | null
  format?: string | null
  status?: string | null
  listStatus?: string | null
  progress: number
  onList: boolean
}

export interface RelatedEntry extends Season {
  kind: string
}

export function useFranchise(animeId?: number) {
  return useQuery({
    enabled: !!animeId,
    queryKey: ['franchise', animeId],
    queryFn: () =>
      api.get<{ rootId: number; seasons: Season[]; related: RelatedEntry[] }>(
        `/api/franchise?id=${animeId}`,
      ),
    staleTime: 30 * 60_000,
  })
}

export function useLibrary(
  f: { status: string; q?: string; sort?: string },
  page = 1,
  enabled = true,
) {
  return useQuery({
    enabled,
    queryKey: ['library', f.status, f.q ?? '', f.sort ?? '', page],
    queryFn: () =>
      api.get<Page<LibraryItem>>(`/api/library${query({ status: f.status, q: f.q, sort: f.sort, page })}`),
  })
}

export interface LibraryCounts {
  total: number
  statuses: Record<string, number>
  favourites: number
}

export function useLibraryCounts() {
  return useQuery({
    queryKey: ['library', 'counts'],
    queryFn: () => api.get<LibraryCounts>('/api/library/counts'),
    staleTime: 30_000,
  })
}

export function useTrackers() {
  return useQuery({
    queryKey: ['trackers'],
    queryFn: () => api.get<{ trackers: Tracker[] }>('/api/trackers'),
    staleTime: 60_000,
  })
}

export function useFavourites(page = 1, enabled = true) {
  return useQuery({
    enabled,
    queryKey: ['bookmarks', page],
    queryFn: () => api.get<Page<LibraryItem>>(`/api/bookmarks${query({ page })}`),
  })
}

export interface Download {
  infoHash: string
  name: string
  title?: string
  cover?: string
  animeId?: number
  episode?: string
  episodes: string[]
  totalBytes: number
  bytesOnDisk: number
  percent: number
  pinned: boolean
  kept: boolean
  state: string
  paused?: boolean
  checking?: boolean
  mbps?: number
  peers?: number
  warning?: string
  freeBytes?: number
  drive?: string
  group?: string
  resolution?: string
  codec?: string
  queued?: Queued
}

export interface ReleaseHold {
  animeId: number
  episode: number
  title: string
  cover?: string
  until: number
  wants: string
}

export function useReleaseHolds() {
  return useQuery({
    queryKey: ['release-holds'],
    queryFn: () => api.get<{ items: ReleaseHold[] }>('/api/releases/waiting'),
    staleTime: 0,
    refetchInterval: 60_000,
  })
}

export interface BetterCopy {
  animeId: number
  title: string
  cover?: string
  kept: string
  episodes: string[]
  size: number
  infoHash: string
  group: string
  resolution: string
  source?: string
  url?: string
  release: string
}

export function useBetterCopies(animeId?: number) {
  return useQuery({
    queryKey: ['better-copies', animeId ?? 0],
    queryFn: () => api.get<{ items: BetterCopy[] }>(`/api/better-copies${animeId ? `?anime=${animeId}` : ''}`),
    staleTime: 5 * 60_000,
  })
}

export interface DriveRoom {
  freeBytes?: number
  drive?: string
}

export function useDriveRoom() {
  return useQuery({
    queryKey: ['cache'],
    queryFn: () => api.get<DriveRoom>('/api/cache'),
    staleTime: 0,
    refetchInterval: 30_000,
  })
}

export interface Queued {
  animeId: number
  epKey: string
  episode: number
  state: string
  error?: string
  title?: string
  cover?: string
  infoHash?: string
  release?: string
  attempts?: number
  retryAt?: number
  torrent?: Download
}

function pollable(items: Queued[] = []): boolean {
  return items.some((q) => q.state !== 'failed' && !(q.retryAt && q.retryAt - Date.now() > 60_000))
}

export function useDownloadQueue() {
  return useQuery({
    queryKey: ['download-queue'],
    queryFn: () =>
      api.get<{
        items: Queued[]
        waiting: Record<string, number>
        paused?: boolean
        watching?: boolean
        short?: { freeBytes: number; drive: string; needBytes: number }
      }>('/api/download/queue'),
    staleTime: 0,
    refetchInterval: (q) => (pollable(q.state.data?.items) ? 5000 : 15_000),
  })
}

export function useDownloads() {
  const qc = useQueryClient()
  const result = useQuery({
    queryKey: ['downloads'],
    queryFn: async () => {
      const before = qc.getQueryData<{ items: Download[] }>(['downloads'])
      const next = await api.get<{ items: Download[]; count: number }>('/api/downloads')
      if (before && finishedHashes(before.items) !== finishedHashes(next.items)) {
        void qc.invalidateQueries({ queryKey: ['episodes'] })
        void qc.invalidateQueries({ queryKey: ['cache'] })
      }
      return next
    },
    staleTime: 0,
    refetchInterval: (q) => {
      const moving = (q.state.data?.items ?? []).some((d) => !d.paused && d.percent < 100)
      const queued = pollable(qc.getQueryData<{ items: Queued[] }>(['download-queue'])?.items)
      return moving || queued ? 5000 : 15_000
    },
  })
  return result
}

const finishedHashes = (items: Download[]) =>
  items
    .filter((d) => d.percent >= 100)
    .map((d) => d.infoHash)
    .sort()
    .join()

export function refreshDownloads(qc: QueryClient) {
  for (const key of ['downloads', 'download-queue', 'cache', 'episodes']) {
    void qc.invalidateQueries({ queryKey: [key] })
  }
}

export function useEpisodes(animeId: number | undefined) {
  return useQuery({
    enabled: !!animeId,
    queryKey: ['episodes', animeId],
    queryFn: () =>
      api.get<{ items: Episode[]; count: number; titlesPending?: boolean }>(
        `/api/episodes${query({ id: animeId })}`,
      ),
    refetchInterval: (q) => (q.state.data?.titlesPending ? 3000 : false),
  })
}

export function useRecommendations(animeId: number | undefined) {
  return useQuery({
    enabled: !!animeId,
    queryKey: ['recommend', animeId],
    queryFn: ({ signal }) =>
      api.get<{ items: DiscoverItem[]; source: string }>(
        `/api/recommend${query({ anime: animeId, limit: 18 })}`,
        signal,
      ),
    staleTime: 60 * 60_000,
  })
}

export interface Theme {
  title: string
  artist?: string
  episodes?: string
}

export interface Credit {
  id?: number
  name: string
  role: string
  image?: string
}

export interface AnimeExtra {
  openings?: Theme[]
  endings?: Theme[]
  staff?: Credit[]
  trailer?: string
}

export function useAnimeExtra(animeId: number) {
  return useQuery({
    enabled: animeId > 0,
    queryKey: ['anime-extra', animeId],
    queryFn: () => api.get<AnimeExtra>(`/api/anime/${animeId}/extra`),
    staleTime: 12 * 60 * 60_000,
  })
}

export function useFilters() {
  return useQuery({
    queryKey: ['filters'],
    queryFn: () => api.get<Record<string, unknown>>('/api/filters'),
    staleTime: 24 * 60 * 60_000,
  })
}

export function usePrefs(animeId?: number) {
  return useQuery({
    queryKey: ['prefs', animeId ?? 0],
    queryFn: () =>
      api.get<{
        effective: Record<string, string>
        defaults: Record<string, string>
        overrides?: Record<string, string>
      }>(`/api/prefs${query({ anime: animeId })}`),
  })
}

export function useSpeedUnit(): SpeedUnit {
  return usePrefs().data?.effective['display.speed'] === 'mbs' ? 'mbs' : 'mbps'
}

export function usePrefFlag(key: string): [boolean, (on: boolean) => void, boolean] {
  const prefs = usePrefs()
  const setPref = useSetPref()
  const settling = setPref.variables?.key === key && (setPref.isPending || (setPref.isSuccess && prefs.isFetching))
  const on = settling ? setPref.variables?.value === 'true' : prefs.data?.effective[key] === 'true'
  return [on, (next) => setPref.mutate({ key, value: String(next) }), !prefs.isPending]
}

export function useAlertsOff() {
  return usePrefs().data?.effective['notify.enabled'] === 'false'
}

export function useSetPref() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { key: string; value: string; animeId?: number }) =>
      api.post('/api/prefs', v),
    onSuccess: (_res, v) => {
      if (v.key === 'display.titles') {
        void qc.invalidateQueries()
        return
      }
      void qc.invalidateQueries({ queryKey: ['prefs'] })
      void qc.invalidateQueries({ queryKey: ['settings'] })
      if (v.key === 'home.adult_picks' || v.key === 'notify.enabled') {
        void qc.invalidateQueries({ queryKey: ['schedule'] })
        void qc.invalidateQueries({ queryKey: ['aired'] })
      }
    },
  })
}

const LIST_STATE_KEYS = [
  'library', 'continue', 'discover', 'browse', 'schedule', 'anime', 'episodes', 'franchise',
  'recommend', 'aired', 'bookmarks', 'search', 'history', 'upnext', 'upnext-after', 'home',
]

export function useUpNext() {
  return useQuery({
    queryKey: ['upnext'],
    queryFn: () => api.get<{ items: UpNextItem[] }>('/api/upnext'),
    refetchInterval: 2 * 60_000,
  })
}

function reorderShows(items: UpNextItem[], animeId: number, to: number): UpNextItem[] {
  const shows = [...new Set(items.map((it) => it.animeId))].filter((id) => id !== animeId)
  shows.splice(Math.min(Math.max(to, 0), shows.length), 0, animeId)
  return shows.flatMap((id) => items.filter((it) => it.animeId === id))
}

export function useUpNextEdit() {
  const qc = useQueryClient()
  const mutationKey = ['upnext-edit']
  const refresh = () => {
    if (qc.isMutating({ mutationKey }) > 1) return
    void qc.invalidateQueries({ queryKey: ['upnext'] })
    void qc.invalidateQueries({ queryKey: ['upnext-after'] })
  }
  const optimistic = async (change: (items: UpNextItem[]) => UpNextItem[]) => {
    await qc.cancelQueries({ queryKey: ['upnext'] })
    const before = qc.getQueryData<{ items: UpNextItem[] }>(['upnext'])
    if (before) qc.setQueryData(['upnext'], { items: change(before.items) })
    return { before }
  }
  const restore = (_e: unknown, _v: unknown, ctx?: { before?: { items: UpNextItem[] } }) => {
    if (ctx?.before) qc.setQueryData(['upnext'], ctx.before)
  }
  return {
    add: useMutation({
      mutationKey,
      mutationFn: (v: { animeId: number; episode: number }) => api.post('/api/upnext', v),
      onMutate: (v) =>
        optimistic((items) => {
          const show = items.find((it) => it.animeId === v.animeId)
          const added = [...items, { ...show, ...v, title: show?.title ?? '', episodeTitle: undefined, position: 0 }]
          const shows = [...new Set(added.map((it) => it.animeId))]
          return shows.flatMap((id) => added.filter((it) => it.animeId === id).sort((a, b) => a.episode - b.episode))
        }),
      onError: restore,
      onSettled: refresh,
    }),
    remove: useMutation({
      mutationKey,
      mutationFn: (v: { animeId: number; episode?: number }) =>
        api.del(`/api/upnext/${v.animeId}${v.episode ? query({ episode: v.episode }) : ''}`),
      onMutate: (v) =>
        optimistic((items) => items.filter((it) => it.animeId !== v.animeId || (!!v.episode && it.episode !== v.episode))),
      onError: restore,
      onSettled: refresh,
    }),
    move: useMutation({
      mutationKey,
      mutationFn: (v: { animeId: number; to: number }) => api.post('/api/upnext/move', v),
      onMutate: (v) => optimistic((items) => reorderShows(items, v.animeId, v.to)),
      onError: restore,
      onSettled: refresh,
    }),
    fill: useMutation({
      mutationKey,
      mutationFn: () => api.post<{ added: number }>('/api/upnext/fill'),
      onSuccess: (r) => toast(r.added === 0 ? 'Nothing to add' : `Added ${r.added}`, 'info'),
      onSettled: refresh,
    }),
    clear: useMutation({
      mutationKey,
      mutationFn: () => api.del('/api/upnext'),
      onSettled: refresh,
    }),
  }
}

export function refreshListState(qc: QueryClient) {
  for (const key of LIST_STATE_KEYS) void qc.invalidateQueries({ queryKey: [key] })
}

export function useRemoveStatus() {
  const qc = useQueryClient()
  return useMutation({
    meta: { inline: true },
    mutationFn: (animeId: number) => api.del(`/api/status/${animeId}`),
    onSuccess: () => refreshListState(qc),
  })
}

export function useSetStatus({ inline = false }: { inline?: boolean } = {}) {
  const qc = useQueryClient()
  return useMutation({
    meta: { inline },
    mutationFn: (v: { animeId: number; status: ListStatus; score?: number }) =>
      api.post('/api/status', v),
    onSuccess: () => refreshListState(qc),
  })
}

export interface Notification {
  id: number
  kind: string
  animeId: number
  episode: number
  title: string
  body: string
  createdAt: number
  read: boolean
}

export function useNotifications() {
  return useQuery({
    queryKey: ['notifications'],
    queryFn: () =>
      api.get<{ items: Notification[]; unread: number }>('/api/notifications?perPage=30'),
    refetchInterval: 5 * 60_000,
  })
}

export interface SetupComponent {
  name: string
  label: string
  purpose: string
  size: string
  required?: boolean
  role?: string
  present: boolean
  version?: string
  latest?: string
  needs?: string
  manual?: string
  system?: boolean
  problem?: string
}

export interface SetupProgress {
  component: string
  stage: 'resolving' | 'downloading' | 'extracting' | 'done' | 'failed'
  version?: string
  bytes: number
  total: number
  error?: string
}

export interface SetupState {
  components: SetupComponent[]
  ready: boolean
  encoder?: string
  toneMapper?: string
  indexers: number
  badIndexers?: string[]
  defaultDataDir?: string
  configPath: string
  dataDir: string
  vlc?: string
  strayConfig?: string
  temporary: boolean
  binDir: string
  cacheDir: string
  cacheBudget: number
  libraryPaths: string[]
  progress: SetupProgress[]
}

export function useSetup(options?: {
  refetchInterval?: UseQueryOptions<SetupState>['refetchInterval']
}) {
  return useQuery({
    queryKey: ['setup'],
    queryFn: () => api.get<SetupState>('/api/setup'),
    staleTime: 30_000,
    ...options,
  })
}

export interface AccessState {
  listening: string
  reachable: boolean
  token: string
  urls: string[]
  canSwitch: boolean
  host: boolean
  away?: boolean
  proxy?: { seen: number; client: string; site: string }
}

export function useAccess() {
  return useQuery({ queryKey: ['access'], queryFn: () => api.get<AccessState>('/api/access') })
}

export interface AccessDevice {
  id: string
  name: string
  addr: string
  status: 'pending' | 'approved' | 'denied' | 'lapsed'
  requestedAt: number
  decidedAt: number
}

export type ApprovalMode = 'off' | 'once' | 'always'

export function useAccessDevices(host: boolean) {
  return useQuery({
    queryKey: ['access-devices'],
    queryFn: () =>
      api.get<{ mode: ApprovalMode; devices: AccessDevice[]; idleMinutes: number }>('/api/access/devices'),
    enabled: host,
    staleTime: 0,
    refetchInterval: (q) => (q.state.data && q.state.data.mode !== 'off' ? 4000 : false),
  })
}

export function useDecideDevice() {
  const qc = useQueryClient()
  return useMutation({
    meta: { inline: true },
    mutationFn: ({ id, status }: { id: string; status: 'approved' | 'denied' | 'removed' }) =>
      status === 'removed'
        ? api.del(`/api/access/devices/${id}`)
        : api.post(`/api/access/devices/${id}`, { status }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['access-devices'] }),
  })
}
