import type { LibraryItem } from './api'

export interface SeasonSplit {
  name: string
  season: string | null
}

const ROMAN: Record<string, number> = { i: 1, ii: 2, iii: 3, iv: 4, v: 5, vi: 6, vii: 7, viii: 8, ix: 9, x: 10 }
const WORDS: Record<string, number> = {
  one: 1, two: 2, three: 3, four: 4, five: 5, six: 6, seven: 7, eight: 8, nine: 9, ten: 10,
}
const ORDINALS: Record<string, number> = {
  first: 1, second: 2, third: 3, fourth: 4, fifth: 5, sixth: 6, seventh: 7, eighth: 8, ninth: 9, tenth: 10,
}
const KINDS: Record<string, string> = { season: 'Season', stage: 'Stage', part: 'Part', cour: 'Cour' }
const SEPARATORS = new Set(['-', '–', '—', ':', '|'])

function count(word?: string): number | null {
  if (!word) return null
  if (/^\d{1,2}$/.test(word)) return Number(word) || null
  return ROMAN[word] ?? WORDS[word] ?? null
}

function ordinal(word?: string): number | null {
  if (!word) return null
  const m = word.match(/^(\d{1,2})(st|nd|rd|th)$/)
  if (m) return Number(m[1]) || null
  return ORDINALS[word] ?? null
}

function piece(words: string[], at: number, kinds: string[]): { label: string; next: number } | null {
  const [a, b] = [words[at], words[at + 1]]
  if (kinds.includes(a) && count(b)) return { label: `${KINDS[a]} ${count(b)}`, next: at + 2 }
  if (kinds.includes(b) && ordinal(a)) return { label: `${KINDS[b]} ${ordinal(a)}`, next: at + 2 }
  return null
}

function parseTail(raw: string[]): string | null {
  const words = raw.map((w) => w.toLowerCase().replace(/[:,]$/, '')).filter((w) => w && !SEPARATORS.has(w))
  if (words.length === 0) return null
  let at = 0
  const the = words[0] === 'the'
  if (the) at = 1

  if (words[at] === 'movie') {
    const n = words.length === at + 2 ? count(words[at + 1]) : null
    if (words.length === at + 1) return 'Movie'
    return n ? `Movie ${n}` : null
  }
  if (!the && words.length === 1 && ['ii', 'iii', 'iv'].includes(words[0])) return `Season ${ROMAN[words[0]]}`

  let head: string | null = null
  if (words[at] === 'final' && (words[at + 1] === 'season' || words[at + 1] === 'stage')) {
    head = `Final ${KINDS[words[at + 1]]}`
    at += 2
  } else if (!the) {
    const first = piece(words, at, ['season', 'stage', 'part', 'cour'])
    if (!first) return null
    head = first.label
    at = first.next
  }
  if (!head) return null
  if (at === words.length) return head
  if (head.startsWith('Part') || head.startsWith('Cour')) return null
  const second = piece(words, at, ['part', 'cour'])
  if (!second || second.next !== words.length) return null
  return `${head} ${second.label}`
}

export function splitSeason(title: string): SeasonSplit {
  const words = title.trim().split(/\s+/)
  for (let k = 1; k < words.length; k++) {
    const season = parseTail(words.slice(k))
    if (!season) continue
    const front = words.slice(0, k)
    while (front.length && SEPARATORS.has(front[front.length - 1])) front.pop()
    const name = front.join(' ').replace(/\s*:$/, '')
    if (name.length >= 2 && /[\p{L}\p{N}]/u.test(name) && name.toLowerCase() !== 'the') return { name, season }
  }
  return { name: title.trim(), season: null }
}

export function seasonOf(title: string, format?: string | null): SeasonSplit {
  const split = splitSeason(title)
  if (format === 'MOVIE' && !split.season) return { name: split.name, season: 'Movie' }
  return split
}

const number = (n: number) => n.toLocaleString('en-US')
const eps = (n: number) => `${number(n)} ${n === 1 ? 'ep' : 'eps'}`

