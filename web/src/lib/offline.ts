import { useSyncExternalStore } from 'react'
import { deviceCaps } from '../player/decode'
import { api, ApiError, fetchAuthed, query, type Episode, type PlaySession, type SkipRange, type StreamInfo, type SubtitleTrack } from './api'
import { bytes } from './format'
import { registerWorker } from './push'
import type { Download, Queued } from './queries'

// Saved episodes: the stream the PC would send this device, kept piece by piece; public/sw.js serves them.

export const SAVED_CACHE = 'yoru-saved'
const SHELL_PREFIX = 'yoru-shell-'
const JOBS_KEY = 'yoru.saving'
const WATCH_KEY = 'yoru.saved.watch'
const QUALITY_KEY = 'yoru.saved.quality'

export type SaveQuality = 'original' | '720'

export interface SavedEpisode {
  key: string
  animeId: number
  episode: number
  title: string
  label: string
  cover?: string
  quality: SaveQuality
  stream: StreamInfo
  skips: SkipRange[]
  /** The show's settings when saved, read by the player offline. */
  prefs: Record<string, string>
  bookend: boolean
  startAt: number
  bytes: number
  savedAt: number
}

export interface Job {
  animeId: number
  episode: number
  quality: SaveQuality
}

export interface SaveStatus {
  phase: 'queued' | 'waiting' | 'preparing' | 'saving' | 'stopped' | 'failed'
  done: number
  total: number
  bytes: number
  /** The PC's download, while waiting for it. */
  percent?: number
  message?: string
}

export const savedKey = (animeId: number, episode: number) => `${animeId}-${episode}`
const keyOf = (j: { animeId: number; episode: number }) => savedKey(j.animeId, j.episode)
const savedBase = (key: string) => `/api/stream/saved-${key}`

export function cannotSave(): string | null {
  if (!window.isSecureContext) return "Saving needs Yoru's secure address (HTTPS)"
  if (!('caches' in window) || !('serviceWorker' in navigator)) return "This browser can't keep episodes"
  return null
}

function read<T>(key: string, fallback: T): T {
  try {
    return (JSON.parse(localStorage.getItem(key) ?? 'null') as T | null) ?? fallback
  } catch {
    return fallback
  }
}

function write(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value))
  } catch {
    // This visit only.
  }
}

export function saveQuality(): SaveQuality {
  return read<SaveQuality>(QUALITY_KEY, 'original') === '720' ? '720' : 'original'
}

export function setSaveQuality(q: SaveQuality) {
  write(QUALITY_KEY, q)
  emit()
}

let saved: SavedEpisode[] | null = null
const status = new Map<string, SaveStatus>()
const listeners = new Set<() => void>()
const current = () => ({
  saved: saved ?? [],
  jobs: read<Job[]>(JOBS_KEY, []),
  status: new Map(status),
  loaded: saved !== null,
  quality: saveQuality(),
})
let snapshot = current()

function emit() {
  snapshot = current()
  listeners.forEach((l) => l())
}

function setStatus(key: string, s: SaveStatus | null) {
  if (s) status.set(key, s)
  else status.delete(key)
  emit()
}

export function useSaved() {
  return useSyncExternalStore((l) => {
    listeners.add(l)
    if (saved === null) void refreshSaved()
    return () => listeners.delete(l)
  }, () => snapshot)
}

async function refreshSaved() {
  try {
    if (cannotSave()) throw new Error()
    const cache = await caches.open(SAVED_CACHE)
    const infos = (await cache.keys()).filter((r) => r.url.endsWith('/info.json'))
    const found = await Promise.all(infos.map((r) => cache.match(r).then((res) => res?.json() as Promise<SavedEpisode> | undefined)))
    saved = found
      .filter((e): e is SavedEpisode => !!e)
      .sort((a, b) => a.title.localeCompare(b.title) || a.episode - b.episode)
  } catch {
    saved ??= []
  }
  emit()
}

const retryAt = new Map<string, number>()
const restarts = new Map<string, number>()
const reask = new Set<string>()
const cancelled = new Set<string>()
let pumping = false
let timer = 0

class SaveFailed extends Error {}
class Cancelled extends Error {}

