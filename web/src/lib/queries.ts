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

// getTimezoneOffset has the opposite sign to what the schedule endpoint wants.
export const tzOffset = () => -new Date().getTimezoneOffset()

export function useHome() {
  return useQuery({
    queryKey: ['continue'],
    queryFn: () => api.get<Page<LibraryItem>>('/api/continue?perPage=20'),
    // Progress moves while an episode plays, including from mpv or a phone.
    refetchInterval: 2 * 60_000,
  })
}

export function useDiscover(sort: string, perPage = 24) {
  return useQuery({
    queryKey: ['discover', sort, perPage],
    queryFn: () => api.get<{ items: DiscoverItem[] }>(`/api/discover${query({ sort, perPage })}`),
    // Trending moves slowly, and a refetch per tab focus wastes AniList's 30 a minute.
    staleTime: 10 * 60_000,
  })
}

export function useSchedule(days = 7, mine = false) {
  return useQuery({
    queryKey: ['schedule', days, mine],
    queryFn: () =>
      api.get<{ days: ScheduleDay[]; items: ScheduleItem[] }>(
        `/api/schedule${query({ tz: tzOffset(), days, mine: mine || undefined })}`,
      ),
    staleTime: 5 * 60_000,
    // Which episode is next changes with the clock, not with any user action.
    refetchInterval: 5 * 60_000,
  })
}

// Re-renders on a timer so relative times ("5h ago", "in 2h") advance on their own.
export function useNow(every = 30_000) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), every)
    return () => window.clearInterval(id)
  }, [every])
  return now
}

