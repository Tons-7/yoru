import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { clockTime } from '../lib/format'
import { PlayIcon } from './PosterCard'
import { ProgressBar } from './ui'

export function ResumeCard({
  animeId,
  title,
  cover,
  episode,
  position,
  duration,
  minutes,
  fallback,
  extra,
  menu,
  footer,
}: {
  animeId: number
  title: string
  cover?: string | null
  episode: number
  position?: number | null
  duration?: number | null
  minutes?: number | null
  fallback?: string
  extra?: string | null
  menu?: ReactNode
  footer?: ReactNode
}) {
  const at = position ?? 0
  const started = at > 0
  const percent = started && duration ? Math.min(100, (at / duration) * 100) : 0
  const length = started
    ? duration
      ? `${Math.max(1, Math.round((duration - at) / 60))} min left`
      : `Paused at ${clockTime(at)}`
    : minutes
      ? `${minutes} min`
      : fallback
  const meta = [`Ep ${episode}`, length, extra].filter(Boolean).join(' · ')

  return (
    <div className="group/resume">
      <Link
        to={`/watch/${animeId}/${episode}`}
        aria-label={`Play ${title} episode ${episode}`}
        className="press block rounded-card focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white"
      >
        <div className="relative aspect-video overflow-hidden rounded-card bg-base-850">
          {cover && (
            <img
              src={cover}
              alt=""
              loading="lazy"
              className="size-full object-cover object-center transition-transform duration-300 group-hover/resume:scale-105"
            />
          )}
          <span className="absolute bottom-3 left-2.5 grid size-9 place-items-center rounded-full bg-base-950/70 text-white backdrop-blur-sm transition-colors group-hover/resume:bg-accent-500">
            <PlayIcon className="size-4" />
          </span>
          {percent > 0 && (
            <ProgressBar
              value={percent}
              label="Watched so far"
              className="absolute inset-x-0 bottom-0 h-[3px] rounded-none bg-white/20"
            />
          )}
        </div>
      </Link>
      <div className="mt-2 flex items-start gap-1">
        <div className="min-w-0 flex-1">
          <Link
            to={`/anime/${animeId}`}
            className="line-clamp-2 text-[15px] leading-snug font-semibold text-base-100 transition-colors hover:text-white"
          >
            {title}
          </Link>
          <p className="mt-0.5 line-clamp-1 text-[13px] text-base-400 tabular-nums">{meta}</p>
        </div>
        {menu && <div className="-mt-1.5 -mr-1.5">{menu}</div>}
      </div>
      {footer}
    </div>
  )
}