export function saveEpisodes(episodes: Array<{ animeId: number; episode: number }>, quality = saveQuality()) {
  const jobs = read<Job[]>(JOBS_KEY, [])
  const have = new Set([...jobs.map(keyOf), ...(saved ?? []).map((e) => e.key)])
  for (const e of episodes) {
    const key = keyOf(e)
    if (have.has(key)) continue
    have.add(key)
    jobs.push({ animeId: e.animeId, episode: e.episode, quality })
    cancelled.delete(key)
    setStatus(key, { phase: 'queued', done: 0, total: 0, bytes: 0 })
  }
  write(JOBS_KEY, jobs)
  // Otherwise the browser may clear it when it wants room.
  void navigator.storage?.persist?.().catch(() => false)
  void registerWorker()?.catch(() => {})
  void pump()
}

export function retrySave(key: string) {
  retryAt.delete(key)
  reask.add(key)
  setStatus(key, { phase: 'queued', done: 0, total: 0, bytes: 0 })
  void pump()
}

function dropJob(key: string) {
  write(JOBS_KEY, read<Job[]>(JOBS_KEY, []).filter((j) => keyOf(j) !== key))
  retryAt.delete(key)
}

/** Deletes a saved episode or stops its save; what was watched still goes to Yoru. */
export async function removeSaved(key: string) {
  cancelled.add(key)
  dropJob(key)
  setStatus(key, null)
  await removeFiles(key)
  await refreshSaved()
  // With nothing saved, a dead network shows the "can't reach" page again.
  if ((saved ?? []).length === 0 && read<Job[]>(JOBS_KEY, []).length === 0) {
    for (const name of (await caches.keys()).filter((k) => k.startsWith(SHELL_PREFIX))) await caches.delete(name)
  }
}

async function removeFiles(key: string) {
  const cache = await caches.open(SAVED_CACHE)
  const prefix = `${location.origin}${savedBase(key)}/`
  for (const req of await cache.keys()) if (req.url.startsWith(prefix)) await cache.delete(req)
}

let started = false

/** Carries on earlier saves and sends what was watched, once Yoru lets this device in. */
export function resumeSaving() {
  if (started || cannotSave()) return
  started = true
  window.addEventListener('online', () => void sendWatched())
  window.setInterval(() => void sendWatched(), 30_000)
  const begin = () =>
    api.get('/api/access').then(
      () => {
        void refreshSaved().then(() => {
          if ((saved ?? []).length === 0) return
          // Registered again so an updated worker takes over.
          void registerWorker()?.catch(() => {})
          void keepApp()
        })
        for (const job of read<Job[]>(JOBS_KEY, [])) {
          if (!status.has(keyOf(job))) setStatus(keyOf(job), { phase: 'queued', done: 0, total: 0, bytes: 0 })
        }
        void pump()
        void sendWatched()
      },
      () => window.setTimeout(() => void begin(), 30_000),
    )
  void begin()
}

async function pump() {
  if (pumping) return
  pumping = true
  let awake: WakeLockSentinel | null = null
  try {
    for (;;) {
      const now = Date.now()
      const job = read<Job[]>(JOBS_KEY, []).find((j) => (retryAt.get(keyOf(j)) ?? 0) <= now)
      if (!job) break
      // A phone asleep stops the page.
      awake ??= await navigator.wakeLock?.request('screen').catch(() => null)
      await run(job)
    }
  } finally {
    pumping = false
    void awake?.release().catch(() => {})
    const next = Math.min(...retryAt.values())
    window.clearTimeout(timer)
    if (Number.isFinite(next)) timer = window.setTimeout(() => void pump(), Math.max(1000, next - Date.now()))
  }
}

interface ShowCard {
  title: string
  status?: string
  episodes?: number
  coverImage?: { large?: string; extraLarge?: string }
  cover?: string
}

