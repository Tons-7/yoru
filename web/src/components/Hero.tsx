import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import type { DiscoverItem } from '../lib/api'
import { cx, tint } from '../lib/format'
import { useMediaQuery } from '../lib/hooks'
import { PlayIcon } from './PosterCard'
import { StatusMenu } from './StatusMenu'
import { Skeleton } from './ui'

const ROTATE_MS = 9000
const PRELOAD = 2

export function Hero({ items, loading }: { items?: DiscoverItem[]; loading?: boolean }) {
  const slides = (items ?? []).filter((a) => a.cover).slice(0, 10)
  const [picked, setIndex] = useState(0)
  const [paused, setPaused] = useState(false)
  const [stopped, setStopped] = useState(false)
  const calm = useMediaQuery('(prefers-reduced-motion: reduce)')
  const timer = useRef<number | undefined>(undefined)
  const index = picked < slides.length ? picked : 0

  useEffect(() => {
    if (paused || stopped || calm || slides.length < 2) return
    timer.current = window.setInterval(
      () => setIndex((i) => (i + 1) % slides.length),
      ROTATE_MS,
    )
    return () => window.clearInterval(timer.current)
  }, [paused, stopped, calm, slides.length])

  const [fetched, setFetched] = useState<number[]>([])
  const ahead = slides.length ? Array.from({ length: PRELOAD + 1 }, (_, step) => (index + step) % slides.length) : []
  if (ahead.some((i) => !fetched.includes(i))) setFetched([...new Set([...fetched, ...ahead])])

  if (loading) {
    return <Skeleton className="-mx-4 -mt-4 h-[27rem] rounded-none sm:-mx-6 sm:h-[clamp(24rem,32vw,32rem)]" />
  }
  if (slides.length === 0) return null

  const active = slides[index]
  const aired = active.nextEpisode ? active.nextEpisode - 1 : active.episodes
  const caughtUp =
    active.listStatus === 'COMPLETED' ||
    active.status === 'NOT_YET_RELEASED' ||
    (!!aired && (active.progress ?? 0) >= aired)

  return (
    <section
      className="relative -mx-4 -mt-4 h-[27rem] overflow-hidden sm:-mx-6 sm:h-[clamp(24rem,32vw,32rem)]"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setPaused(false)
      }}
      aria-roledescription="carousel"
    >
      <div className="absolute inset-0 min-[1700px]:[mask-image:linear-gradient(to_right,transparent,black_6rem,black_calc(100%-6rem),transparent)]">
        {slides.map((anime, i) => (
          <div
            key={anime.id}
            aria-hidden={i !== index}
            className={cx(
              'absolute inset-0 transition-opacity duration-700 [mask-image:linear-gradient(to_bottom,black_70%,transparent)]',
              i === index ? 'opacity-100' : 'pointer-events-none opacity-0',
            )}
          >
            {fetched.includes(i) &&
              (anime.banner ? (
                <picture>
                  <source media="(min-width: 640px)" srcSet={anime.banner} />
                  <img
                    src={anime.coverLarge ?? anime.cover}
                    alt=""
                    decoding="async"
                    className={cx(
                      'size-full object-cover object-top transition-transform duration-[9000ms] ease-linear sm:object-center',
                      i === index ? 'sm:scale-[1.03]' : 'scale-100',
                    )}
                  />
                </picture>
              ) : (
                <img
                  src={anime.coverLarge ?? anime.cover}
                  alt=""
                  decoding="async"
                  className="size-full object-cover object-top sm:scale-125 sm:opacity-40 sm:blur-3xl"
                />
              ))}
            <div className="absolute inset-0 bg-gradient-to-r from-base-950 via-base-950/70 to-transparent max-sm:hidden" />
            <div className="absolute inset-0 bg-gradient-to-t from-base-950 via-base-950/75 via-50% to-transparent sm:via-base-950/10" />
            {fetched.includes(i) && !anime.banner && (
              <img
                src={anime.coverLarge ?? anime.cover}
                alt=""
                decoding="async"
                className={cx(
                  'absolute inset-y-0 right-0 aspect-square h-full object-cover object-[center_20%] transition-transform duration-[9000ms] ease-linear [mask-image:linear-gradient(to_right,transparent,black_45%)] max-sm:hidden',
                  i === index ? 'scale-[1.03]' : 'scale-100',
                )}
              />
            )}
            <div
              className="absolute inset-0"
              style={{
                background: `radial-gradient(60% 70% at 0% 100%, ${tint(anime.color, 0.35) ?? 'rgb(111 92 255 / 0.25)'}, transparent 70%)`,
              }}
            />
          </div>
        ))}
      </div>

      <div className="relative mx-auto flex h-full max-w-[1600px] flex-col justify-end px-4 pb-5 sm:px-6 sm:pb-8 lg:px-10">
        <div className="max-w-2xl">
          <p className="mb-2 text-xs font-semibold tracking-[0.16em] text-accent-300 uppercase">Trending #{index + 1}</p>

          <h1 className="line-clamp-2 font-display text-3xl leading-[1.05] font-bold tracking-tight text-white text-balance drop-shadow-lg sm:text-5xl lg:text-[3.5rem]">
            {active.title}
          </h1>

          <p className="mt-3 flex flex-wrap items-center gap-x-2 text-sm font-medium text-base-200">
            {[active.format?.replace('_', ' '), active.episodes ? `${active.episodes} eps` : null, ...(active.Genres ?? []).slice(0, 3)]
              .filter(Boolean)
              .join(' · ')}
            {active.score ? <span className="text-amber-300">★ {(active.score / 10).toFixed(1)}</span> : null}
          </p>
          {active.description && (
            <p className="mt-2 line-clamp-2 max-w-xl text-sm leading-relaxed text-base-300 max-sm:hidden">
              {active.description.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim()}
            </p>
          )}

          <div className="mt-6 flex items-center gap-2">
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
            <StatusMenu key={active.id} animeId={active.id} current={active.listStatus ?? null} compact />
          </div>
        </div>

        <div className="mt-4 flex" role="tablist" aria-label="Featured anime">
          {slides.map((slide, i) => (
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
