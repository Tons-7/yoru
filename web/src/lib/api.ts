export const TOKEN_KEY = 'yoru.token'

function readToken(): string {
  const params = new URLSearchParams(window.location.search)
  const fromUrl = params.get('token')
  if (fromUrl) {
    try {
      localStorage.setItem(TOKEN_KEY, fromUrl)
    } catch {}
    params.delete('token')
    const rest = params.toString()
    window.history.replaceState(null, '', `${window.location.pathname}${rest ? `?${rest}` : ''}${window.location.hash}`)
    return fromUrl
  }
  try {
    return localStorage.getItem(TOKEN_KEY) ?? ''
  } catch {
    return ''
  }
}

export const UNPAIRED_EVENT = 'yoru:unpaired'

export class ApiError extends Error {
  readonly status: number
  readonly body?: unknown

  constructor(status: number, message: string, body?: unknown) {
    super(message)
    this.status = status
    this.body = body
  }
}

let savedAt: number | null = null
const savedListeners = new Set<() => void>()

function noteSource(res: Response) {
  const saved = res.headers.get('X-Yoru-Saved')
  const next = saved ? Number(saved) : res.headers.has('X-Yoru-Live') ? null : savedAt
  if (next === savedAt) return
  savedAt = next
  savedListeners.forEach((l) => l())
}

export const catalogueSource = {
  subscribe(listener: () => void) {
    savedListeners.add(listener)
    return () => savedListeners.delete(listener)
  },
  savedAt: () => savedAt,
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  if (init?.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')

  const res = await fetchAuthed(path, { ...init, headers })
  noteSource(res)

  if (!res.ok) {
    let body: unknown
    let message = `${res.status} ${res.statusText}`
    try {
      body = await res.json()
      const err = (body as { error?: string })?.error
      if (err) message = err
      if (res.status === 403 && (body as { approval?: string })?.approval) window.location.reload()
    } catch {}
    throw new ApiError(res.status, message, body)
  }

  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

export const api = {
  get: <T>(path: string, signal?: AbortSignal) => request<T>(path, { signal }),
  post: <T>(path: string, body?: unknown, signal?: AbortSignal) =>
    request<T>(path, { method: 'POST', body: body ? JSON.stringify(body) : undefined, signal }),
  put: <T>(path: string, body: unknown) => request<T>(path, { method: 'PUT', body: JSON.stringify(body) }),
  del: <T>(path: string) => request<T>(path, { method: 'DELETE' }),
  upload: <T>(path: string, file: File) =>
    request<T>(path, {
      method: 'POST',
      body: file,
      headers: { 'Content-Type': file.type || 'application/octet-stream' },
    }),
}

let keptAlive = 0
const KEEPALIVE_BUDGET = 48_000

export async function fetchAuthed(path: string, init?: RequestInit): Promise<Response> {
  const token = readToken()
  const headers = new Headers(init?.headers)
  if (token) headers.set('Authorization', `Bearer ${token}`)
  const body = init?.body
  const size = typeof body === 'string' ? new Blob([body]).size : 0
  const leaving =
    init?.keepalive === undefined &&
    document.visibilityState === 'hidden' &&
    !!init?.method &&
    init.method !== 'GET' &&
    (body == null || typeof body === 'string') &&
    keptAlive + size <= KEEPALIVE_BUDGET
  if (leaving) keptAlive += size
  try {
    const res = await fetch(path, { ...init, headers, credentials: 'same-origin', keepalive: init?.keepalive ?? leaving })
    if (res.status === 401) window.dispatchEvent(new Event(UNPAIRED_EVENT))
    return res
  } finally {
    if (leaving) keptAlive -= size
  }
}

export function withToken(path: string): string {
  const token = readToken()
  if (!token) return path
  return `${path}${path.includes('?') ? '&' : '?'}token=${encodeURIComponent(token)}`
}

export function beacon(path: string, body: unknown) {
  const url = withToken(path)
  const blob = new Blob([JSON.stringify(body)], { type: 'application/json' })
  if (!navigator.sendBeacon(url, blob)) {
    void api.post(path, body).catch(() => {})
  }
}

export function query(params: Record<string, string | number | boolean | undefined | null>) {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === '') continue
    search.set(key, String(value))
  }
  const s = search.toString()
  return s ? `?${s}` : ''
}