export function outDay(unix: number, now: number): string {
  const day = (t: number) => {
    const d = new Date(t * 1000)
    d.setHours(0, 0, 0, 0)
    return d.getTime()
  }
  const ahead = Math.round((day(unix) - day(now)) / 86_400_000)
  if (ahead <= 0) return 'today'
  if (ahead === 1) return 'tomorrow'
  const when = new Date(unix * 1000)
  if (ahead < 7) return when.toLocaleDateString('en-US', { weekday: 'short' })
  return when.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

export interface LibraryProgress {
  aired: number | null
  watched: number
  next: number | null
  waiting: number | null
  done: boolean
  percent: number
  lead: string
  tail: string | null
  fresh: boolean
}

type Facts = Pick<LibraryItem, 'episodes' | 'progress' | 'status' | 'nextEpisode' | 'nextAiringAt' | 'airing'>

export function airedOf(item: Pick<LibraryItem, 'episodes' | 'nextEpisode' | 'airing'>): number | null {
  if (item.airing === 'NOT_YET_RELEASED') return 0
  if (item.nextEpisode) return item.nextEpisode - 1
  return item.episodes ?? null
}

export function libraryProgress(item: Facts, now: number): LibraryProgress {
  const total = item.episodes || null
  const aired = airedOf(item)
  const watched = Math.max(0, item.progress)
  const done = item.status === 'COMPLETED' || (!!total && watched >= total && item.airing !== 'RELEASING')
  const base = { aired, watched, done, fresh: false }

  if (done) {
    const all = total ?? (watched || null)
    return { ...base, next: null, waiting: null, percent: 100, lead: all ? `Done · ${eps(all)}` : 'Done', tail: null }
  }

  const percent = aired ? Math.min(100, (watched / aired) * 100) : 0
  const upcoming = watched + 1
  const out = aired === null || upcoming <= aired
  const next = out ? upcoming : null
  const waiting = out ? null : upcoming
  const active = item.status === 'CURRENT' || item.status === 'REPEATING'

  if (watched === 0 && !active && out) {
    const size = total ?? aired
    return { ...base, next, waiting, percent, lead: size ? `Not started · ${eps(size)}` : 'Not started', tail: null }
  }
  if (waiting !== null) {
    const when = item.nextAiringAt ? `out ${outDay(item.nextAiringAt, now)}` : 'not out yet'
    return { ...base, next, waiting, percent, lead: `Ep ${number(waiting)} next`, tail: when }
  }
  const lead = `Ep ${number(upcoming)} next`
  if (item.airing === 'RELEASING' && aired !== null && aired > watched) {
    return { ...base, next, waiting, percent, lead, tail: `${number(aired - watched)} new`, fresh: true }
  }
  const size = total ?? aired
  const left = size !== null ? size - watched : null
  return { ...base, next, waiting, percent, lead, tail: left && left > 0 ? `${number(left)} left` : null }
}

export function lastWatched(unix: number | null | undefined, now: number): string | null {
  if (!unix) return null
  const day = (t: number) => {
    const d = new Date(t * 1000)
    d.setHours(0, 0, 0, 0)
    return d.getTime()
  }
  const back = Math.round((day(now) - day(unix)) / 86_400_000)
  if (back <= 0) return 'Today'
  if (back === 1) return 'Yesterday'
  if (back < 7) return `${back} days ago`
  const when = new Date(unix * 1000)
  const sameYear = when.getFullYear() === new Date(now * 1000).getFullYear()
  return when.toLocaleDateString('en-US', sameYear ? { month: 'short', day: 'numeric' } : { month: 'short', year: 'numeric' })
}

export function timeLeft(position: number, duration?: number | null): string {
  if (!duration || duration <= 0) return 'Partly watched'
  const seconds = Math.max(0, duration - position)
  if (seconds < 60) return 'Under a minute left'
  const minutes = Math.round(seconds / 60)
  if (minutes < 60) return `${minutes} min left`
  const h = Math.floor(minutes / 60)
  const m = minutes % 60
  return m ? `${h} h ${m} min left` : `${h} h left`
}

export interface PlayedEpisode {
  episode: number
  position: number
  duration?: number | null
  finished: boolean
  progress: number
  episodes?: number | null
  airing?: string | null
  nextEpisode?: number | null
  nextAiringAt?: number | null
}

export type UpNext =
  | { kind: 'next'; episode: number; resume: PlayedEpisode | null }
  | { kind: 'waiting'; episode: number; at: number | null }
  | { kind: 'done' }

export function historyUpNext(entries: PlayedEpisode[]): UpNext {
  const show = entries[0]
  const furthest = Math.max(show.progress, ...entries.filter((e) => e.finished).map((e) => e.episode))
  const episode = furthest + 1
  const total = show.episodes || null
  if (total && episode > total) return { kind: 'done' }
  const aired = airedOf({ episodes: show.episodes ?? null, nextEpisode: show.nextEpisode ?? null, airing: show.airing })
  if (aired !== null && episode > aired) return { kind: 'waiting', episode, at: show.nextAiringAt ?? null }
  const resume = entries.find((e) => e.episode === episode && !e.finished && e.position > 0) ?? null
  return { kind: 'next', episode, resume }
}