async function run(job: Job) {
  const key = keyOf(job)
  const at = savedBase(key)
  let streamId: string | undefined
  setStatus(key, { phase: 'preparing', done: 0, total: 0, bytes: 0 })
  try {
    const [episodes, show, prefs, ready] = await Promise.all([
      api.get<{ items: Episode[] }>(`/api/episodes${query({ id: job.animeId })}`),
      api.get<ShowCard>(`/api/anime/${job.animeId}`),
      api.get<{ effective: Record<string, string> }>(`/api/prefs${query({ anime: job.animeId })}`),
      api.get<{ ready: boolean; downloading?: boolean }>(`/api/saved/ready${query({ anime: job.animeId, episode: job.episode })}`),
    ])
    const ep = episodes.items.find((e) => e.number === job.episode)
    if (!ep || !ready.ready) return await waitForPC(job, !!ready.downloading)

    const effective = prefs.effective
    const play = await api.post<PlaySession>('/api/play', { animeId: job.animeId, episode: job.episode })
    const stream = await api.post<StreamInfo>(
      `/api/stream/open${query({
        id: job.animeId, episode: job.episode, source: play.streamUrl, audio: effective['audio.prefer'] || 'sub',
        caps: await deviceCaps(false), keep: 1, quality: job.quality === '720' ? '720' : undefined,
        voice: effective['playback.voice'] || 'off', night: effective['playback.night'] === 'true',
        headphones: effective['playback.headphones'] === 'true', mono: effective['playback.mono'] === 'true',
        boost: effective['playback.boost'] || 'off',
      })}`,
    )
    streamId = stream.id
    if (!stream.complete) return await waitForPC(job, true)

    const cache = await caches.open(SAVED_CACHE)
    // Pieces from an earlier try fit only if the PC makes the copy the same way again.
    const shape = JSON.stringify([stream.plan, stream.duration, stream.audioTrack, job.quality])
    const begun = await cache.match(`${at}/shape`)
    if (begun && (await begun.text()) !== shape) await removeFiles(key)
    await cache.put(`${at}/shape`, new Response(shape))

    const from = `/api/stream/${stream.id}`
    const playlist = await (await fetchOk(stream.playlist)).text()
    const pieces = ['init.mp4', ...[...playlist.matchAll(/^(\d+\.mp4)$/gm)].map((m) => m[1])]
    await cache.put(`${at}/playlist.m3u8`, new Response(playlist, { headers: { 'Content-Type': 'application/vnd.apple.mpegurl' } }))
    const extras = keepExtras(cache, key, stream).then((ok) => ({ ok }), (err: unknown) => ({ err }))
    void api.get(`${from}/thumbnails`).catch(() => {})

    let size = 0
    let done = 0
    let next = 0
    let fresh = 0
    let freshBytes = 0
    let failed: unknown = null
    const worker = async () => {
      try {
        while (next < pieces.length && !failed) {
          if (cancelled.has(key)) throw new Cancelled()
          const piece = pieces[next++]
          const url = `${at}/${piece}`
          const have = await cache.match(url)
          if (have) {
            size += Number(have.headers.get('Content-Length')) || 0
          } else {
            const blob = await (await fetchOk(`${from}/${piece}`)).blob()
            await keep(cache, url, blob, 'video/mp4')
            size += blob.size
            fresh++
            freshBytes += blob.size
            if (fresh === 4) await checkRoom((freshBytes / fresh) * (pieces.length - done - 1))
          }
          done++
          if (!failed) setStatus(key, { phase: 'saving', done, total: pieces.length, bytes: size })
        }
      } catch (e) {
        failed ??= e
      }
    }
    await Promise.all([worker(), worker(), worker(), worker()])
    if (failed) throw failed
    const extra = await extras
    if ('err' in extra) throw extra.err
    const { subtitles } = extra.ok
    size += extra.ok.size

    await keepJSON(cache, `${at}/loaded`, { complete: true, ranges: [[0, stream.duration]] })
    for (let i = 0; i < 5; i++) {
      const thumbs = await api.get<{ ready?: boolean; partial?: boolean }>(`${from}/thumbnails`).catch(() => null)
      if (thumbs?.ready && !thumbs.partial) {
        await keep(cache, `${at}/thumbs.jpg`, await (await fetchOk(`${from}/thumbs.jpg`)).blob(), 'image/jpeg')
        await keepJSON(cache, `${at}/thumbnails`, thumbs)
        break
      }
      await new Promise((r) => setTimeout(r, 2000))
    }
    const skips = await api
      .get<{ ranges: SkipRange[] }>(`/api/episode/skips${query({ anime: job.animeId, episode: job.episode, duration: Math.round(stream.duration) })}`)
      .then((r) => r.ranges, () => [])
    let cover: string | undefined
    const art = show.coverImage?.large ?? show.coverImage?.extraLarge ?? show.cover
    if (art) {
      try {
        const res = await fetch(art, { mode: 'cors' })
        if (res.ok) {
          await keep(cache, `${at}/cover`, await res.blob(), res.headers.get('Content-Type') ?? 'image/jpeg')
          cover = `${at}/cover`
        }
      } catch {
        // Listed without its picture.
      }
    }

    const info: SavedEpisode = {
      key, animeId: job.animeId, episode: job.episode, title: show.title,
      label: [`Episode ${ep.display}`, ep.titleEn].filter(Boolean).join(' · '),
      cover, quality: job.quality, skips, prefs: effective, startAt: play.startAt, bytes: size, savedAt: Date.now(),
      bookend: job.episode === 1 || (show.status === 'FINISHED' && !!show.episodes && job.episode === show.episodes),
      stream: {
        ...stream, id: `saved-${key}`, playlist: `${at}/playlist.m3u8`, subtitles, fonts: [], airplay: undefined, saved: true,
        fontsUrl: stream.fontsUrl ? `${at}/fonts` : undefined,
      },
    }
    if (cancelled.has(key)) throw new Cancelled()
    // Last, so only a whole episode is listed.
    await keepJSON(cache, `${at}/info.json`, info)
    dropJob(key)
    restarts.delete(key)
    setStatus(key, null)
    await refreshSaved()
    void keepApp()
  } catch (e) {
    if (e instanceof Cancelled) return await removeFiles(key)
    const was = status.get(key) ?? { done: 0, total: 0, bytes: 0 }
    if (e instanceof ApiError && e.status === 404 && (restarts.get(key) ?? 0) < 3) {
      restarts.set(key, (restarts.get(key) ?? 0) + 1)
      retryAt.set(key, Date.now() + 5000)
      setStatus(key, { ...was, phase: 'stopped', message: 'Starting again' })
      return
    }
    if (e instanceof SaveFailed || e instanceof ApiError || (e instanceof DOMException && e.name === 'QuotaExceededError')) {
      retryAt.set(key, Infinity)
      setStatus(key, { ...was, phase: 'failed', message: e instanceof DOMException ? 'This device ran out of room for it' : (e as Error).message })
      return
    }
    retryAt.set(key, Date.now() + 30_000)
    setStatus(key, { ...was, phase: 'stopped', message: "Can't reach Yoru" })
  } finally {
    if (streamId) void api.del(`/api/stream/${streamId}`).catch(() => {})
  }
}