export function useAired(hours = 48) {
  // The clock hook, not Date.now(): render stays pure, and an open page moves on the hour.
  const now = useNow(60_000)
  const start = Math.floor(now / 3_600_000) * 3600 - hours * 3600
  return useQuery({
    queryKey: ['aired', hours, start],
    // The window moves on the hour, which is a new key; the last answer stays up until the next lands.
    placeholderData: keepPreviousData,
    queryFn: () =>
      // The window has to reach now, whatever the span.
      api.get<{ items: ScheduleItem[] }>(
        `/api/schedule${query({ start, days: Math.min(28, Math.ceil(hours / 24) + 1) })}`,
      ),
    staleTime: 5 * 60_000,
    // New episodes appear on their own, so a page left open stays current.
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
  /** What the user tagged it, as opposed to whether it has finished airing. */
  listStatus?: string | null
  progress: number
  onList: boolean
}

/** A film, OVA, special or spin-off: same franchise, not part of the seasons. */
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
  /** The release filename. `title` is the show it belongs to. */
  name: string
  title?: string
  cover?: string
  animeId?: number
  /** Episodes joined for display; a pack has several. */
  episode?: string
  episodes: string[]
  totalBytes: number
  bytesOnDisk: number
  percent: number
  pinned: boolean
  /** Asked for, so outside the cache budget and never evicted. */
  kept: boolean
  state: string
  paused?: boolean
  /** The engine hashing data already on disk. */
  checking?: boolean
  mbps?: number
  peers?: number
  /** The engine can't write it to disk and keeps retrying; with the drive's room left when that is known. */
  warning?: string
  freeBytes?: number
  drive?: string
  /** The queue entry this torrent is the download of, paired client-side. */
  queued?: Queued
}

/** A new episode auto-download holds for a better release, until it lands or the wait ends. */
export interface ReleaseHold {
  animeId: number
  episode: number
  title: string
  cover?: string
  /** When the best there is gets taken anyway, unix seconds. */
  until: number
  /** What it waits for: "SubsPlease at 1080p", "a 1080p release". */
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

/** SeaDex's best release for a show whose kept episodes are another copy; offered, never taken on its own. */
export interface BetterCopy {
  animeId: number
  title: string
  cover?: string
  /** One kept episode's release, to say what would go. */
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

/** Room left on the downloads drive, from /api/cache. */
export interface DriveRoom {
  freeBytes?: number
  drive?: string
}

/** Free space where downloads go: shown in Downloads, and checked before a whole show is queued. */
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
  /** A release chosen by hand: downloaded instead of whatever the search would pick. */
  infoHash?: string
  release?: string
  /** Tries that found a dead swarm, and when the next is due (unix ms); `error` says why it waits. */
  attempts?: number
  retryAt?: number
  /** A failed entry's torrent, paired client-side: what it got before giving up. */
  torrent?: Download
}

// Queue entries worth polling fast for: not failed, and not waiting minutes for a retry.
function pollable(items: Queued[] = []): boolean {
  return items.some((q) => q.state !== 'failed' && !(q.retryAt && q.retryAt - Date.now() > 60_000))
}

// The queue and downloads are local, cheap reads: always fresh on arrival, polled fast while anything moves.
export function useDownloadQueue() {
  return useQuery({
    queryKey: ['download-queue'],
    queryFn: () =>
      api.get<{
        items: Queued[]
        waiting: Record<string, number>
        paused?: boolean
        watching?: boolean
        /** The downloads drive is nearly full, so nothing starts until there's room. */
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
    // A finished download changes the episode lists' on-disk marks and the cache figures; noticed here, once
    // per fetch, rather than by every component holding this hook.
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
    // A queued episode becomes a download between polls, so a busy queue counts as activity.
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

/** Everything that shows download state, refreshed together after a download action. */
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
      api.get<{ items: Episode[]; count: number }>(`/api/episodes${query({ id: animeId })}`),
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

/** A show's themes, staff and trailer, fetched apart from the details as two more requests. */
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

/** The unit Settings shows speeds in; megabits until the prefs arrive. */
export function useSpeedUnit(): SpeedUnit {
  return usePrefs().data?.effective['display.speed'] === 'mbs' ? 'mbs' : 'mbps'
}

export function useSetPref() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { key: string; value: string; animeId?: number }) =>
      api.post('/api/prefs', v),
    onSuccess: (_res, v) => {
      // Titles are picked server-side on nearly every list, wherever the setting was changed from.
      if (v.key === 'display.titles') {
        void qc.invalidateQueries()
        return
      }
      void qc.invalidateQueries({ queryKey: ['prefs'] })
      void qc.invalidateQueries({ queryKey: ['settings'] })
    },
  })
}

// Everything that shows list state or progress: a status change, a tick or an import refreshes all of it.
const LIST_STATE_KEYS = [
  'library', 'continue', 'discover', 'browse', 'schedule', 'anime', 'episodes', 'franchise',
  'recommend', 'aired', 'bookmarks', 'search', 'history', 'upnext', 'upnext-after',
]

export function useUpNext() {
  return useQuery({
    queryKey: ['upnext'],
    queryFn: () => api.get<{ items: UpNextItem[] }>('/api/upnext'),
    // Watched episodes leave it, from mpv or a phone too.
    refetchInterval: 2 * 60_000,
  })
}

/** The list with one show moved to place `to` among the shows, as the server will have it. */
function reorderShows(items: UpNextItem[], animeId: number, to: number): UpNextItem[] {
  const shows = [...new Set(items.map((it) => it.animeId))].filter((id) => id !== animeId)
  shows.splice(Math.min(Math.max(to, 0), shows.length), 0, animeId)
  return shows.flatMap((id) => items.filter((it) => it.animeId === id))
}

/** Every change to the Up next list, each refreshing it and what the player would play after. */
export function useUpNextEdit() {
  const qc = useQueryClient()
  const mutationKey = ['upnext-edit']
  // After the last change in flight only: an earlier one's refetch would bring back what a later one changed.
  const refresh = () => {
    if (qc.isMutating({ mutationKey }) > 1) return
    void qc.invalidateQueries({ queryKey: ['upnext'] })
    void qc.invalidateQueries({ queryKey: ['upnext-after'] })
  }
  // Shown at once, so a dropped row stays where it was put; put back if the server says no.
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
      // With the show's other episodes in episode order, else at the bottom; the refetch fills in the rest.
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
      // Without an episode, the whole show.
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

/** Takes a show off the list, on the trackers first. */
export function useRemoveStatus() {
  const qc = useQueryClient()
  return useMutation({
    meta: { inline: true },
    mutationFn: (animeId: number) => api.del(`/api/status/${animeId}`),
    onSuccess: () => refreshListState(qc),
  })
}

/** inline: the caller shows the error itself, so no toast. */
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
  /** Shown in place of "Optional" where that would mislead. */
  role?: string
  present: boolean
  version?: string
  /** Newest published version, once looked up. */
  latest?: string
  needs?: string
  /** A package-manager command, for what Yoru cannot fetch on this OS. */
  manual?: string
  /** Found on PATH rather than downloaded by Yoru. */
  system?: boolean
  /** Why one that is there cannot be used. */
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
  /** Torrent sites searched, saved in the app or config.toml's. None ship with Yoru. */
  indexers: number
  /** Sites skipped, with the reason. */
  badIndexers?: string[]
  /** Where history lives when data_dir is empty. */
  defaultDataDir?: string
  configPath: string
  /** Database and window profile: AppData, or a data folder beside the exe. */
  dataDir: string
  /** Where VLC was found, or empty. */
  vlc?: string
  /** A config.toml.txt beside the real file, which Yoru never reads. */
  strayConfig?: string
  /** Running from the OS temp folder, as an exe opened from inside a zip does. */
  temporary: boolean
  binDir: string
  cacheDir: string
  cacheBudget: number
  libraryPaths: string[]
  progress: SetupProgress[]
}

/** Shared so the setup page and the first-run nudge read one cached answer. */
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
  /** The address Yoru is bound to. */
  listening: string
  /** Reachable from other devices, not loopback alone. */
  reachable: boolean
  token: string
  /** Links another device opens, HTTPS first; empty with no home-network address. */
  urls: string[]
  /** The host, whose page may open or close Yoru to the network. */
  canSwitch: boolean
  /** This page runs on the machine Yoru runs on. */
  host: boolean
  /** Host only: the last request a proxy on this machine passed on, and where it's reached from outside. */
  proxy?: { seen: number; client: string; site: string }
}

/** Yoru's reach and whether this page is on its machine; one query, read by every page that needs it. */
export function useAccess() {
  return useQuery({ queryKey: ['access'], queryFn: () => api.get<AccessState>('/api/access') })
}

export interface AccessDevice {
  id: string
  name: string
  addr: string
  /** lapsed: accepted, then away too long under "every time". */
  status: 'pending' | 'approved' | 'denied' | 'lapsed'
  requestedAt: number
  decidedAt: number
}

export type ApprovalMode = 'off' | 'once' | 'always'

/** Devices asking the host for access. Host-only; polled while approval is on so a request shows up unasked. */
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
