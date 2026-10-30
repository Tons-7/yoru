import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import type { FollowState } from '../lib/api'
import type { CardAnime } from '../lib/cards'
import { cx, posterSet, tint } from '../lib/format'
import { useNow } from '../lib/queries'
import { Badge, ProgressBar } from './ui'
import { AlertPill } from './AlertPill'
import { useAlertSwitch } from '../lib/alerts'
import { HoverInfo } from './HoverInfo'
import { StatusMenu } from './StatusMenu'

const WEEK = 7 * 86_400

function starts(date?: string | null) {
  const m = date?.match(/^(\d{4})(?:-(\d{2}))?(?:-(\d{2}))?$/)
  const [year, month, day] = [Number(m?.[1] ?? 0), Number(m?.[2] ?? 0), Number(m?.[3] ?? 0)]
  if (!year) return null
  if (!month) return `Starts ${year}`
  const when = new Date(year, month - 1, day || 1)
  return `Starts ${when.toLocaleDateString(undefined, day ? { month: 'short', day: 'numeric' } : { month: 'short', year: 'numeric' })}`
}

const count = (n: number) => n.toLocaleString()

export function PosterCard({
  anime,
  to,
  quietList,
  alert,
}: {
  anime: CardAnime
  to?: string
  quietList?: boolean
  alert?: boolean
}) {
  const href = to ?? `/anime/${anime.id}`
  const now = useNow(600_000) / 1000
  const [loaded, setLoaded] = useState(false)
  const watched = anime.progress ?? 0
  const released = anime.aired ?? anime.episodes
  const nextEpisode =
    anime.airing === 'NOT_YET_RELEASED' || (released != null && watched >= released) ? null : watched + 1
  const airing = anime.airing === 'RELEASING'
  const fresh = airing && !!anime.aired && !!anime.nextAiringAt && anime.nextAiringAt - now <= WEEK
  const format = anime.format?.replace('_', ' ')
  const start = starts(anime.startDate)

  return (
    <HoverInfo
      anime={{
        ...anime,
        status: anime.airing,
        listStatus: anime.status,
        play: nextEpisode
          ? {
              to: `/watch/${anime.id}/${nextEpisode}`,
              label: watched > 0 ? `Continue ep ${nextEpisode}` : 'Watch ep 1',
            }
          : undefined,
      }}
    >
    <div className="group/card relative">
      <Link
        to={href}
        viewTransition
        onClick={(e) => {
          const img = e.currentTarget.querySelector('img')
          if (img) img.style.viewTransitionName = 'show-art'
        }}
        aria-label={anime.title}
        className="press block rounded-poster focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white"
      >
        <div
          className="relative aspect-[2/3] overflow-hidden rounded-poster bg-base-850"
          style={
            anime.color
              ? { backgroundImage: `linear-gradient(to bottom, ${tint(anime.color, 0.32)}, ${tint(anime.color, 0.12)})` }
              : undefined
          }
        >
          {anime.cover ? (
            <img
              ref={(img) => {
                if (img?.complete) setLoaded(true)
              }}
              src={anime.cover}
              srcSet={posterSet(anime.cover, anime.coverLarge)}
              alt=""
              loading="lazy"
              decoding="async"
              onLoad={() => setLoaded(true)}
              className={cx(
                'size-full object-cover transition-[opacity,transform] duration-150 group-hover/card:scale-[1.04]',
                loaded ? 'opacity-100' : 'opacity-0',
              )}
            />
          ) : (
            <div className="flex size-full flex-col items-center justify-center gap-2 p-3 text-center">
              <span className="text-3xl font-bold text-white/80">{anime.title.slice(0, 1)}</span>
              <span className="line-clamp-3 text-xs font-medium text-white/80">{anime.title}</span>
            </div>
          )}
          <span aria-hidden className="pointer-events-none absolute inset-0 rounded-poster ring-1 ring-white/[0.08] ring-inset" />

          {anime.badge ? (
            <div className="absolute top-1.5 left-1.5">
              <Badge tone="overlay">{anime.badge}</Badge>
            </div>
          ) : fresh ? (
            <span className="absolute top-1.5 left-1.5 flex items-center gap-1.5 rounded-md bg-base-950/85 px-1.5 py-0.5 text-xs font-semibold text-base-100 backdrop-blur-sm">
              <span className="size-1.5 rounded-full bg-sky-300" />
              New Ep {count(anime.aired!)}
            </span>
          ) : null}

          {typeof anime.percent === 'number' && anime.percent > 0 && (
            <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-base-950/80 to-transparent p-2 pt-6">
              <ProgressBar value={anime.percent} />
            </div>
          )}
        </div>
      </Link>

      {nextEpisode && (
        <div className="pointer-events-none absolute inset-x-0 top-0 grid aspect-[2/3] place-items-center">
          <Link
            to={`/watch/${anime.id}/${nextEpisode}`}
            aria-label={`Play episode ${nextEpisode}`}
            title={`Play episode ${nextEpisode}`}
            className="pointer-events-auto grid size-11 place-items-center rounded-full bg-base-950/75 opacity-0 backdrop-blur-sm transition-[opacity,transform] duration-150 group-hover/card:opacity-100 hover:scale-105 hover:bg-accent-500 focus-visible:opacity-100 [@media(hover:none)]:hidden"
          >
            <PlayIcon />
          </Link>
        </div>
      )}

      <div className="absolute top-1.5 right-1.5">
        <StatusMenu animeId={anime.id} current={anime.status} compact quiet={quietList} />
      </div>

      <Link to={href} tabIndex={-1} aria-hidden className="mt-2 block">
        <p
          className="line-clamp-2 min-h-[2lh] text-sm leading-snug font-medium text-base-100 transition-colors group-hover/card:text-white"
          title={anime.title}
        >
          {anime.title}
        </p>
        <p className="mt-0.5 flex items-center gap-1.5 text-xs whitespace-nowrap text-base-400 tabular-nums">
          <span className="min-w-0 truncate">
            {anime.airing === 'NOT_YET_RELEASED' ? (
              start ? <span className="text-sky-300">{start}</span> : 'Date not set yet'
            ) : airing ? (
              <>
                <span className="text-sky-300">Airing</span>
                {anime.aired
                  ? ` · Ep ${count(anime.aired)}${anime.episodes && anime.episodes > anime.aired ? ` of ${count(anime.episodes)}` : ''}`
                  : ''}
              </>
            ) : (
              [format, anime.episodes ? `${count(anime.episodes)} ${anime.episodes === 1 ? 'ep' : 'eps'}` : null]
                .filter(Boolean)
                .join(' · ')
            )}
          </span>
          {anime.myScore ? (
            <span className="ml-auto shrink-0 text-accent-300" title="Your score">
              ★ {Math.round(anime.myScore / 10)}/10
            </span>
          ) : anime.score ? (
            <span className="ml-auto shrink-0 text-amber-300">★ {(anime.score / 10).toFixed(1)}</span>
          ) : null}
        </p>
      </Link>
      {alert && anime.airing === 'NOT_YET_RELEASED' && <AlertButton animeId={anime.id} title={anime.title} told={anime.alertOn} />}
    </div>
    </HoverInfo>
  )
}

function AlertButton({ animeId, title, told }: { animeId: number; title: string; told?: boolean | null }) {
  const qc = useQueryClient()
  const { change, pending, asked } = useAlertSwitch(animeId, title)
  const saved = useQuery({
    queryKey: ['alert', animeId],
    queryFn: () => qc.getQueryData<{ notify?: FollowState }>(['anime', animeId])?.notify?.on ?? null,
    staleTime: Infinity,
    gcTime: Infinity,
  })
  const known = qc.getQueryData<{ notify?: FollowState }>(['anime', animeId])?.notify?.on
  const on = asked ?? known ?? saved.data ?? told ?? false
  return <AlertPill on={on} title={title} onToggle={() => change(!on)} pending={pending} wide className="mt-2" />
}

export function PosterGrid({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-3 gap-x-3 gap-y-5 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 xl:grid-cols-7 min-[1800px]:grid-cols-8 min-[2200px]:grid-cols-10">
      {children}
    </div>
  )
}

export function PlayIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={cx('size-5 translate-x-px', className)} aria-hidden>
      <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />
    </svg>
  )
}