export interface Page<T> {
  items: T[]
  total: number
  page: number
  perPage: number
  hasMore: boolean
}

export interface Resume {
  epKey: string
  episode: number | null
  position: number
  duration: number | null
  watched: boolean
  lastPlayed?: number
}

export interface UpNextItem {
  animeId: number
  episode: number
  title: string
  cover?: string
  episodeTitle?: string
  minutes?: number
  position?: number
  duration?: number
  spoiler?: boolean
}

export interface LibraryItem {
  id: number
  title: string
  romaji: string
  english: string | null
  cover: string | null
  color: string | null
  episodes: number | null
  status: string | null
  progress: number
  score: number
  nextEpisode: number | null
  nextAiringAt: number | null
  format?: string | null
  seasonYear?: number | null
  airing?: string | null
  favourite?: boolean
  resume?: Resume
}

export interface DiscoverItem {
  id: number
  title: string
  romaji: string
  english?: string
  cover?: string
  coverLarge?: string
  thumb?: string
  banner?: string
  color?: string
  format?: string
  status?: string
  episodes?: number
  season?: string
  seasonYear?: number
  score?: number
  popularity?: number
  Genres?: string[]
  description?: string
  onList: boolean
  progress: number
  listStatus?: string | null
  alertOn?: boolean
  malId?: number
  nextEpisode?: number
  nextAiringAt?: number
  startDate?: string
  endDate?: string
  duration?: number
  studios?: { id: number; name: string }[]
}

export interface ScheduleItem {
  animeId: number
  episode: number
  airingAt: number
  title: string
  romaji: string
  english?: string
  cover?: string
  coverLarge?: string
  thumb?: string
  colour?: string
  format?: string
  onList: boolean
  alerts: boolean
  progress: number
  behind: number
  watched: boolean
  out?: boolean
  watch?: boolean
  checkedAt?: number
  position?: number
  duration?: number
  delayedFrom?: number
  jstWeekday: string
  show?: DiscoverItem
}

export interface AlertShow {
  animeId: number
  title: string
  cover?: string
  status?: string
  nextEpisode?: number
  nextAiringAt?: number
  because: 'followed' | 'watching' | 'auto'
}

export interface ScheduleDay {
  date: string
  weekday: string
  today: boolean
  items: ScheduleItem[]
}

export interface Episode {
  animeId: number
  epKey: string
  number: number
  display: number
  absolute?: number
  titleEn?: string
  titleJa?: string
  overview?: string
  still?: string
  airDate?: number
  runtime?: number
  planned?: boolean
  filler?: string
  recap: boolean
  watched: boolean
  position?: number
  duration?: number
  resumable?: boolean
  onDisk: boolean
  local?: boolean
  spoiler?: boolean
  score?: number
  forumUrl?: string
}

export function hasFiller(episode: Episode) {
  return episode.filler === 'filler' || episode.filler === 'mixed'
}

export interface PlaySession {
  animeId: number
  episode: number
  title: string
  source: 'local' | 'torrent'
  infoHash?: string
  torrentId?: number
  fileIndex: number
  streamUrl: string
  localPath?: string
  startAt: number
  player: string
  size?: number
}

export interface ReleaseCandidate {
  Torrent: {
    title: string
    infoHash: string
    size: number
    seeders: number
    seedersKnown: boolean
    leechers: number
    trusted?: boolean
    indexer?: string
  }
  Release: {
    Group?: string
    Resolution?: string
    Source?: string
    Codec?: string
    BitDepth?: number
    DualAudio?: boolean
    Batch?: boolean
    Episode?: number
    EpisodeEnd?: number
    Subtitles?: string[]
  }
  SeaDexBest?: boolean
  SeaDexListed?: boolean
  SeaDexGroup?: boolean
  SeaDexNotes?: string
  SeaDexCompare?: string[]
  SeaDexIncomplete?: boolean
  Confirmed?: boolean
  score: number
  reasons?: string[]
  playable: boolean
  autoPick: boolean
  blocked?: string
}

