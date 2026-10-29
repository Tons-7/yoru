import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { statusLabel } from '../lib/api'
import { cx, personName } from '../lib/format'
import { initials, realImage } from '../lib/people'
import { Skeleton } from './ui'

/** A portrait card: the picture and name open one page, the line under them another. */
export function PortraitCard({
  to,
  image,
  name,
  badge,
  under,
  lines = 2,
}: {
  to: string
  image?: string
  name: string
  badge?: ReactNode
  under?: ReactNode
  /** Show titles get three: a franchise's run the same for two lines. */
  lines?: 2 | 3
}) {
  const picture = realImage(image)
  return (
    <div className="group/portrait min-w-0">
      <Link to={to} tabIndex={-1} aria-hidden className="relative block overflow-hidden rounded-card bg-base-850 shadow-card">
        {picture ? (
          <img
            src={picture}
            alt=""
            loading="lazy"
            className="aspect-[2/3] w-full object-cover transition-transform duration-300 group-hover/portrait:scale-105"
          />
        ) : (
          <div className="grid aspect-[2/3] w-full place-items-center font-display text-2xl font-semibold text-base-600">
            {initials(name)}
          </div>
        )}
        {badge && <span className="pointer-events-none absolute top-1.5 left-1.5">{badge}</span>}
      </Link>
      <Link
        to={to}
        className={cx(
          'mt-2 text-[13px] leading-snug font-medium text-base-100 transition-colors hover:text-white',
          lines === 3 ? 'line-clamp-3' : 'line-clamp-2',
        )}
      >
        {name}
      </Link>
      {under}
    </div>
  )
}

/** A face, or its initials where MyAnimeList has none. */
export function Face({ image, name, className }: { image?: string; name: string; className: string }) {
  const picture = realImage(image)
  return picture ? (
    <img src={picture} alt="" loading="lazy" className={cx('shrink-0 rounded-full object-cover', className)} />
  ) : (
    <span className={cx('grid shrink-0 place-items-center rounded-full bg-base-800 text-xs font-semibold text-base-400', className)}>
      {initials(name)}
    </span>
  )
}

/** On the list: its status on the card, as the Library would say it. */
export function ListBadge({ status }: { status?: string }) {
  if (!status) return null
  return (
    <span className="rounded-md bg-base-950/85 px-1.5 py-0.5 text-[10px] font-semibold text-base-100 ring-1 ring-white/15 backdrop-blur-sm">
      {statusLabel(status)}
    </span>
  )
}

export function PortraitGrid({ children }: { children: ReactNode }) {
  // Three across on a phone, more as the width allows.
  return <div className="grid grid-cols-3 gap-x-3 gap-y-5 sm:grid-cols-[repeat(auto-fill,minmax(7.25rem,1fr))]">{children}</div>
}

/** A section of a person page, its count beside the heading; long ones show the first part until asked. */
export function PeopleSection<T>({
  title,
  items,
  cap = 30,
  children,
}: {
  title: string
  items: T[]
  cap?: number
  children: (items: T[]) => ReactNode
}) {
  const [all, setAll] = useState(false)
  if (items.length === 0) return null
  return (
    <section className="space-y-3">
      <h2 className="flex items-baseline gap-2 section-title">
        {title}
        <span className="text-sm font-normal text-base-500">{items.length}</span>
      </h2>
      {children(all ? items : items.slice(0, cap))}
      {items.length > cap && (
        <button onClick={() => setAll((v) => !v)} className="tap relative text-sm text-accent-300 hover:underline">
          {all ? 'Show fewer' : `Show all ${items.length}`}
        </button>
      )}
    </section>
  )
}

/** A people page's header: portrait, names, a line of facts, and the about text folded to a few lines. */
export function PeopleHeader({
  image,
  name,
  native,
  facts,
  about,
}: {
  image?: string
  name: string
  native?: string
  facts: string[]
  about?: string
}) {
  const [open, setOpen] = useState(false)
  return (
    <header className="flex flex-col gap-5 sm:flex-row sm:items-start">
      <div className="w-36 shrink-0 overflow-hidden rounded-card bg-base-850 shadow-card sm:w-44">
        {realImage(image) ? (
          <img src={image} alt="" className="aspect-[2/3] w-full object-cover" />
        ) : (
          <div className="grid aspect-[2/3] w-full place-items-center font-display text-4xl font-semibold text-base-600">
            {initials(personName(name))}
          </div>
        )}
      </div>
      <div className="min-w-0 flex-1">
        <h1 className="font-display text-3xl font-bold text-white sm:text-4xl">{personName(name)}</h1>
        {native && <p className="mt-1 text-base text-base-400">{native}</p>}
        {facts.length > 0 && <p className="mt-2 text-sm text-base-400">{facts.join(' · ')}</p>}
        {about && (
          <div className="mt-4 max-w-3xl">
            {/* Folded: MyAnimeList's notes run long and can tell what happens. */}
            <p className={cx('text-sm leading-relaxed whitespace-pre-line text-base-300', !open && 'line-clamp-4')}>
              {/* MyAnimeList spaces each fact with a blank line, which spent the folded lines on nothing. */}
              {about.replace(/\n\s*\n+/g, '\n')}
            </p>
            <button onClick={() => setOpen((v) => !v)} className="tap relative mt-1 text-sm text-accent-300 hover:underline">
              {open ? 'Show less' : 'Show more'}
            </button>
          </div>
        )}
      </div>
    </header>
  )
}

export function PeopleSkeleton() {
  return (
    <div className="space-y-8">
      <div className="flex gap-5">
        <Skeleton className="aspect-[2/3] w-36 shrink-0 sm:w-44" />
        <div className="flex-1 space-y-3">
          <Skeleton className="h-9 w-64" />
          <Skeleton className="h-4 w-40" />
          <Skeleton className="h-16 w-full max-w-3xl" />
        </div>
      </div>
      <PortraitGrid>
        {Array.from({ length: 12 }, (_, i) => (
          <Skeleton key={i} className="aspect-[2/3] w-full" />
        ))}
      </PortraitGrid>
    </div>
  )
}
