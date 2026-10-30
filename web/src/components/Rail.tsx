import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { cx } from '../lib/format'
import { buttonClass } from '../lib/ui'
import { Skeleton } from './ui'

function useScrollState(track: React.RefObject<HTMLDivElement | null>) {
  const [state, setState] = useState({
    scrollable: false,
    atStart: true,
    atEnd: false,
    progress: 0,
    visible: 1,
  })

  useEffect(() => {
    const el = track.current
    if (!el) return
    const measure = () => {
      const max = el.scrollWidth - el.clientWidth
      const next = {
        scrollable: max > 1,
        atStart: el.scrollLeft <= 1,
        atEnd: el.scrollLeft >= max - 1,
        progress: max > 1 ? el.scrollLeft / max : 0,
        visible: el.scrollWidth > 0 ? el.clientWidth / el.scrollWidth : 1,
      }
      setState((prev) =>
        (Object.keys(next) as (keyof typeof next)[]).every((k) => prev[k] === next[k]) ? prev : next,
      )
    }
    el.addEventListener('scroll', measure, { passive: true })

    const resize = new ResizeObserver(measure)
    resize.observe(el)
    const swap = new MutationObserver(measure)
    swap.observe(el, { childList: true, subtree: true })

    return () => {
      el.removeEventListener('scroll', measure)
      resize.disconnect()
      swap.disconnect()
    }
  }, [track])

  return state
}

export function RailHeading({ title, to }: { title: string; to?: string }) {
  return (
    <h2 className="section-title min-w-0">
      {to ? (
        <Link
          to={to}
          className="group/heading tap relative -my-2 flex min-h-11 items-center gap-1 transition-colors hover:text-base-200"
        >
          {title}
          <ChevronGlyph className="size-4 shrink-0 text-base-500 transition-colors group-hover/heading:text-base-300" />
        </Link>
      ) : (
        title
      )}
    </h2>
  )
}

export function Rail({
  title,
  to,
  meta,
  action,
  folded = false,
  onUnfold,
  children,
}: {
  title: string
  to?: string
  meta?: ReactNode
  action?: ReactNode
  folded?: boolean
  onUnfold?: () => void
  children: ReactNode
}) {
  const track = useRef<HTMLDivElement>(null)
  const { scrollable, atStart, atEnd, progress, visible } = useScrollState(track)

  const scrollBy = (direction: 1 | -1) => {
    const el = track.current
    if (!el) return
    el.scrollBy({ left: direction * Math.round(el.clientWidth * 0.8), behavior: 'smooth' })
  }

  return (
    <section className="group/rail">
      <div className="mb-3 flex items-center justify-between gap-3">
        <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-0.5">
          <RailHeading title={title} to={to} />
          {meta && !folded && <span className="text-sm text-base-500 tabular-nums">{meta}</span>}
        </div>
        {folded ? (
          onUnfold && (
            <button
              type="button"
              onClick={onUnfold}
              aria-expanded={false}
              aria-label={`Show ${title}`}
              className={buttonClass('ghost', 'sm')}
            >
              Show
            </button>
          )
        ) : (
          <div className="flex shrink-0 items-center gap-2">
            {action}
            {scrollable && (
              <div className="hidden gap-1.5 md:flex">
                <ArrowButton direction={-1} disabled={atStart} onClick={() => scrollBy(-1)} />
                <ArrowButton direction={1} disabled={atEnd} onClick={() => scrollBy(1)} />
              </div>
            )}
          </div>
        )}
      </div>

      <div
        ref={track}
        data-rail-track
        className={cx('no-scrollbar -mx-1 flex snap-x snap-mandatory gap-3 overflow-x-auto scroll-smooth px-1 pb-1', folded && 'hidden')}
      >
        {children}
      </div>

      {scrollable && !folded && (
        <div className="mt-1.5 h-0.5 overflow-hidden rounded-full bg-base-850 pointer-coarse:hidden" aria-hidden>
          <div
            className="h-full rounded-full bg-base-700"
            style={{
              width: `${Math.max(visible * 100, 8)}%`,
              marginInlineStart: `${progress * (100 - Math.max(visible * 100, 8))}%`,
            }}
          />
        </div>
      )}
    </section>
  )
}

export type RailSize = 'poster' | 'wide' | 'still'

const WIDTHS: Record<RailSize, string> = {
  poster: 'w-[38vw] sm:w-40 md:w-44 lg:w-[11.5rem]',
  wide: 'w-[16.25rem] lg:w-[18.75rem]',
  still: 'w-[12.5rem] lg:w-[15rem]',
}

export function RailItem({ children, size = 'poster' }: { children: ReactNode; size?: RailSize }) {
  return <div className={cx('shrink-0 snap-start', WIDTHS[size])}>{children}</div>
}

export function RailFailed({ title, to, retry }: { title: string; to?: string; retry: () => void }) {
  return (
    <section>
      <div className="mb-3">
        <RailHeading title={title} to={to} />
      </div>
      <p className="flex flex-wrap items-center gap-3 text-sm text-base-400">
        <span>Couldn't load this.</span>
        <button type="button" onClick={retry} className={buttonClass('secondary', 'sm')}>
          Try again
        </button>
      </p>
    </section>
  )
}

export function RailSkeleton({ title, to, size = 'poster' }: { title: string; to?: string; size?: RailSize }) {
  return (
    <section>
      <div className="mb-3">
        <RailHeading title={title} to={to} />
      </div>
      <div className="flex gap-3 overflow-hidden">
        {Array.from({ length: size === 'poster' ? 16 : 10 }, (_, i) => (
          <div key={i} className={cx('shrink-0', WIDTHS[size])}>
            <Skeleton className={cx('w-full', size === 'poster' ? 'aspect-[2/3]' : 'aspect-video rounded-card')} />
            <Skeleton className="mt-2 h-4 w-3/4" />
          </div>
        ))}
      </div>
    </section>
  )
}

function ArrowButton({
  direction,
  disabled,
  onClick,
}: {
  direction: 1 | -1
  disabled?: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={direction === 1 ? 'Scroll right' : 'Scroll left'}
      className="grid size-11 place-items-center rounded-full bg-base-800 text-base-200 transition-colors hover:bg-base-750 hover:text-white disabled:pointer-events-none disabled:bg-base-900 disabled:text-base-600"
    >
      <ChevronGlyph className={cx('size-5', direction === -1 && 'rotate-180')} />
    </button>
  )
}

function ChevronGlyph({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <path d="m9 5 7 7-7 7" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}
