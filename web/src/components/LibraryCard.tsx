import { useState } from 'react'
import { Link } from 'react-router-dom'
import type { LibraryItem } from '../lib/api'
import { toCard } from '../lib/cards'
import { cx, posterSet, tint } from '../lib/format'
import { statusWord, usePlusOne, useProgress } from '../lib/library'
import { seasonOf, type LibraryProgress } from '../lib/libraryText'
import { HoverInfo } from './HoverInfo'
import { QuickEdit } from './QuickEdit'
import { StatusMenu } from './StatusMenu'

export function LibraryCard({ item, showStatus = false }: { item: LibraryItem; showStatus?: boolean }) {
  const p = useProgress(item)
  const { name, season } = seasonOf(item.title, item.format)
  const href = `/anime/${item.id}`
  const [loaded, setLoaded] = useState(false)
  const tens = Math.round(item.score / 10)

  return (
    <div className="group/card relative">
      <HoverInfo
        anime={{
          ...toCard(item),
          format: item.format,
          status: item.airing,
          listStatus: item.status,
          play: p.next
            ? { to: `/watch/${item.id}/${p.next}`, label: p.watched > 0 ? `Continue ep ${p.next}` : 'Watch ep 1' }
            : undefined,
        }}
      >
        <Link
          to={href}
          viewTransition
          onClick={(e) => {
            const img = e.currentTarget.querySelector('img')
            if (img) img.style.viewTransitionName = 'show-art'
          }}
          aria-label={item.title}
          className="press block rounded-poster focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white"
        >
          <div
            className="relative aspect-[2/3] overflow-hidden rounded-poster bg-base-850"
            style={
              item.color
                ? { backgroundImage: `linear-gradient(to bottom, ${tint(item.color, 0.32)}, ${tint(item.color, 0.12)})` }
                : undefined
            }
          >
            {item.cover ? (
              <img
                ref={(img) => {
                  if (img?.complete) setLoaded(true)
                }}
                src={item.cover}
                srcSet={posterSet(item.cover)}
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
                <span className="text-3xl font-bold text-white/80">{item.title.slice(0, 1)}</span>
                <span className="line-clamp-3 text-xs font-medium text-white/80">{item.title}</span>
              </div>
            )}
            <span aria-hidden className="pointer-events-none absolute inset-0 rounded-poster ring-1 ring-white/[0.08] ring-inset" />
            <PosterBar p={p} />
          </div>
        </Link>
      </HoverInfo>

      <div className="absolute top-1.5 right-1.5">
        <StatusMenu animeId={item.id} current={item.status} compact quiet />
      </div>

      <div className="mt-2 flex items-start gap-0.5">
        <Link to={href} tabIndex={-1} aria-hidden className="min-w-0 flex-1">
          <p
            className="line-clamp-2 text-sm leading-[18px] font-semibold text-base-100 transition-colors group-hover/card:text-white sm:text-[15px] sm:leading-5"
            title={item.title}
          >
            {name}
          </p>
        </Link>
        <QuickEdit item={item} className="-mt-1.5 -mr-1.5" />
      </div>
      {(season || tens > 0) && (
        <p className="mt-0.5 flex items-center gap-2 text-xs text-base-400">
          {season && <span className="min-w-0 truncate">{season}</span>}
          {tens > 0 && (
            <span className="ml-auto shrink-0 text-accent-300 tabular-nums" title="Your score">
              ★ {tens}/10
            </span>
          )}
        </p>
      )}
      {showStatus && <p className="mt-0.5 text-xs font-medium text-base-300">{statusWord(item.status)}</p>}
      <ProgressMeta p={p} className="mt-0.5" />
    </div>
  )
}

function PosterBar({ p }: { p: LibraryProgress }) {
  if (!p.done && p.percent <= 0) return null
  return (
    <span aria-hidden className="pointer-events-none absolute inset-x-0 bottom-0 h-1 bg-base-950/70">
      <span
        className={cx('block h-full', p.done ? 'bg-emerald-400' : 'bg-accent-400')}
        style={{ width: `${p.done ? 100 : p.percent}%` }}
      />
    </span>
  )
}

export function ProgressMeta({ p, prefix, className }: { p: LibraryProgress; prefix?: string; className?: string }) {
  return (
    <p
      className={cx(
        'flex flex-wrap items-center gap-x-1 text-xs tabular-nums sm:text-[13px]',
        p.done ? 'text-emerald-400' : 'text-base-400',
        className,
      )}
    >
      <span className="whitespace-nowrap">
        {prefix && `${prefix} · `}
        {p.lead}
        {p.tail && ' ·'}
      </span>{' '}
      {p.tail &&
        (p.fresh ? (
          <span className="rounded-full bg-accent-500/20 px-1.5 text-xs font-semibold whitespace-nowrap text-accent-300">
            {p.tail}
          </span>
        ) : (
          <span className="whitespace-nowrap">{p.tail}</span>
        ))}
    </p>
  )
}

export function ProgressBar3({ p, className }: { p: LibraryProgress; className?: string }) {
  return (
    <span aria-hidden className={cx('block h-[3px] overflow-hidden rounded-full bg-base-800', className)}>
      <span
        className={cx('block h-full rounded-full', p.done ? 'bg-emerald-400' : 'bg-accent-400')}
        style={{ width: `${p.done ? 100 : p.percent}%` }}
      />
    </span>
  )
}

export function PlusOne({ item, p }: { item: LibraryItem; p: LibraryProgress }) {
  const plusOne = usePlusOne()
  if (p.done) return null
  const waiting = p.waiting
  return (
    <button
      type="button"
      disabled={waiting !== null}
      onClick={() => plusOne(item)}
      aria-label={waiting !== null ? `Episode ${waiting} isn't out yet` : `Mark episode ${p.next} of ${item.title} watched`}
      title={waiting !== null ? `Episode ${waiting} isn't out yet` : `Mark episode ${p.next} watched`}
      className="press grid h-11 min-w-11 shrink-0 place-items-center rounded-full bg-base-800 px-2 text-sm font-semibold text-base-100 tabular-nums transition-colors hover:bg-accent-500 hover:text-white disabled:bg-base-850 disabled:text-base-600 disabled:hover:bg-base-850 sm:h-9 sm:min-w-9 sm:pointer-coarse:h-11 sm:pointer-coarse:min-w-11"
    >
      +1
    </button>
  )
}
