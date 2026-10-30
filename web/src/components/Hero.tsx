import { useEffect, useId, useRef, useState, type CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import type { DiscoverItem } from '../lib/api'
import { cx, tint } from '../lib/format'
import { useMediaQuery } from '../lib/hooks'
import { PlayIcon } from './PosterCard'
import { StatusMenu } from './StatusMenu'
import { Skeleton } from './ui'

const ROTATE_MS = 9000
const MAX_SLIDES = 5
const SETTLE_MS = 350
const FADE = 'opacity 700ms var(--ease-standard)'
const SLIDE = `transform ${SETTLE_MS}ms var(--ease-standard)`
const HEIGHT = 'min-h-[min(27rem,64svh)] sm:min-h-[clamp(24rem,30vw,30rem)]'
const BLEED = 'left-[calc(50%_-_min(50vw,1425px))] right-[calc(50%_-_min(50vw,1425px))]'

type Slide = { anime: DiscoverItem; rank: number }
type Drag = { dx: number; dir: number; to: number }
type Settle = { other: number; at: number }
type Touch = { id: number; x: number; y: number; t: number; dx: number; axis?: 'x' | 'y' }

export function Hero({ items, loading }: { items?: DiscoverItem[]; loading?: boolean }) {
  const slides = pick(items)
  const n = slides.length
  const [picked, setPicked] = useState(0)
  const [hovered, setHovered] = useState(false)
  const [focused, setFocused] = useState(false)
  const [stopped, setStopped] = useState(false)
  const [drag, setDrag] = useState<Drag | null>(null)
  const [settle, setSettle] = useState<Settle | null>(null)
  const [gone, setGone] = useState<number | null>(null)
  const [fetched, setFetched] = useState<number[]>([])
  const calm = useMediaQuery('(prefers-reduced-motion: reduce)')
  const wide = useMediaQuery('(min-width: 40rem)')
  const touch = useRef<Touch | null>(null)
  const swipedAt = useRef(-Infinity)
  const tabs = useRef<(HTMLButtonElement | null)[]>([])
  const slideId = useId()
  const index = picked < n ? picked : 0
  const rotating = n > 1 && !hovered && !focused && !stopped && !calm && !drag

  useEffect(() => {
    if (!rotating) return
    const timer = window.setTimeout(() => {
      setGone(null)
      setPicked((index + 1) % n)
    }, ROTATE_MS)
    return () => window.clearTimeout(timer)
  }, [rotating, index, n])

  useEffect(() => {
    if (!settle) return
    const timer = window.setTimeout(() => {
      setGone(settle.other)
      setSettle(null)
    }, SETTLE_MS)
    return () => window.clearTimeout(timer)
  }, [settle])

  const near = n ? [0, 1, 2, -1].map((step) => slides[(index + step + n) % n].anime.id) : []
  if (near.some((id) => !fetched.includes(id))) setFetched([...new Set([...fetched, ...near])])

  if (loading) return <HeroSkeleton />
  if (n === 0) return null

  const go = (to: number) => {
    setGone(settle ? settle.other : null)
    setSettle(null)
    setPicked((to + n) % n)
  }

  const onPointerDown = (e: React.PointerEvent<HTMLElement>) => {
    if (e.pointerType === 'mouse' || n < 2) return
    if (settle) {
      setGone(settle.other)
      setSettle(null)
    }
    touch.current = { id: e.pointerId, x: e.clientX, y: e.clientY, t: e.timeStamp, dx: 0 }
  }

  const onPointerMove = (e: React.PointerEvent<HTMLElement>) => {
    const start = touch.current
    if (!start || start.id !== e.pointerId) return
    const dx = e.clientX - start.x
    const dy = e.clientY - start.y
    if (!start.axis) {
      if (Math.hypot(dx, dy) < 10) return
      start.axis = Math.abs(dx) > Math.abs(dy) ? 'x' : 'y'
    }
    if (start.axis !== 'x') return
    const width = e.currentTarget.clientWidth
    start.dx = Math.max(-width, Math.min(width, dx))
    const dir = start.dx < 0 ? 1 : -1
    setDrag({ dx: start.dx, dir, to: (index + dir + n) % n })
  }

  const onPointerEnd = (e: React.PointerEvent<HTMLElement>) => {
    const start = touch.current
    if (!start || start.id !== e.pointerId) return
    touch.current = null
    if (start.axis !== 'x') return
    swipedAt.current = e.timeStamp
    const dir = start.dx < 0 ? 1 : -1
    const to = (index + dir + n) % n
    const far = Math.abs(start.dx) > Math.min(96, e.currentTarget.clientWidth / 5)
    const flick = e.timeStamp - start.t < 300 && Math.abs(start.dx) > 30
    if (e.type === 'pointerup' && (far || flick)) {
      setSettle({ other: index, at: -dir })
      setPicked(to)
    } else {
      setSettle({ other: to, at: dir })
    }
    setGone(null)
    setDrag(null)
  }

  const onTabKey = (e: React.KeyboardEvent) => {
    const moves: Record<string, number | undefined> = { ArrowRight: index + 1, ArrowLeft: index - 1, Home: 0, End: n - 1 }
    const to = moves[e.key]
    if (to === undefined) return
    e.preventDefault()
    go(to)
    tabs.current[(to + n) % n]?.focus()
  }

  const placed = (i: number): CSSProperties => {
    if (drag) {
      if (i === index) return { transform: `translateX(${drag.dx}px)`, transition: 'none' }
      if (i === drag.to) return { transform: `translateX(calc(${drag.dx}px + ${drag.dir * 100}%))`, transition: 'none' }
      return { opacity: 0, transition: 'none' }
    }
    if (settle) {
      if (i === index) return { transform: 'translateX(0)', transition: SLIDE }
      if (i === settle.other) return { transform: `translateX(${settle.at * 100}%)`, transition: SLIDE }
      return { opacity: 0, transition: 'none' }
    }
    if (i === index) return { transition: FADE }
    return { opacity: 0, transition: i === gone ? 'none' : FADE }
  }

  const { anime: active, rank } = slides[index]
  const name = titleOf(active)
  const next = (active.progress ?? 0) + 1
  const done = caughtUp(active)
  const button =
    'press flex h-11 items-center justify-center rounded-xl px-5 text-[15px] whitespace-nowrap transition-[background-color,transform] pointer-coarse:h-12 max-sm:px-4'

  return (
    <section
      aria-roledescription="carousel"
      aria-label="Trending"
      className={cx('relative -mx-4 -mt-4 flex flex-col justify-end sm:-mx-6', HEIGHT, n > 1 && 'touch-pan-y touch-pinch-zoom')}
      onPointerEnter={(e) => {
        if (e.pointerType === 'mouse') setHovered(true)
      }}
      onPointerLeave={(e) => {
        if (e.pointerType === 'mouse') setHovered(false)
      }}
      onFocus={() => setFocused(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setFocused(false)
      }}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerEnd}
      onPointerCancel={onPointerEnd}
      onClickCapture={(e) => {
        if (e.timeStamp - swipedAt.current < 400) {
          e.preventDefault()
          e.stopPropagation()
        }
      }}
    >
      <div
        aria-hidden
        className={cx(
          'absolute inset-y-0 overflow-hidden min-[1700px]:[mask-image:linear-gradient(to_right,transparent,black_6rem,black_calc(100%-6rem),transparent)]',
          BLEED,
        )}
      >
        {slides.map(({ anime }, i) => (
          <div
            key={anime.id}
            style={placed(i)}
            className="absolute inset-0 [mask-image:linear-gradient(to_bottom,black_70%,transparent)] sm:[mask-image:linear-gradient(to_bottom,transparent,black_2rem,black_70%,transparent)]"
          >
            <Art anime={anime} shown={fetched.includes(anime.id)} />
          </div>
        ))}
      </div>

      <div className="relative px-4 pt-24 pb-1 sm:px-6 sm:pt-16 sm:pb-5">
        <div aria-live={rotating ? 'off' : 'polite'}>
          <div
            key={active.id}
            id={slideId}
            role="group"
            aria-roledescription="slide"
            aria-label={`${index + 1} of ${n}`}
            style={
              drag
                ? { transform: `translateX(${drag.dx}px)`, opacity: Math.max(0.2, 1 - Math.abs(drag.dx) / 240), transition: 'none' }
                : undefined
            }
            className="max-w-[42rem] animate-rise transition-[transform,opacity] duration-300 ease-standard"
          >
            <p className="mb-2 text-[13px] font-semibold text-base-300 tabular-nums">Trending #{rank}</p>

            <h1 className={cx('font-display leading-[1.1] font-bold tracking-tight text-balance text-base-100 drop-shadow-lg', titleSize(name))}>
              {name}
            </h1>

            <p className="mt-3 flex flex-wrap items-center gap-x-2 text-sm font-medium text-base-200 tabular-nums">
              {[active.format?.replace('_', ' '), active.episodes ? `${active.episodes} eps` : null, ...(active.Genres ?? []).slice(0, 3)]
                .filter(Boolean)
                .join(' · ')}
              {active.score ? <span className="text-amber-300">★ {(active.score / 10).toFixed(1)}</span> : null}
            </p>
            {active.description && (
              <p className="mt-2 line-clamp-2 max-w-[35rem] text-sm leading-relaxed text-base-300 max-sm:hidden">
                {active.description.replace(/<[^>]*>/g, ' ').replace(/\s+/g, ' ').trim()}
              </p>
            )}

            <div className="mt-5 flex flex-wrap items-center gap-2 sm:mt-6">
              {!done && (
                <Link
                  to={`/watch/${active.id}/${next}`}
                  className={cx(button, 'gap-2 bg-accent-500 font-semibold text-white hover:bg-accent-600 max-sm:flex-1')}
                >
                  <PlayIcon />
                  {active.progress ? `Resume Ep ${next}` : `Play Ep ${next}`}
                </Link>
              )}
              <Link
                to={`/anime/${active.id}`}
                className={cx(
                  button,
                  done
                    ? 'bg-accent-500 font-semibold text-white hover:bg-accent-600 max-sm:flex-1'
                    : 'bg-base-800 font-medium text-base-100 hover:bg-base-750',
                )}
              >
                Details
              </Link>
              <div
                className={
                  wide
                    ? '[&_button]:h-11 pointer-coarse:[&_button]:h-12'
                    : '[&_button]:size-12 [&_button]:rounded-xl [&_button]:bg-base-800 [&_button]:text-base-100 hover:[&_button]:bg-base-750'
                }
              >
                <StatusMenu animeId={active.id} current={active.listStatus ?? null} compact={!wide} quiet />
              </div>
            </div>
          </div>
        </div>

        {n > 1 && (
          <div className="mt-2 flex items-center justify-between gap-2 sm:mt-4">
            <div role="tablist" aria-label="Featured anime" className="flex" onKeyDown={onTabKey}>
              {slides.map(({ anime }, i) => (
                <button
                  key={anime.id}
                  ref={(el) => {
                    tabs.current[i] = el
                  }}
                  role="tab"
                  aria-selected={i === index}
                  aria-controls={slideId}
                  aria-label={titleOf(anime)}
                  tabIndex={i === index ? 0 : -1}
                  onClick={() => go(i)}
                  className="group grid h-8 w-7 place-items-center pointer-coarse:h-11 pointer-coarse:w-8"
                >
                  <span
                    className={cx(
                      'h-1 w-5 rounded-full bg-white transition-[transform,opacity] duration-300 ease-standard',
                      i === index ? 'opacity-100' : 'scale-x-50 opacity-35 group-hover:opacity-60',
                    )}
                  />
                </button>
              ))}
            </div>

            <div className="flex gap-2">
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
              <Arrow label="Previous" onClick={() => go(index - 1)} />
              <Arrow label="Next" next onClick={() => go(index + 1)} />
            </div>
          </div>
        )}
      </div>
    </section>
  )
}

