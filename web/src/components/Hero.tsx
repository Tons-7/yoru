import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import type { DiscoverItem } from '../lib/api'
import { cx, tint } from '../lib/format'
import { useMediaQuery } from '../lib/hooks'
import { PlayIcon } from './PosterCard'
import { StatusMenu } from './StatusMenu'
import { Skeleton } from './ui'

const ROTATE_MS = 9000

// Artwork is fetched two slides ahead, time enough on a slow connection.
const PRELOAD = 2

export function Hero({ items, loading }: { items?: DiscoverItem[]; loading?: boolean }) {
  const slides = (items ?? []).filter((a) => a.cover).slice(0, 10)
  const [picked, setIndex] = useState(0)
  const [paused, setPaused] = useState(false)
  // Hover and focus pause it; a touch screen has neither, so it stops by hand. Reduced motion never moves.
  const [stopped, setStopped] = useState(false)
  const calm = useMediaQuery('(prefers-reduced-motion: reduce)')
  const timer = useRef<number | undefined>(undefined)
  // A shorter list after a refetch would otherwise leave the index past the end.
  const index = picked < slides.length ? picked : 0

  useEffect(() => {
    if (paused || stopped || calm || slides.length < 2) return
    timer.current = window.setInterval(
      () => setIndex((i) => (i + 1) % slides.length),
      ROTATE_MS,
    )
    return () => window.clearInterval(timer.current)
  }, [paused, stopped, calm, slides.length])

  // Every slide sits in the viewport at opacity zero, so lazy loading holds none back: fetched just ahead of
  // the rotation, kept once arrived, and updated during render so no effect paints it a frame late.
  const [fetched, setFetched] = useState<number[]>([])
  const ahead = slides.length ? Array.from({ length: PRELOAD + 1 }, (_, step) => (index + step) % slides.length) : []
  if (ahead.some((i) => !fetched.includes(i))) setFetched([...new Set([...fetched, ...ahead])])

  if (loading) {
    return <Skeleton className="h-[clamp(22rem,46vw,34rem)] w-full rounded-2xl" />
  }
  // Trending failed or is empty: nothing to feature, and an endless skeleton reads as broken.
  if (slides.length === 0) return null

  const active = slides[index]
  // Nothing to play: finished, unaired, or caught up with what has aired (PosterCard's rule).
  const aired = active.nextEpisode ? active.nextEpisode - 1 : active.episodes
  const caughtUp =
    active.listStatus === 'COMPLETED' ||
    active.status === 'NOT_YET_RELEASED' ||
    (!!aired && (active.progress ?? 0) >= aired)

  return (
    <section
      className="relative h-[clamp(22rem,46vw,34rem)] overflow-hidden rounded-2xl bg-base-900 ring-1 ring-white/[0.06]"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      // Keyboard too: rotating under focus sent Enter to a different show.
      onFocus={() => setPaused(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setPaused(false)
      }}
      aria-roledescription="carousel"
    >
      {slides.map((anime, i) => (
        <div
          key={anime.id}
          aria-hidden={i !== index}
          className={cx(
            'absolute inset-0 transition-opacity duration-700',
            i === index ? 'opacity-100' : 'pointer-events-none opacity-0',
          )}
        >
          {fetched.includes(i) &&
            (anime.banner ? (
              <img
                src={anime.banner}
                alt=""
                decoding="async"
                // A slow drift keeps the strip alive; reduced motion stops it.
                className={cx(
                  'size-full object-cover object-center transition-transform duration-[9000ms] ease-linear',
                  i === index ? 'scale-105' : 'scale-100',
                )}
              />
            ) : (
              // No banner: stretched across a wide strip a poster is a smear, so there it becomes the
              // backdrop. A narrow strip is about a poster's width, and shows it as it is.
              <img
                src={anime.coverLarge ?? anime.cover}
                alt=""
                decoding="async"
                className="size-full object-cover lg:scale-125 lg:opacity-60 lg:blur-2xl"
              />
            ))}
          <div className="absolute inset-0 bg-gradient-to-r from-base-950 via-base-950/75 to-transparent" />
          <div className="absolute inset-0 bg-gradient-to-t from-base-950 via-base-950/10 to-transparent" />
          {/* And the poster itself, sharp, where the banner's subject would have been. */}
          {fetched.includes(i) && !anime.banner && (
            <img
              src={anime.coverLarge ?? anime.cover}
              alt=""
              decoding="async"
              // Smaller on a mid-width strip, where a tall poster would sit over the title.
              className="absolute top-1/2 right-[6%] aspect-[2/3] h-[60%] -translate-y-1/2 rounded-xl object-cover shadow-lift ring-1 ring-white/10 max-lg:hidden xl:right-[12%] xl:h-[76%]"
            />
          )}
          {/* The show's own colour, low in the corner. */}
          <div
            className="absolute inset-0"
            style={{
              background: `radial-gradient(60% 70% at 0% 100%, ${tint(anime.color, 0.35) ?? 'rgb(111 92 255 / 0.25)'}, transparent 70%)`,
            }}
          />
        </div>
      ))}

      <div className="relative flex h-full flex-col justify-end p-5 sm:p-10">
        <div className="max-w-2xl">
          <div className="mb-3 flex flex-wrap items-center gap-2 text-xs font-semibold">
            <span className="rounded-md bg-accent-500 px-2 py-0.5 tracking-wider text-white uppercase">
              #{index + 1} Trending
            </span>
            {active.format && (
              <span className="rounded-md bg-base-950/60 px-2 py-0.5 text-base-100 uppercase ring-1 ring-white/10 backdrop-blur-md">
                {active.format.replace('_', ' ')}
              </span>
            )}
            {active.episodes ? (
              <span className="rounded-md bg-base-950/60 px-2 py-0.5 text-base-100 uppercase ring-1 ring-white/10 backdrop-blur-md">
                {active.episodes} eps
              </span>
            ) : null}
            {active.score ? (
              <span className="rounded-md bg-base-950/60 px-2 py-0.5 text-amber-300 ring-1 ring-amber-400/30 backdrop-blur-md">
                ★ {(active.score / 10).toFixed(1)}
              </span>
            ) : null}
          </div>

          <h1 className="font-display text-3xl leading-[1.05] font-bold tracking-tight text-white text-balance drop-shadow-lg sm:text-5xl lg:text-6xl">
            {active.title}
          </h1>

          {active.Genres && active.Genres.length > 0 && (
            <p className="mt-3 text-sm font-medium text-base-200">{active.Genres.slice(0, 4).join(' · ')}</p>
          )}
          {active.description && (
            <p className="mt-2 line-clamp-2 max-w-xl text-sm leading-relaxed text-base-300 max-sm:hidden">
              {active.description.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim()}
            </p>
          )}

          <div className="mt-6 flex items-center gap-2">
            {/* No next episode to link to, so the show page decides. */}
            {caughtUp ? null : (
              <Link
                to={`/watch/${active.id}/${(active.progress ?? 0) + 1}`}
                className="flex items-center gap-2 rounded-xl bg-gradient-to-b from-accent-400 to-accent-500 px-5 py-2.5 text-sm font-semibold text-white shadow-[0_8px_24px_-8px_rgb(111_92_255/0.7)] transition-[transform,filter] hover:scale-[1.02] hover:brightness-110 active:scale-95"
              >
                <PlayIcon />
                {active.progress ? `Continue ep ${active.progress + 1}` : 'Watch'}
              </Link>
            )}
            <Link
              to={`/anime/${active.id}`}
              className={cx(
                'rounded-xl px-5 py-2.5 text-sm font-medium backdrop-blur-md transition-colors',
                caughtUp
                  ? 'bg-gradient-to-b from-accent-400 to-accent-500 text-white hover:brightness-110'
                  : 'bg-white/10 text-white ring-1 ring-white/15 hover:bg-white/20',
              )}
            >
              Details
            </Link>
            {/* Keyed by slide: rotation must close a menu opened for the last one. */}
            <StatusMenu key={active.id} animeId={active.id} current={active.listStatus ?? null} compact />
          </div>
        </div>

        <div className="mt-4 flex" role="tablist" aria-label="Featured anime">
          {slides.map((slide, i) => (
            // The bar is 4px; the button around it is what a finger has to hit.
            <button
              key={slide.id}
              role="tab"
              aria-selected={i === index}
              aria-label={slide.title}
              onClick={() => setIndex(i)}
              className="group px-[3px] py-2 pointer-coarse:px-1.5 pointer-coarse:py-4"
            >
              <span
                className={cx(
                  'block h-1 rounded-full transition-all duration-300',
                  i === index ? 'w-7 bg-white' : 'w-3 bg-white/30 group-hover:bg-white/50',
                )}
              />
            </button>
          ))}
        </div>
      </div>

      {slides.length > 1 && (
        <div className="absolute right-4 bottom-4 flex gap-2 sm:right-6 sm:bottom-6">
          {!calm && (
            <button
              aria-label={stopped ? 'Play slideshow' : 'Pause slideshow'}
              aria-pressed={stopped}
              onClick={() => setStopped((v) => !v)}
              className="tap relative rounded-full bg-base-950/50 p-2 text-white/70 backdrop-blur-sm transition-colors hover:bg-base-950/80 hover:text-white"
            >
              <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
                {stopped ? (
                  <path d="M8 5v14l11-7z" fill="currentColor" />
                ) : (
                  <path d="M7 5h3.5v14H7zM13.5 5H17v14h-3.5z" fill="currentColor" />
                )}
              </svg>
            </button>
          )}
          <Arrow
            label="Previous"
            onClick={() => setIndex((i) => (i - 1 + slides.length) % slides.length)}
          />
          <Arrow label="Next" next onClick={() => setIndex((i) => (i + 1) % slides.length)} />
        </div>
      )}
    </section>
  )
}

function Arrow({ label, next, onClick }: { label: string; next?: boolean; onClick: () => void }) {
  return (
    <button
      aria-label={label}
      onClick={onClick}
      className="tap relative rounded-full bg-base-950/50 p-2 text-white/70 backdrop-blur-sm transition-colors hover:bg-base-950/80 hover:text-white"
    >
      <svg viewBox="0 0 24 24" fill="none" className={cx('size-5', next && 'rotate-180')}>
        <path
          d="M15 18l-6-6 6-6"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </button>
  )
}