async function waitForPC(job: Job, downloading: boolean) {
  const key = keyOf(job)
  const [downloads, queue] = await Promise.all([
    api.get<{ items: Download[] }>('/api/downloads'),
    api.get<{ items: Queued[] }>('/api/download/queue'),
  ])
  const item = downloads.items.find((d) => d.animeId === job.animeId && d.episodes.includes(String(job.episode)))
  const queued = queue.items.find((q) => q.animeId === job.animeId && q.episode === job.episode)
  const retry = reask.delete(key)
  if (queued?.state === 'failed' && !retry) {
    throw new SaveFailed(`The PC couldn't download it: ${queued.error ?? 'no release found'}`)
  }
  if (!downloading && !item && (!queued || queued.state === 'failed')) {
    await api.post('/api/download', { animeId: job.animeId, episode: job.episode })
  }
  retryAt.set(key, Date.now() + 20_000)
  setStatus(key, { phase: 'waiting', done: 0, total: 0, bytes: 0, percent: item?.percent })
}

async function keepExtras(cache: Cache, key: string, stream: StreamInfo) {
  const at = savedBase(key)
  let size = 0
  const kept = new Map<number, SubtitleTrack>()
  const queue = [...stream.subtitles]
  // Two at a time: each track is read out of the whole file on the PC.
  await Promise.all(
    [0, 1].map(async () => {
      for (let track = queue.shift(); track; track = queue.shift()) {
        if (cancelled.has(key)) throw new Cancelled()
        const path = `${at}/${track.pictures ? 'pgs' : 'subtitle'}/${track.index}`
        const blob = await (await fetchOk(track.url)).blob()
        await keep(cache, path, blob, track.pictures ? 'application/octet-stream' : 'text/plain; charset=utf-8')
        size += blob.size
        kept.set(track.index, { ...track, url: path, whole: true })
      }
    }),
  )
  const fonts: Array<{ url: string }> = []
  for (const font of stream.fontsUrl ? await extractedFonts(stream.fontsUrl) : []) {
    if (cancelled.has(key)) throw new Cancelled()
    const path = `${at}/font/${font.split('/').pop()}`
    const blob = await (await fetchOk(font)).blob()
    await keep(cache, path, blob, 'font/ttf')
    size += blob.size
    fonts.push({ url: path })
  }
  await keepJSON(cache, `${at}/fonts`, { ready: true, fonts })
  return { subtitles: stream.subtitles.map((t) => kept.get(t.index)!), size }
}