function Art({ anime, shown }: { anime: DiscoverItem; shown: boolean }) {
  const cover = anime.coverLarge ?? anime.cover
  return (
    <>
      {shown &&
        (anime.banner ? (
          <picture>
            <source media="(min-width: 640px)" srcSet={anime.banner} />
            <img src={cover} alt="" decoding="async" className="size-full object-cover object-top sm:object-center" />
          </picture>
        ) : (
          <img
            src={cover}
            alt=""
            decoding="async"
            className="size-full object-cover object-top sm:scale-125 sm:opacity-60 sm:blur-3xl sm:saturate-150"
          />
        ))}
      <div className="absolute inset-0 bg-gradient-to-r from-base-950/90 via-base-950/70 via-35% to-transparent to-60% max-sm:hidden" />
      <div className="absolute inset-0 bg-gradient-to-t from-base-950 via-base-950/75 via-50% to-transparent sm:via-base-950/10" />
      {shown && !anime.banner && (
        <img
          src={cover}
          alt=""
          decoding="async"
          className="absolute inset-y-0 right-0 aspect-[4/3] h-full object-cover object-[center_20%] [mask-image:linear-gradient(to_right,transparent,black_40%)] max-sm:hidden"
        />
      )}
      <div
        className="absolute inset-0"
        style={{
          background: `radial-gradient(60% 70% at 0% 100%, ${tint(anime.color, 0.35) ?? 'rgb(111 92 255 / 0.25)'}, transparent 70%)`,
        }}
      />
    </>
  )
}

