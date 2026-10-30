import { useEffect, useEffectEvent, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, statusLabel, type LibraryItem, type ListStatus } from '../lib/api'
import { cx } from '../lib/format'
import { useMediaQuery } from '../lib/hooks'
import {
  patchItem,
  progressPatch,
  refreshElsewhere,
  sendProgress,
  STATUS_ORDER,
  useRemoveFromLibrary,
} from '../lib/library'
import { airedOf } from '../lib/libraryText'
import { PopSheet } from './PopSheet'
import { ConfirmInline, Switch } from './ui'

export function QuickEdit({
  item,
  large = false,
  className,
}: {
  item: LibraryItem
  large?: boolean
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const close = () => setOpen(false)

  return (
    <>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={`Edit ${item.title}`}
        onClick={() => setOpen((v) => !v)}
        className={cx(
          'tap press relative grid shrink-0 place-items-center rounded-full text-base-400 transition-colors hover:bg-base-800 hover:text-white aria-expanded:bg-base-800 aria-expanded:text-white',
          large ? 'size-11' : 'size-8',
          className,
        )}
      >
        <DotsGlyph />
      </button>
      <PopSheet open={open} onClose={close} title={item.title} anchor={trigger} width={360}>
        {open && <EditBody item={item} onClose={close} />}
      </PopSheet>
    </>
  )
}

function EditBody({ item, onClose }: { item: LibraryItem; onClose: () => void }) {
  const qc = useQueryClient()
  const changed = useRef(false)
  const onChanged = () => {
    changed.current = true
  }
  const work = useRef(new Set<Promise<unknown>>())
  const track = <T,>(p: Promise<T>): Promise<T> => {
    work.current.add(p)
    const done = () => {
      work.current.delete(p)
    }
    p.then(done, done)
    return p
  }
  const wide = useMediaQuery('(min-width: 40rem)')
  const remove = useRemoveFromLibrary()
  const [count, setCount] = useState(item.progress)
  const [error, setError] = useState<string | null>(null)
  const [confirmRewatch, setConfirmRewatch] = useState(false)
  const shown = useRef(item.progress)
  const sent = useRef(item.progress)
  const timer = useRef(0)
  const inFlight = useRef<Promise<unknown>>(Promise.resolve())
  const failures = useRef(0)
  const status = useRef(item.status)
  useEffect(() => {
    status.current = item.status
  }, [item.status])

  const total = item.episodes || null
  const aired = airedOf(item)
  const ceiling = total ?? aired
  const tens = Math.round(item.score / 10)

  const flush = () => {
    window.clearTimeout(timer.current)
    const from = sent.current
    const to = shown.current
    if (from === to) return inFlight.current
    sent.current = to
    onChanged()
    patchItem(qc, item.id, progressPatch({ ...item, progress: from, status: status.current }, to))
    const queuedAt = failures.current
    inFlight.current = inFlight.current
      .then(() => (failures.current === queuedAt ? track(sendProgress(item.id, from, to)) : undefined))
      .then(
        () => refreshElsewhere(qc),
        (e: Error) => {
          failures.current++
          sent.current = from
          shown.current = from
          setCount(from)
          patchItem(qc, item.id, { progress: from, status: status.current })
          setError(`Couldn't save episodes watched: ${e.message}`)
        },
      )
    return inFlight.current
  }
  const leave = useEffectEvent(() => {
    void Promise.allSettled([flush(), ...work.current]).then(() => {
      if (changed.current) void qc.invalidateQueries({ queryKey: ['library'] })
    })
  })
  useEffect(() => () => leave(), [])

  const step = (delta: number) => {
    const next = Math.max(0, Math.min(ceiling ?? Infinity, shown.current + delta))
    shown.current = next
    setCount(next)
    setError(null)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(flush, 600)
  }

  const setStatus = useMutation({
    meta: { inline: true },
    mutationFn: (next: ListStatus) =>
      track(flush().then(() => api.post('/api/status', { animeId: item.id, status: next }))),
    onMutate: (next) => {
      setError(null)
      onChanged()
      const before = { status: item.status, progress: shown.current }
      const restart = next === 'REPEATING' && item.status !== 'REPEATING'
      patchItem(qc, item.id, restart ? { status: next, progress: 0 } : { status: next })
      if (restart) {
        shown.current = sent.current = 0
        setCount(0)
      }
      return { before, restart }
    },
    onSuccess: () => refreshElsewhere(qc),
    onError: (e, _next, ctx) => {
      if (ctx) {
        patchItem(qc, item.id, ctx.before)
        if (ctx.restart) {
          shown.current = sent.current = ctx.before.progress
          setCount(ctx.before.progress)
        }
      }
      setError(`Couldn't change the status: ${e.message}`)
    },
  })

  const setScore = useMutation({
    meta: { inline: true },
    mutationFn: (n: number) => track(api.post('/api/score', { animeId: item.id, score: n * 10 })),
    onMutate: (n) => {
      setError(null)
      onChanged()
      patchItem(qc, item.id, { score: n * 10 })
      return { before: item.score }
    },
    onSuccess: () => refreshElsewhere(qc),
    onError: (e, _n, ctx) => {
      if (ctx) patchItem(qc, item.id, { score: ctx.before })
      setError(`Couldn't save your score: ${e.message}`)
    },
  })

  const setFavourite = useMutation({
    meta: { inline: true },
    mutationFn: (on: boolean) => track(api.post('/api/bookmarks', { animeId: item.id, favourite: on })),
    onMutate: (on) => {
      setError(null)
      onChanged()
      patchItem(qc, item.id, { favourite: on })
    },
    onSuccess: () => refreshElsewhere(qc),
    onError: (e, on) => {
      patchItem(qc, item.id, { favourite: !on })
      setError(`Couldn't change Favourite: ${e.message}`)
    },
  })

  const pickStatus = (next: ListStatus) => {
    if (next === item.status) return
    if (next === 'REPEATING') {
      setConfirmRewatch(true)
      return
    }
    setConfirmRewatch(false)
    setStatus.mutate(next)
  }

  const summary = [
    statusLabel(item.status) || 'Not in a list',
    total ? `${count} of ${total}` : count > 0 ? `${count} watched` : null,
  ]
    .filter(Boolean)
    .join(' · ')
  const atLast = !!total && count >= total
  const blocked = ceiling !== null && count >= ceiling && !atLast

  return (
    <div className="space-y-4 pb-1 sm:p-2">
      <div className="flex items-center gap-3">
        {item.cover ? (
          <img src={item.cover} alt="" className="aspect-[2/3] w-12 shrink-0 rounded-md object-cover" />
        ) : (
          <div className="aspect-[2/3] w-12 shrink-0 rounded-md bg-base-850" />
        )}
        <div className="min-w-0">
          {wide && <p className="line-clamp-2 text-[15px] font-semibold text-base-100">{item.title}</p>}
          <p className="text-[13px] text-base-400 tabular-nums">{summary}</p>
        </div>
      </div>

      <section aria-label="Episodes watched">
        <p className="mb-1.5 text-[13px] font-medium text-base-400">Episodes watched</p>
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => step(-1)}
            disabled={count <= 0}
            aria-label="One episode fewer"
            className="press grid size-12 shrink-0 place-items-center rounded-xl bg-base-800 text-xl text-base-100 transition-colors hover:bg-base-700 disabled:opacity-40"
          >
            −
          </button>
          <p className="min-w-0 flex-1 text-center text-base font-semibold text-white tabular-nums" aria-live="polite">
            {total ? `${count} of ${total}` : aired !== null ? `${count} of ${aired} out` : `${count}`}
          </p>
          <button
            type="button"
            onClick={() => step(1)}
            disabled={ceiling !== null && count >= ceiling}
            aria-label="One episode more"
            className="press grid size-12 shrink-0 place-items-center rounded-xl bg-base-800 text-xl text-base-100 transition-colors hover:bg-base-700 disabled:opacity-40"
          >
            +
          </button>
        </div>
        {atLast && item.status !== 'COMPLETED' && (
          <p className="mt-1.5 text-[13px] text-emerald-400">The last episode: this marks it Completed.</p>
        )}
        {blocked && <p className="mt-1.5 text-[13px] text-base-400">Episode {count + 1} isn't out yet.</p>}
      </section>

      <section aria-label="Status">
        <p className="mb-1.5 text-[13px] font-medium text-base-400">Status</p>
        <div className="flex flex-wrap gap-1.5">
          {STATUS_ORDER.map((s) => (
            <button
              key={s}
              type="button"
              aria-pressed={item.status === s}
              disabled={setStatus.isPending}
              onClick={() => pickStatus(s)}
              className={cx(
                'h-9 rounded-full px-3 text-sm font-medium transition-colors pointer-coarse:h-11',
                item.status === s ? 'bg-accent-500 text-white' : 'bg-base-800 text-base-200 hover:bg-base-700',
              )}
            >
              {statusLabel(s)}
            </button>
          ))}
        </div>
        {confirmRewatch && (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <ConfirmInline
              note="Starting a rewatch resets episodes watched to 0."
              label="Rewatch"
              tone="accent"
              size="sm"
              pending={setStatus.isPending}
              onConfirm={() => {
                setConfirmRewatch(false)
                setStatus.mutate('REPEATING')
              }}
              onCancel={() => setConfirmRewatch(false)}
            />
          </div>
        )}
      </section>

      <section aria-label="Your score">
        <div className="mb-1.5 flex items-center justify-between">
          <p className="text-[13px] font-medium text-base-400">Your score</p>
          {tens > 0 && (
            <button
              type="button"
              onClick={() => setScore.mutate(0)}
              className="tap relative text-[13px] font-medium text-accent-300 hover:underline"
            >
              Clear score
            </button>
          )}
        </div>
        <div className="grid grid-cols-5 gap-1.5">
          {Array.from({ length: 10 }, (_, i) => i + 1).map((n) => (
            <button
              key={n}
              type="button"
              aria-pressed={n === tens}
              aria-label={`Score ${n}`}
              onClick={() => setScore.mutate(n === tens ? 0 : n)}
              className={cx(
                'h-10 rounded-lg text-sm font-semibold tabular-nums transition-colors pointer-coarse:h-11',
                n === tens ? 'bg-accent-500 text-white' : 'bg-base-800 text-base-100 hover:bg-base-700',
              )}
            >
              {n}
            </button>
          ))}
        </div>
      </section>

      <label className="flex min-h-11 cursor-pointer items-center justify-between gap-3 text-[15px] text-base-100">
        Favourite
        <Switch
          on={!!item.favourite}
          onChange={(on) => setFavourite.mutate(on)}
          disabled={setFavourite.isPending}
          aria-label="Favourite"
        />
      </label>

      {error && <p className="text-[13px] text-red-300">{error}</p>}

      <button
        type="button"
        onClick={() => {
          void flush()
          onClose()
          remove(item)
        }}
        className="press flex min-h-11 w-full items-center rounded-lg text-left text-[15px] font-medium text-red-300 transition-colors hover:text-red-200"
      >
        {item.status ? 'Remove from library' : 'Remove from favourites'}
      </button>
    </div>
  )
}

export function DotsGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" fill="currentColor" aria-hidden>
      <circle cx="5" cy="12" r="1.8" />
      <circle cx="12" cy="12" r="1.8" />
      <circle cx="19" cy="12" r="1.8" />
    </svg>
  )
}
