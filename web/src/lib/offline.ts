import { useSyncExternalStore } from 'react'
import { deviceCaps } from '../player/decode'
import { cellular } from '../player/network'
import { api, ApiError, fetchAuthed, query, type Episode, type PlaySession, type SkipRange, type StreamInfo, type SubtitleTrack } from './api'
import { isUnaired } from './episodes'
import { bytes } from './format'
import { registerWorker } from './push'
import type { Download, Queued } from './queries'
import { aboutLeft, ordinal } from './shelf'

export const SAVED_CACHE = 'yoru-saved'
const SHELL_PREFIX = 'yoru-shell-'
const JOBS_KEY = 'yoru.saving'
const WATCH_KEY = 'yoru.saved.watch'
const QUALITY_KEY = 'yoru.saved.quality'
const RULE_KEYS = { next: 'yoru.saved.next', tidy: 'yoru.saved.tidy', wifi: 'yoru.saved.wifi' } as const
const DAY = 86_400_000

export type SaveQuality = 'original' | '720'

export interface SaveRules {
  next: boolean
  tidy: boolean
  wifi: boolean
}

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
  paused?: boolean
  auto?: boolean
  failed?: string
  reached?: { done: number; total: number; bytes: number }
}

export interface SaveStatus {
  phase: 'queued' | 'waiting' | 'preparing' | 'saving' | 'stopped' | 'failed' | 'paused'
  done: number
  total: number
  bytes: number
  percent?: number
  message?: string
  rate?: number
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
  }
}

export function saveQuality(): SaveQuality {
  return read<SaveQuality>(QUALITY_KEY, 'original') === '720' ? '720' : 'original'
}

export function setSaveQuality(q: SaveQuality) {
  write(QUALITY_KEY, q)
  emit()
}

export function saveRules(): SaveRules {
  return {
    next: read<boolean>(RULE_KEYS.next, false) === true,
    tidy: read<boolean>(RULE_KEYS.tidy, false) === true,
    wifi: read<boolean>(RULE_KEYS.wifi, false) === true,
  }
}

export function setSaveRule(rule: keyof SaveRules, on: boolean) {
  write(RULE_KEYS[rule], on)
  emit()
  if (rule === 'tidy' && on) void tidySaved()
  if (rule === 'wifi') wakeForWifi()
}

function wakeForWifi() {
  for (const [key, s] of status) if (s.phase === 'stopped' && s.message === WIFI_WAIT) retryAt.delete(key)
  void pump()
}

const WIFI_WAIT = 'Waits for Wi-Fi'

export const knowsNetwork = () => cellular() !== null

const onMobileData = () => saveRules().wifi && cellular() === true

let saved: SavedEpisode[] | null = null
const status = new Map<string, SaveStatus>()
const listeners = new Set<() => void>()
const current = () => ({
  saved: saved ?? [],
  jobs: read<Job[]>(JOBS_KEY, []),
  status: new Map(status),
  loaded: saved !== null,
  quality: saveQuality(),
  rules: saveRules(),
})
let snapshot = current()

function emit() {
  snapshot = current()
  listeners.forEach((l) => l())
}

let emitTimer = 0

function setStatus(key: string, s: SaveStatus | null) {
  const was = status.get(key)
  if (s) status.set(key, s)
  else status.delete(key)
  if (s?.phase === 'saving' && was?.phase === 'saving') {
    if (!emitTimer) {
      emitTimer = window.setTimeout(() => {
        emitTimer = 0
        emit()
      }, 250)
    }
    return
  }
  window.clearTimeout(emitTimer)
  emitTimer = 0
  emit()
}

let firstScan: Promise<void> | null = null

const subscribeSaved = (l: () => void) => {
  listeners.add(l)
  if (saved === null) firstScan ??= refreshSaved()
  return () => {
    listeners.delete(l)
  }
}
const savedSnapshot = () => snapshot

