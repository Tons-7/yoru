import { useEffect, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { cx, episodeCount, readableTint, relativeTime } from '../lib/format'
import { useNow } from '../lib/queries'
import { ProgressBar } from './ui'

export interface HoverAnime {
  id: number
  title: string
  romaji?: string | null
  english?: string | null
  format?: string | null
  status?: string | null
  episodes?: number | null
  aired?: number | null
  seasonYear?: number | null
  score?: number | null
  genres?: string[] | null
  color?: string | null
  description?: string | null
  progress?: number
  play?: { to: string; label: string }
  cover?: string | null
  banner?: string | null
  malId?: number | null
  nextEpisode?: number | null
  nextAiringAt?: number | null
  startDate?: string | null
  endDate?: string | null
  duration?: number | null
  studios?: { id: number; name: string }[] | null
  listStatus?: string | null
}

const PANEL_WIDTH = 340
const GAP = 12
const OPEN_DELAY = 380
const CLOSE_DELAY = 140

export function HoverInfo({ anime, children }: { anime: HoverAnime; children: ReactNode }) {
  const anchor = useRef<HTMLDivElement>(null)
  const timer = useRef(0)
  const [box, setBox] = useState<{ top: number; left: number } | null>(null)

  useEffect(() => () => window.clearTimeout(timer.current), [])

  const open = () => {
    if (!window.matchMedia('(hover: hover) and (pointer: fine)').matches) return
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => {
      const el = anchor.current
      if (!el) return
      const rect = el.getBoundingClientRect()
      const right = rect.right + GAP
      const left = right + PANEL_WIDTH < window.innerWidth ? right : rect.left - GAP - PANEL_WIDTH
      const top = Math.min(
        Math.max(GAP, rect.top + rect.height / 2 - 220),
        window.innerHeight - 470,
      )
      setBox({ top: Math.max(GAP, top), left: Math.max(GAP, left) })
    }, OPEN_DELAY)
  }

  const close = () => {
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setBox(null), CLOSE_DELAY)
  }

  return (
    <div ref={anchor} onMouseEnter={open} onMouseLeave={close}>
      {children}
      {box &&
        createPortal(
          <Panel anime={anime} top={box.top} left={box.left} onEnter={open} onLeave={close} />,
          document.body,
        )}
    </div>
  )
}

const FORMATS: Record<string, string> = {
  TV_SHORT: 'TV Short',
  MOVIE: 'Movie',
  SPECIAL: 'Special',
  MUSIC: 'Music',
}

function airingState(anime: HoverAnime): { label: string; tone: string; live?: boolean } | null {
  switch (anime.status) {
    case 'RELEASING':
      return { label: 'Airing', tone: 'text-sky-300', live: true }
    case 'FINISHED':
      return { label: 'Finished', tone: 'text-base-200' }
    case 'NOT_YET_RELEASED':
      return { label: 'Upcoming', tone: 'text-sky-300' }
    case 'HIATUS':
      return { label: 'On hiatus', tone: 'text-amber-300' }
    case 'CANCELLED':
      return { label: 'Cancelled', tone: 'text-red-300' }
  }
  return null
}

function day(date: string): string {
  const [y, m, d] = date.split('-').map(Number)
  if (!m) return String(y)
  return new Date(y, m - 1, d || 1).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    ...(d ? { day: 'numeric' } : {}),
  })
}

function aired(anime: HoverAnime): string | null {
  const { startDate, endDate } = anime
  if (!startDate) return anime.seasonYear ? String(anime.seasonYear) : null
  const start = day(startDate)
  if (anime.status === 'RELEASING') return `${start} – now`
  if (endDate && endDate !== startDate) return `${start} – ${day(endDate)}`
  return start
}