function HeroSkeleton() {
  return (
    <div className={cx('relative -mx-4 -mt-4 sm:-mx-6', HEIGHT)}>
      <Skeleton className={cx('absolute inset-y-0 rounded-none', BLEED)} />
    </div>
  )
}

function Arrow({ label, next, onClick }: { label: string; next?: boolean; onClick: () => void }) {
  return (
    <button
      aria-label={label}
      onClick={onClick}
      className="tap relative rounded-full bg-base-950/50 p-2 text-white/70 backdrop-blur-sm transition-colors hover:bg-base-950/80 hover:text-white"
    >
      <svg viewBox="0 0 24 24" fill="none" className={cx('size-5', next && 'rotate-180')} aria-hidden>
        <path d="M15 18l-6-6 6-6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </button>
  )
}

function pick(items?: DiscoverItem[]): Slide[] {
  const ranked = (items ?? []).map((anime, i) => ({ anime, rank: i + 1 })).filter(({ anime }) => anime.cover)
  const watching = ranked.find(
    ({ anime }) =>
      (anime.listStatus === 'CURRENT' || anime.listStatus === 'REPEATING') && (anime.progress ?? 0) > 0 && !caughtUp(anime),
  )
  return (watching ? [watching, ...ranked.filter((slide) => slide !== watching)] : ranked).slice(0, MAX_SLIDES)
}

function caughtUp(anime: DiscoverItem) {
  const aired = anime.nextEpisode ? anime.nextEpisode - 1 : anime.episodes
  return (
    anime.listStatus === 'COMPLETED' ||
    anime.status === 'NOT_YET_RELEASED' ||
    (!!aired && (anime.progress ?? 0) >= aired)
  )
}

function titleOf(anime: DiscoverItem) {
  return anime.english && anime.title.length > 40 && anime.english.length < anime.title.length ? anime.english : anime.title
}

function titleSize(name: string) {
  const n = name.length
  return cx(
    n <= 24 ? 'text-[32px]' : n <= 56 ? 'text-[28px]' : 'text-2xl',
    n <= 24 ? 'sm:text-5xl' : n <= 44 ? 'sm:text-[2.5rem]' : n <= 60 ? 'sm:text-[2.125rem]' : 'sm:text-[1.75rem]',
    n <= 30 ? 'lg:text-[3.5rem]' : n <= 54 ? 'lg:text-[2.75rem]' : n <= 70 ? 'lg:text-[2.25rem]' : 'lg:text-3xl',
  )
}