export function useSaved() {
  return useSyncExternalStore(subscribeSaved, savedSnapshot)
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
const halted = new Set<string>()
let pumping = false
let timer = 0

class SaveFailed extends Error {}
class Cancelled extends Error {}
class Held extends Error {}
class Paused extends Error {}

export function saveEpisodes(episodes: Array<{ animeId: number; episode: number }>, quality = saveQuality(), auto = false) {
  if (saved === null) {
    void (firstScan ??= refreshSaved()).then(() => saveEpisodes(episodes, quality, auto))
    return
  }
  const jobs = read<Job[]>(JOBS_KEY, [])
  const have = new Set([...jobs.map(keyOf), ...(saved ?? []).map((e) => e.key)])
  for (const e of episodes) {
    const key = keyOf(e)
    if (have.has(key)) continue
    have.add(key)
    jobs.push({ animeId: e.animeId, episode: e.episode, quality, ...(auto && { auto }) })
    cancelled.delete(key)
    setStatus(key, { phase: 'queued', done: 0, total: 0, bytes: 0 })
  }
  write(JOBS_KEY, jobs)
  void navigator.storage?.persist?.().catch(() => false)
  void registerWorker()?.catch(() => {})
  void pump()
}

export function retrySave(key: string) {
  write(JOBS_KEY, read<Job[]>(JOBS_KEY, []).map((j) => (keyOf(j) === key ? { ...j, failed: undefined } : j)))
  retryAt.delete(key)
  reask.add(key)
  setStatus(key, { phase: 'queued', done: 0, total: 0, bytes: 0 })
  void pump()
}

function markJob(key: string, paused: boolean) {
  write(JOBS_KEY, read<Job[]>(JOBS_KEY, []).map((j) => (keyOf(j) === key ? { ...j, paused, reached: undefined } : j)))
}

export function pauseSave(key: string) {
  const was = status.get(key) ?? { done: 0, total: 0, bytes: 0 }
  write(
    JOBS_KEY,
    read<Job[]>(JOBS_KEY, []).map((j) =>
      keyOf(j) === key ? { ...j, paused: true, reached: { done: was.done, total: was.total, bytes: was.bytes } } : j,
    ),
  )
  halted.add(key)
  retryAt.delete(key)
  setStatus(key, { ...was, phase: 'paused', message: undefined })
}

export function resumeSave(key: string) {
  markJob(key, false)
  halted.delete(key)
  retryAt.delete(key)
  setStatus(key, { ...(status.get(key) ?? { done: 0, total: 0, bytes: 0 }), phase: 'queued', message: undefined })
  void pump()
}

export function saveNextAfter(animeId: number, episode: number) {
  if (!saveRules().next || cannotSave()) return
  const finished = (saved ?? []).find((e) => e.animeId === animeId && e.episode === episode)
  if (!finished) return
  saveEpisodes([{ animeId, episode: episode + 1 }], finished.quality, true)
}

export async function tidySaved() {
  if (!saveRules().tidy || cannotSave()) return
  const watched = read<Record<string, Watched>>(WATCH_KEY, {})
  const now = Date.now()
  const old = (saved ?? []).filter((e) => {
    const w = sinceSaved(e, watched)
    return !!w && w.duration > 0 && w.position / w.duration >= 0.9 && now - w.at >= DAY
  })
  await removeSavedMany(old.map((e) => e.key))
}

function dropJob(key: string) {
  write(JOBS_KEY, read<Job[]>(JOBS_KEY, []).filter((j) => keyOf(j) !== key))
  retryAt.delete(key)
  halted.delete(key)
}

export async function removeSaved(key: string) {
  await removeSavedMany([key])
}

export async function removeSavedMany(keys: string[]) {
  if (keys.length === 0) return
  for (const key of keys) {
    cancelled.add(key)
    dropJob(key)
    setStatus(key, null)
  }
  await removeFilesMany(keys)
  await refreshSaved()
  if ((saved ?? []).length === 0 && read<Job[]>(JOBS_KEY, []).length === 0) {
    await keepingApp
    for (const name of (await caches.keys()).filter((k) => k.startsWith(SHELL_PREFIX))) await caches.delete(name)
  }
}

async function removeFiles(key: string) {
  await removeFilesMany([key])
}

async function removeFilesMany(keys: string[]) {
  const cache = await caches.open(SAVED_CACHE)
  const prefixes = keys.map((key) => `${location.origin}${savedBase(key)}/`)
  const doomed = (await cache.keys()).filter((req) => prefixes.some((p) => req.url.startsWith(p)))
  await Promise.all(doomed.map((req) => cache.delete(req)))
}

let started = false

export function resumeSaving() {
  if (started || cannotSave()) return
  started = true
  window.addEventListener('online', () => void sendWatched())
  window.setInterval(() => void sendWatched(), 30_000)
  window.setInterval(() => void tidySaved(), 60 * 60_000)
  ;(navigator as Navigator & { connection?: EventTarget }).connection?.addEventListener('change', wakeForWifi)
  for (const job of read<Job[]>(JOBS_KEY, [])) {
    if (!job.paused) continue
    halted.add(keyOf(job))
    setStatus(keyOf(job), { phase: 'paused', ...(job.reached ?? { done: 0, total: 0, bytes: 0 }) })
  }
  const begin = () =>
    api.get('/api/access').then(
      () => {
        void refreshSaved().then(() => {
          if ((saved ?? []).length === 0) return
          void registerWorker()?.catch(() => {})
          void keepApp()
          void tidySaved()
        })
        for (const job of read<Job[]>(JOBS_KEY, [])) {
          if (status.has(keyOf(job))) continue
          if (job.failed) {
            retryAt.set(keyOf(job), Infinity)
            setStatus(keyOf(job), { phase: 'failed', done: 0, total: 0, bytes: 0, message: job.failed })
          } else setStatus(keyOf(job), { phase: 'queued', done: 0, total: 0, bytes: 0 })
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
  if (!navigator.locks) return drain()
  let ran = false
  await navigator.locks
    .request('yoru-saving', { ifAvailable: true }, async (lock) => {
      if (!lock) return
      ran = true
      await drain()
    })
    .catch(() => {})
  if (!ran && !pumping) {
    window.clearTimeout(timer)
    timer = window.setTimeout(() => void pump(), 30_000)
  }
}

async function drain() {
  if (pumping) return
  pumping = true
  let awake: WakeLockSentinel | null = null
  const relock = () => {
    if (document.visibilityState !== 'visible' || (awake && !awake.released)) return
    void navigator.wakeLock
      ?.request('screen')
      .then((lock) => {
        awake = lock
      })
      .catch(() => {})
  }
  document.addEventListener('visibilitychange', relock)
  try {
    for (;;) {
      const now = Date.now()
      const ready = read<Job[]>(JOBS_KEY, []).filter((j) => !j.paused && (retryAt.get(keyOf(j)) ?? 0) <= now)
      if (ready.length === 0) break
      if (onMobileData()) {
        for (const j of ready) {
          retryAt.set(keyOf(j), now + 60_000)
          setStatus(keyOf(j), { ...(status.get(keyOf(j)) ?? { done: 0, total: 0, bytes: 0 }), phase: 'stopped', message: WIFI_WAIT })
        }
        break
      }
      awake ??= await navigator.wakeLock?.request('screen').catch(() => null)
      await run(ready[0])
    }
  } finally {
    pumping = false
    document.removeEventListener('visibilitychange', relock)
    void awake?.release().catch(() => {})
    const next = Math.min(
      ...read<Job[]>(JOBS_KEY, [])
        .filter((j) => !j.paused)
        .map((j) => retryAt.get(keyOf(j)) ?? Infinity),
    )
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
  let pendingExtras: Promise<unknown> | undefined
  setStatus(key, { phase: 'preparing', done: 0, total: 0, bytes: 0 })
  try {
    const [episodes, ready] = await Promise.all([
      api.get<{ items: Episode[] }>(`/api/episodes${query({ id: job.animeId })}`),
      api.get<{ ready: boolean; downloading?: boolean }>(`/api/saved/ready${query({ anime: job.animeId, episode: job.episode })}`),
    ])
    const ep = episodes.items.find((e) => e.number === job.episode)
    if (job.auto && (!ep || ep.planned || isUnaired(ep, Date.now()))) {
      dropJob(key)
      setStatus(key, null)
      return
    }
    if (halted.has(key)) throw new Paused()
    if (!ep || !ready.ready) return await waitForPC(job, !!ready.downloading, !!ep && isUnaired(ep, Date.now()))
    const [show, prefs] = await Promise.all([
      api.get<ShowCard>(`/api/anime/${job.animeId}`),
      api.get<{ effective: Record<string, string> }>(`/api/prefs${query({ anime: job.animeId })}`),
    ])

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
    const shape = JSON.stringify([stream.plan, stream.duration, stream.audioTrack, job.quality])
    const begun = await cache.match(`${at}/shape`)
    if (begun && (await begun.text()) !== shape) await removeFiles(key)
    await cache.put(`${at}/shape`, new Response(shape))

    const from = `/api/stream/${stream.id}`
    const playlist = await (await fetchOk(stream.playlist)).text()
    const pieces = ['init.mp4', ...[...playlist.matchAll(/^(\d+\.mp4)$/gm)].map((m) => m[1])]
    await cache.put(`${at}/playlist.m3u8`, new Response(playlist, { headers: { 'Content-Type': 'application/vnd.apple.mpegurl' } }))
    const extras = keepExtras(cache, key, stream).then((ok) => ({ ok }), (err: unknown) => ({ err }))
    pendingExtras = extras
    void api.get(`${from}/thumbnails`).catch(() => {})

    let size = 0
    let done = 0
    let next = 0
    let fresh = 0
    let freshBytes = 0
    let failed: unknown = null
    const began = Date.now()
    const rate = () => (fresh >= 3 && Date.now() > began ? freshBytes / ((Date.now() - began) / 1000) : undefined)
    const worker = async () => {
      try {
        while (next < pieces.length && !failed) {
          if (cancelled.has(key)) throw new Cancelled()
          if (halted.has(key)) throw new Paused()
          if (onMobileData()) throw new Held()
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
            restarts.delete(key)
            if (fresh % 8 === 4) await checkRoom((freshBytes / fresh) * (pieces.length - done - 1))
          }
          done++
          if (!failed && !halted.has(key)) setStatus(key, { phase: 'saving', done, total: pieces.length, bytes: size, rate: rate() })
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
        cover = undefined
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
    if (cancelled.has(key) || !read<Job[]>(JOBS_KEY, []).some((j) => keyOf(j) === key)) throw new Cancelled()
    await keepJSON(cache, `${at}/info.json`, info)
    dropJob(key)
    restarts.delete(key)
    setStatus(key, null)
    await refreshSaved()
    void keepApp()
  } catch (e) {
    if (e instanceof Cancelled || cancelled.has(key)) {
      await pendingExtras
      return await removeFiles(key)
    }
    const was = status.get(key) ?? { done: 0, total: 0, bytes: 0 }
    if (e instanceof Paused || e instanceof Held) {
      if (halted.has(key)) return setStatus(key, { ...was, phase: 'paused', message: undefined })
      if (e instanceof Paused) return setStatus(key, { ...was, phase: 'queued', message: undefined })
      retryAt.set(key, Date.now() + 60_000)
      return setStatus(key, { ...was, phase: 'stopped', message: WIFI_WAIT })
    }
    if (e instanceof ApiError && e.status === 404 && (restarts.get(key) ?? 0) < 3) {
      restarts.set(key, (restarts.get(key) ?? 0) + 1)
      retryAt.set(key, Date.now() + 5000)
      setStatus(key, { ...was, phase: 'stopped', message: 'Starting again' })
      return
    }
    if (e instanceof SaveFailed || e instanceof ApiError || (e instanceof DOMException && e.name === 'QuotaExceededError')) {
      retryAt.set(key, Infinity)
      const message = e instanceof DOMException ? 'This device ran out of room for it' : (e as Error).message
      write(JOBS_KEY, read<Job[]>(JOBS_KEY, []).map((j) => (keyOf(j) === key ? { ...j, failed: message } : j)))
      setStatus(key, { ...was, phase: 'failed', message })
      return
    }
    retryAt.set(key, Date.now() + 30_000)
    setStatus(key, { ...was, phase: 'stopped', message: "Can't reach Yoru" })
  } finally {
    if (streamId) void api.del(`/api/stream/${streamId}`).catch(() => {})
  }
}

async function waitForPC(job: Job, downloading: boolean, unaired = false) {
  const key = keyOf(job)
  const [downloads, queue] = await Promise.all([
    api.get<{ items: Download[] }>('/api/downloads'),
    api.get<{ items: Queued[] }>('/api/download/queue'),
  ])
  const item = downloads.items.find((d) => d.animeId === job.animeId && d.episodes.includes(String(job.episode)))
  const queued = queue.items.find((q) => q.animeId === job.animeId && q.episode === job.episode)
  if (halted.has(key)) throw new Paused()
  const retry = reask.delete(key)
  if (queued?.state === 'failed' && !retry) {
    throw new SaveFailed(`The PC couldn't download it: ${queued.error ?? 'no release found'}`)
  }
  if (!downloading && !item && (!queued || queued.state === 'failed')) {
    await api.post('/api/download', { animeId: job.animeId, episode: job.episode })
  }
  retryAt.set(key, Date.now() + (unaired ? 5 * 60_000 : 20_000))
  setStatus(key, { phase: halted.has(key) ? 'paused' : 'waiting', done: 0, total: 0, bytes: 0, percent: item?.percent })
}

async function keepExtras(cache: Cache, key: string, stream: StreamInfo) {
  const at = savedBase(key)
  let size = 0
  const kept = new Map<number, SubtitleTrack>()
  const queue = [...stream.subtitles]
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
  for (const font of stream.fontsUrl ? await extractedFonts(stream.fontsUrl, key) : []) {
    if (cancelled.has(key)) throw new Cancelled()
    const path = `${at}/font/${font.split('/').pop()}`
    const blob = await (await fetchOk(font)).blob()
    await keep(cache, path, blob, 'font/ttf')
    size += blob.size
    fonts.push({ url: path })
  }
  if (cancelled.has(key)) throw new Cancelled()
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

async function extractedFonts(url: string, key: string): Promise<string[]> {
  for (let i = 0; i < 60; i++) {
    if (cancelled.has(key)) throw new Cancelled()
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

let keepingApp: Promise<void> | null = null

function keepApp() {
  keepingApp ??= storeApp().finally(() => {
    keepingApp = null
  })
  return keepingApp
}

async function storeApp() {
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
  }
}

interface Watched {
  animeId: number
  episode: number
  position: number
  duration: number
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

const subscribeWatched = (l: () => void) => {
  watchedListeners.add(l)
  return () => {
    watchedListeners.delete(l)
  }
}
const currentWatched = () => watchedSnapshot

export function useWatched() {
  return useSyncExternalStore(subscribeWatched, currentWatched)
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
      now[key] = cur.at === w.at ? { ...cur, played: 0, dirty: false } : { ...cur, played: Math.max(0, cur.played - w.played) }
    }
    if (saved !== null) {
      const here = new Set(saved.map((e) => e.key))
      for (const [key, w] of Object.entries(now)) if (!w.dirty && !here.has(key)) delete now[key]
    }
    writeWatched(now)
  } catch {
  } finally {
    sending = false
  }
}

const sinceSaved = (e: SavedEpisode, watched: Record<string, Watched>) => {
  const w = watched[e.key]
  return w && w.at >= (e.savedAt ?? 0) ? w : undefined
}

export function resumeAt(e: SavedEpisode, watched: Record<string, Watched>): number {
  const w = sinceSaved(e, watched)
  if (!w) return e.startAt
  return w.duration > 0 && w.position / w.duration >= 0.9 ? 0 : w.position
}

export function watchedThrough(e: SavedEpisode, watched: Record<string, Watched>): number {
  const w = sinceSaved(e, watched)
  return w && w.duration > 0 ? Math.min(1, w.position / w.duration) : 0
}

export const savedShare = (s?: SaveStatus) => (s?.phase === 'saving' && s.total ? s.done / s.total : 0)

export function saveEta(s?: SaveStatus): string {
  if (s?.phase !== 'saving' || !s.rate || !s.done || !s.total) return ''
  return aboutLeft(((s.bytes / s.done) * (s.total - s.done)) / s.rate)
}

export function saveWords(s: SaveStatus | undefined, place: number): string {
  switch (s?.phase) {
    case 'saving':
      return `Saving · ${Math.floor(savedShare(s) * 100)}%`
    case 'preparing':
      return 'Getting it ready on the PC'
    case 'waiting':
      return `Waiting for the PC to download it${s.percent != null ? ` · ${Math.floor(s.percent)}%` : ''}`
    case 'paused':
      return 'Paused'
    case 'stopped':
      if (s.message === WIFI_WAIT) return 'Waiting for Wi-Fi'
      if (s.message === "Can't reach Yoru") return "Can't reach the PC · carries on when it answers"
      return s.message ?? 'Stopped for now'
    case 'failed':
      return `Couldn't save: ${s.message ?? 'something went wrong'}`
    default:
      return place > 1 ? `Waiting · ${ordinal(place)} in line` : 'Next in line'
  }
}