function Panel({
  anime,
  top,
  left,
  onEnter,
  onLeave,
}: {
  anime: HoverAnime
  top: number
  left: number
  onEnter: () => void
  onLeave: () => void
}) {
  const now = useNow()
  const hue = readableTint(anime.color) ?? '#8b7cff'
  const art = anime.banner ?? anime.cover
  const state = airingState(anime)
  const count = episodeCount(anime)
  const other = anime.romaji && anime.romaji !== anime.title ? anime.romaji : anime.english
  const synopsis = anime.description?.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim()
  const upcoming =
    anime.nextEpisode && anime.nextAiringAt && anime.nextAiringAt * 1000 > now
      ? `Ep ${anime.nextEpisode} ${relativeTime(anime.nextAiringAt)}`
      : null
  const total = anime.episodes ?? anime.aired
  const rows = [
    { label: 'Aired', value: aired(anime) },
    { label: 'Studio', value: anime.studios?.map((s) => s.name).join(', ') },
    { label: 'Genres', value: anime.genres?.slice(0, 4).join(', ') },
  ].filter((r) => r.value)

  return (
    <div
      onMouseEnter={onEnter}
      onMouseLeave={onLeave}
      style={{ top, left, width: PANEL_WIDTH }}
      className="fixed z-50 animate-rise overflow-hidden rounded-sheet bg-base-800 shadow-panel"
    >
      <div className="relative h-28 overflow-hidden bg-base-850">
        {art && (
          <img
            src={art}
            alt=""
            className={cx('size-full object-cover', !anime.banner && 'scale-110 blur-md brightness-75')}
          />
        )}
        <div className="absolute inset-0 bg-gradient-to-t from-base-800 via-base-800/30 to-transparent" />
        {anime.format && (
          <span className="absolute top-2.5 left-2.5 rounded-md bg-base-950/85 px-2 py-0.5 text-xs font-semibold text-base-100 backdrop-blur-sm">
            {FORMATS[anime.format] ?? anime.format.replace('_', ' ')}
          </span>
        )}
        {state && (
          <span
            className={cx(
              'absolute top-2.5 right-2.5 flex items-center gap-1.5 rounded-full bg-base-950/85 px-2 py-0.5 text-xs font-semibold backdrop-blur-sm',
              state.tone,
            )}
          >
            {state.live && (
              <span className="relative flex size-1.5">
                <span className="absolute inset-0 animate-ping rounded-full bg-sky-300 opacity-75" />
                <span className="relative size-1.5 rounded-full bg-sky-300" />
              </span>
            )}
            {state.label}
          </span>
        )}
      </div>

      <div className="-mt-6 space-y-3 px-4 pb-4">
        <div className="relative">
          <p className="line-clamp-2 text-base leading-tight font-bold text-white">{anime.title}</p>
          {other && other !== anime.title && (
            <p className="mt-1 line-clamp-1 text-xs text-base-400">{other}</p>
          )}
        </div>

        <div className="flex flex-wrap gap-1.5 text-xs font-semibold">
          {anime.score ? (
            <span className="flex items-center gap-1 rounded-md bg-amber-400/10 px-2 py-1 text-amber-300">
              <StarIcon />
              {(anime.score / 10).toFixed(1)}
            </span>
          ) : null}
          {count && (
            <span
              className="flex items-center gap-1 rounded-md bg-base-750 px-2 py-1 text-base-100"
              title={
                anime.status === 'RELEASING' && anime.aired
                  ? `${anime.aired} out${anime.episodes ? ` of ${anime.episodes}` : ''}`
                  : undefined
              }
            >
              <LayersIcon />
              {count}
            </span>
          )}
          {anime.duration ? (
            <span className="rounded-md bg-base-750 px-2 py-1 text-base-300">{anime.duration} min</span>
          ) : null}
        </div>

        {upcoming && (
          <p className="flex items-center gap-1.5 text-xs font-medium text-sky-300">
            <ClockIcon />
            {upcoming}
          </p>
        )}

        {synopsis && (
          <p
            className="line-clamp-4 border-l-2 pl-3 text-[13px] leading-relaxed text-base-300"
            style={{ borderColor: hue }}
          >
            {synopsis}
          </p>
        )}

        {rows.length > 0 && (
          <dl className="grid grid-cols-[4.25rem_1fr] gap-x-2 gap-y-1 text-xs">
            {rows.map((r) => (
              <div key={r.label} className="contents">
                <dt className="font-semibold text-base-500">{r.label}</dt>
                <dd className="truncate font-medium text-base-200">{r.value}</dd>
              </div>
            ))}
          </dl>
        )}

        {!!anime.progress && anime.progress > 0 && (
          <div>
            <p className="mb-1 text-xs text-base-400">
              Watched {anime.progress}
              {total ? ` of ${total}${anime.episodes ? '' : ' out'}` : ''}
            </p>
            <ProgressBar value={total ? (anime.progress / total) * 100 : 0} />
          </div>
        )}

        <div className="flex items-stretch gap-1.5 pt-1">
          <div className="flex overflow-hidden rounded-lg bg-base-750">
            {anime.malId ? (
              <ExternalChip href={`https://myanimelist.net/anime/${anime.malId}`} label="MAL" />
            ) : null}
            {anime.id > 0 && <ExternalChip href={`https://anilist.co/anime/${anime.id}`} label="AL" />}
          </div>
          <Link
            to={`/anime/${anime.id}`}
            className="flex-1 rounded-lg bg-base-750 px-2 py-2 text-center text-xs font-semibold text-base-100 transition-colors hover:bg-base-700"
          >
            Details
          </Link>
          {anime.play && (
            <Link
              to={anime.play.to}
              title={anime.play.label}
              aria-label={anime.play.label}
              className="flex items-center gap-1.5 rounded-lg bg-accent-500 px-3 text-xs font-semibold text-white transition-colors hover:bg-accent-600"
            >
              <PlayGlyph />
              {anime.play.label}
            </Link>
          )}
        </div>
      </div>
    </div>
  )
}

function ExternalChip({ href, label }: { href: string; label: string }) {
  return (
    <a
      href={href}
      target="_blank"
      rel="noreferrer"
      className="grid place-items-center px-2.5 text-xs font-semibold text-base-300 transition-colors not-last:border-r not-last:border-white/10 hover:bg-base-700 hover:text-white"
    >
      {label}
    </a>
  )
}

function StarIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-3" aria-hidden>
      <path d="m12 3 2.7 5.6 6.1.9-4.4 4.3 1 6.1L12 17l-5.4 2.9 1-6.1-4.4-4.3 6.1-.9L12 3Z" fill="currentColor" />
    </svg>
  )
}

function LayersIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-3" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="m12 3 9 5-9 5-9-5 9-5Z" strokeLinejoin="round" />
      <path d="m3 13 9 5 9-5" strokeLinejoin="round" />
    </svg>
  )
}

function ClockIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-3" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" strokeLinecap="round" />
    </svg>
  )
}

function PlayGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-3.5" aria-hidden>
      <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />
    </svg>
  )
}