async function fetchOk(path: string): Promise<Response> {
  for (let attempt = 1; ; attempt++) {
    const res = await fetchAuthed(path)
    if (res.ok) return res
    if (attempt >= 3 || res.status < 500) {
      const body = (await res.json().catch(() => ({}))) as { error?: string }
      throw new ApiError(res.status, body.error ?? `The PC answered ${res.status}`, body)
    }
    await new Promise((r) => setTimeout(r, 1000 * attempt))
  }
}

async function extractedFonts(url: string): Promise<string[]> {
  for (let i = 0; i < 60; i++) {
    const body = await api.get<{ ready: boolean; fonts: Array<{ url: string }> }>(url).catch(() => null)
    if (body?.ready) return body.fonts.map((f) => f.url)
    await new Promise((r) => setTimeout(r, 1000))
  }
  return []
}

async function checkRoom(more: number) {
  const est = await navigator.storage?.estimate?.().catch(() => undefined)
  if (!est?.quota) return
  const free = est.quota - (est.usage ?? 0)
  if (more > free) throw new SaveFailed(`Needs about ${bytes(more)} more; Yoru has ${bytes(free)} left on this device`)
}

const keep = (cache: Cache, url: string, blob: Blob, type: string) =>
  cache.put(url, new Response(blob, { headers: { 'Content-Type': type, 'Content-Length': String(blob.size) } }))

const keepJSON = (cache: Cache, url: string, value: unknown) =>
  cache.put(url, new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } }))

// The app's own files, one whole copy per build, so Yoru opens here with the PC off.
async function keepApp() {
  try {
    const { version, files } = await api.get<{ version: string; files: string[] }>('/api/app/files')
    const name = SHELL_PREFIX + version
    const others = (await caches.keys()).filter((k) => k.startsWith(SHELL_PREFIX) && k !== name)
    const cache = await caches.open(name)
    if (!(await cache.match('/__ready'))) {
      await cache.addAll(files)
      await cache.put('/__ready', new Response(version))
    }
    for (const old of others) await caches.delete(old)
  } catch {
    // Tried again next start.
  }
}

interface Watched {
  animeId: number
  episode: number
  position: number
  duration: number
  /** Since last sent. */
  played: number
  at: number
  dirty: boolean
}

const watchedListeners = new Set<() => void>()
let watchedSnapshot = read<Record<string, Watched>>(WATCH_KEY, {})

function writeWatched(all: Record<string, Watched>) {
  write(WATCH_KEY, all)
  watchedSnapshot = all
  watchedListeners.forEach((l) => l())
}

export function useWatched() {
  return useSyncExternalStore((l) => {
    watchedListeners.add(l)
    return () => watchedListeners.delete(l)
  }, () => watchedSnapshot)
}

export function noteWatched(e: SavedEpisode, position: number, duration: number, played: number, final: boolean) {
  const all = { ...read<Record<string, Watched>>(WATCH_KEY, {}) }
  const was = all[e.key]
  all[e.key] = { animeId: e.animeId, episode: e.episode, position, duration, played: (was?.played ?? 0) + played, at: Date.now(), dirty: true }
  writeWatched(all)
  if (final) void sendWatched()
}

let sending = false

export async function sendWatched() {
  const all = read<Record<string, Watched>>(WATCH_KEY, {})
  const items = Object.entries(all).filter(([, w]) => w.dirty)
  if (sending || items.length === 0) return
  sending = true
  try {
    const res = await api.post<{ failed?: Array<{ animeId: number; episode: number }> }>('/api/saved/progress', {
      now: Date.now(),
      items: items.map(([, w]) => w),
    })
    const again = new Set((res.failed ?? []).map((f) => savedKey(f.animeId, f.episode)))
    const now = { ...read<Record<string, Watched>>(WATCH_KEY, {}) }
    for (const [key, w] of items) {
      const cur = now[key]
      if (!cur || again.has(key)) continue
      // Watched on while sending: only the time sent is taken off.
      now[key] = cur.at === w.at ? { ...cur, played: 0, dirty: false } : { ...cur, played: Math.max(0, cur.played - w.played) }
    }
    writeWatched(now)
  } catch {
    // Sent next time.
  } finally {
    sending = false
  }
}

export function resumeAt(e: SavedEpisode, watched: Record<string, Watched>): number {
  const w = watched[e.key]
  if (!w) return e.startAt
  return w.duration > 0 && w.position / w.duration >= 0.9 ? 0 : w.position
}

export function watchedThrough(e: SavedEpisode, watched: Record<string, Watched>): number {
  const w = watched[e.key]
  return w && w.duration > 0 ? Math.min(1, w.position / w.duration) : 0
}