export interface FollowState {
  on: boolean
  because?: 'followed' | 'watching' | 'auto'
  wanted?: number
}

export interface Bookmark {
  favourite: boolean
  pinned: boolean
  hidden: boolean
  note: string
}

export interface SubtitleTrack {
  index: number
  title?: string
  language?: string
  url: string
  default?: boolean
  pictures?: boolean
  whole?: boolean
}

export interface AudioTrack {
  index: number
  codec?: string
  channels?: number
  language?: string
  title?: string
  default?: boolean
}

export interface DownloadFile {
  fileIndex: number
  epKey: string
  kept: boolean
  complete: boolean
}

export interface StreamHealth {
  mbps: number
  needed: number
  peers: number
  complete: boolean
  slow: boolean
  speed?: number
  percent?: number
}

export type VoiceLevel = 'off' | 'medium' | 'high'
export const VOICE_LEVELS: VoiceLevel[] = ['off', 'medium', 'high']

export interface SoundOptions {
  voice: VoiceLevel
  night: boolean
  headphones: boolean
  mono: boolean
  boost: VoiceLevel
}
export const DEFAULT_SOUND: SoundOptions = { voice: 'off', night: false, headphones: false, mono: false, boost: 'off' }
export const SOUND_PREFS: Record<keyof SoundOptions, string> = {
  voice: 'playback.voice', night: 'playback.night', headphones: 'playback.headphones', mono: 'playback.mono', boost: 'playback.boost',
}
export function soundFrom(raw?: Partial<SoundOptions> | null): SoundOptions {
  return {
    voice: raw?.voice || 'off', night: !!raw?.night, headphones: !!raw?.headphones, mono: !!raw?.mono, boost: raw?.boost || 'off',
  }
}
export function sameSound(a: SoundOptions, b: SoundOptions): boolean {
  return a.voice === b.voice && a.night === b.night && a.headphones === b.headphones && a.mono === b.mono && a.boost === b.boost
}

export interface StreamInfo {
  id: string
  playlist: string
  duration: number
  plan?: {
    videoCopy?: boolean
    audioCopy?: boolean
    videoCodec?: string
    audioCodec?: string
    reason?: string
    channels?: number
    forDevice?: boolean
    toneMap?: boolean
    deinterlace?: boolean
    soften?: boolean
    height?: number
    maxRate?: number
  }
  video?: {
    codec?: string
    profile?: string
    width?: number
    height?: number
    bitDepth?: number
    transfer?: string
    fieldOrder?: string
    dvProfile?: number
  }
  audio?: AudioTrack[]
  audioTrack?: number
  sound?: Partial<SoundOptions>
  subtitles: SubtitleTrack[]
  fonts: string[]
  fontsUrl?: string
  imageSubtitles?: boolean
  airplay?: string
  saved?: boolean
  complete?: boolean
}

export interface SkipRange {
  kind: string
  start: number
  end: number
  source?: string
  id?: string
}

export interface SkipMarkResult {
  start: number
  end: number
  snapped: '' | 'chapters' | 'sound'
}

export const LIST_STATUSES = [
  { value: 'CURRENT', label: 'Watching' },
  { value: 'COMPLETED', label: 'Completed' },
  { value: 'PAUSED', label: 'On hold' },
  { value: 'DROPPED', label: 'Dropped' },
  { value: 'PLANNING', label: 'Plan to watch' },
  { value: 'REPEATING', label: 'Rewatching' },
] as const

export type ListStatus = (typeof LIST_STATUSES)[number]['value']

export interface Tracker {
  provider: string
  name: string
  clientId: string
  hasSecret: boolean
  configured: boolean
  connected: boolean
  reconnect?: boolean
  user?: string
  redirect: string
  register: string
  secretRequired: boolean
  tokenUrl?: string
}

export function statusLabel(value?: string | null) {
  return LIST_STATUSES.find((s) => s.value === value)?.label ?? ''
}
