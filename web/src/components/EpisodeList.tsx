import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { api, hasFiller, type Episode } from '../lib/api'
import { airTime, clockTime, cx, relativeTime } from '../lib/format'
import {
  refreshDownloads,
  refreshListState,
  useAccess,
  useDownloadQueue,
  useDownloads,
  useNow,
  usePrefs,
  useSetPref,
  useUpNextEdit,
} from '../lib/queries'
import { isUnaired } from '../lib/episodes'
import { cannotSave, saveEpisodes, savedKey, useSaved } from '../lib/offline'
import { buttonClass } from '../lib/ui'
import { Badge, Button, ConfirmInline, SearchInput } from './ui'
import { MoreMenu, type MoreItem } from './MoreMenu'
import { useInUpNext } from '../lib/upnext'
import { conceal, reveal, useRevealed } from '../lib/spoilers'
import { useAnchorBox, useDismiss, useModalFocus } from '../lib/hooks'

const PAGE_SIZE = 50
const DAY = 86_400_000

export function EpisodeKind({ episode }: { episode: Episode }) {
  const filler = hasFiller(episode)
  if (!filler && !episode.recap) return null

  return (
    <span className="inline-flex gap-1 align-middle">
      {filler && <Badge tone="filler">{episode.filler === 'mixed' ? 'Mixed' : 'Filler'}</Badge>}
      {episode.recap && <Badge tone="recap">Recap</Badge>}
    </span>
  )
}

function airDay(unix: number) {
  const date = new Date(unix * 1000)
  return date.toLocaleDateString(undefined, {
    day: 'numeric',
    month: 'short',
    year: date.getFullYear() === new Date().getFullYear() ? undefined : 'numeric',
  })
}

function outWhen(unix: number | undefined, now: number, withTime = true) {
  if (!unix) return 'Not out yet'
  if (unix * 1000 - now < DAY) return `Out ${relativeTime(unix)}`
  const day = new Date(unix * 1000).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
  return withTime ? `Out ${day}, ${airTime(unix)}` : `Out ${day}`
}

type Show = 'all' | 'unwatched' | 'downloaded'
type Art = 'still' | 'tile'

export function EpisodeList({
  animeId,
  episodes,
  current,
  compact,
  selectable,
  upNext,
}: {
  animeId: number
  episodes: Episode[]
  current?: number
  compact?: boolean
  selectable?: boolean
  upNext?: number
}) {
  const now = useNow()
  const [term, setTerm] = useState('')
  const [show, setShow] = useState<Show>('all')
  const [page, setPage] = useState(0)

  const [selecting, setSelecting] = useState(false)
  const [picked, setPicked] = useState<Set<number>>(() => new Set())
  const [note, setNote] = useState<string | null>(null)
  const [why, setWhy] = useState(false)
  const [confirmAll, setConfirmAll] = useState(false)
  const qc = useQueryClient()
  const queue = useDownloadQueue()
  const downloads = useDownloads()
  const access = useAccess()
  const guest = !!access.data && !access.data.host
  const { saved, jobs } = useSaved()
  const savedHere = useMemo(() => new Set(saved.map((e) => e.key)), [saved])
  const savingHere = useMemo(() => new Set(jobs.map((j) => savedKey(j.animeId, j.episode))), [jobs])
  const prefs = usePrefs()
  const autoDeletes = prefs.data?.effective['cache.autodelete'] === 'now'
  const busy = useMemo(() => {
    const set = new Set<number>()
    for (const q of queue.data?.items ?? []) if (q.animeId === animeId && q.state !== 'failed') set.add(q.episode)
    for (const d of downloads.data?.items ?? []) {
      if (d.animeId === animeId && d.percent < 100) for (const e of d.episodes) set.add(Number(e))
    }
    return set
  }, [queue.data, downloads.data, animeId])
  const failedEps = useMemo(
    () => new Set((queue.data?.items ?? []).filter((q) => q.animeId === animeId && q.state === 'failed').map((q) => q.episode)),
    [queue.data, animeId],
  )
  const showPrefs = usePrefs(animeId)
  const setPref = useSetPref()
  const showsAll = showPrefs.data?.overrides?.['display.spoilers'] === 'off'
  const anyHidden = episodes.some((e) => e.spoiler)
  const setSpoilers = (value: string) =>
    setPref.mutate(
      { key: 'display.spoilers', value, animeId },
      {
        onSuccess: () => {
          void qc.invalidateQueries({ queryKey: ['episodes', animeId] })
          void qc.invalidateQueries({ queryKey: ['upnext'] })
        },
      },
    )
  const listed = useInUpNext(animeId)
  const upNextEdit = useUpNextEdit()
  const toggleUpNext = (episode: number) =>
    listed.has(episode) ? upNextEdit.remove.mutate({ animeId, episode }) : upNextEdit.add.mutate({ animeId, episode })

  const download = useMutation({
    meta: { inline: true },
    mutationFn: (numbers: number[]) =>
      api.post<{ queued: number }>('/api/download/episodes', { animeId, episodes: numbers }),
    onSuccess: (res, numbers) => {
      refreshDownloads(qc)
      setNote(
        res.queued === 0
          ? 'Already queued or downloaded'
          : `Queued ${res.queued} of ${numbers.length} episode${numbers.length === 1 ? '' : 's'}`,
      )
      stopSelecting()
    },
  })
  const markAll = useMutation({
    mutationFn: (episode: number) => api.post('/api/watched', { animeId, episode, watched: true }),
    onSettled: () => {
      setConfirmAll(false)
      refreshListState(qc)
    },
  })
  const stopSelecting = () => {
    setSelecting(false)
    setPicked(new Set())
  }
  const toggle = (number: number) =>
    setPicked((prev) => {
      const next = new Set(prev)
      if (next.has(number)) next.delete(number)
      else next.add(number)
      return next
    })

  const { aired, later } = useMemo(() => {
    const out: Episode[] = []
    const notOut = new Set<number>()
    for (const e of episodes) {
      if (isUnaired(e, now) || (notOut.size > 0 && !e.airDate)) {
        notOut.add(e.number)
        if (notOut.size > 1) continue
      }
      out.push(e)
    }
    return { aired: out, later: notOut }
  }, [episodes, now])

  const released = aired.filter((e) => !later.has(e.number))
  const watchedCount = released.filter((e) => e.watched).length
  const downloadedCount = released.filter((e) => e.onDisk).length
  const art: Art = aired.some((e) => e.still) ? 'still' : 'tile'
  const allPlanned = released.length > 0 && released.every((e) => e.planned)
  const planned = released.some((e) => e.planned)

  const filtered = useMemo(() => {
    let list = aired
    if (show === 'unwatched') list = list.filter((e) => !e.watched)
    if (show === 'downloaded') list = list.filter((e) => e.onDisk)
    const q = term.trim().toLowerCase()
    if (!q) return list
    const asNumber = Number(q)
    if (Number.isInteger(asNumber) && asNumber > 0) {
      return list.filter((e) => String(e.display || e.number).includes(q))
    }
    return list.filter((e) =>
      `${e.titleEn ?? ''} ${e.titleJa ?? ''} ${e.overview ?? ''}`.toLowerCase().includes(q),
    )
  }, [aired, term, show])

  const firstUnwatched = useMemo(() => aired.find((e) => !e.watched && !later.has(e.number)), [aired, later])
  const lastWatched = useMemo(() => [...aired].reverse().find((e) => e.watched), [aired])
  const lastOut = released[released.length - 1]
  const canMarkAll = !!lastOut && !lastOut.watched && !!firstUnwatched && firstUnwatched.number < lastOut.number
  const allFrom = firstUnwatched ? firstUnwatched.display || firstUnwatched.number : 0
  const allTo = lastOut ? lastOut.display || lastOut.number : 0

  const pages = Math.ceil(filtered.length / PAGE_SIZE)
  const at = Math.min(page, Math.max(0, pages - 1))
  const paged = pages > 1 ? filtered.slice(at * PAGE_SIZE, (at + 1) * PAGE_SIZE) : filtered

  const [placedFor, setPlacedFor] = useState<string>()
  const place = current ?? upNext
  const placeKey = current != null ? `${animeId} playing ${current}` : `${animeId} next`
  if (place && placedFor !== placeKey) {
    const index = filtered.findIndex((e) => e.number === place)
    if (index >= 0) {
      setPlacedFor(placeKey)
      setPage(Math.floor(index / PAGE_SIZE))
    }
  }
  const search = (value: string) => {
    setTerm(value)
    setPage(0)
  }
  const filterBy = (value: Show) => {
    setShow(value)
    setPage(0)
  }

  const searchable = episodes.length > 12
  const total = episodes.length
  const done = released.length > 0 && watchedCount === released.length
  const filterOptions: { value: Show; label: string; hidden?: boolean }[] = [
    { value: 'all', label: 'All' },
    { value: 'unwatched', label: 'Unwatched', hidden: watchedCount === 0 },
    { value: 'downloaded', label: 'Downloaded', hidden: downloadedCount === 0 },
  ]
  const filterCount = filterOptions.filter((o) => !o.hidden).length
  const spoilerSwitch = anyHidden || showsAll
  const tools = filterCount > 1 || spoilerSwitch || (canMarkAll && !selecting) || searchable

  const progress = (
    <p className="min-w-0 text-sm text-base-400">
      {released.length === 0 ? (
        'None out yet'
      ) : done ? (
        <span className="font-medium text-emerald-300">
          {released.length === 1 ? 'Watched' : `All ${released.length} watched`}
        </span>
      ) : (
        <>
          <span className="font-semibold text-base-100 tabular-nums">{watchedCount}</span> of{' '}
          <span className="tabular-nums">{released.length}</span>
          {total > released.length ? ' out' : ''} watched
        </>
      )}
      {total > released.length && <span className="text-base-500"> · {total} in total</span>}
      {downloadedCount > 0 && <span className="text-base-500"> · {downloadedCount} downloaded</span>}
    </p>
  )

  return (
    <div className="space-y-3">
      {!compact && (
        <div className="space-y-2.5">
          {selecting ? (
            <>
              <div className="flex min-h-11 items-center justify-between gap-3">
                <span className="text-sm text-base-300 tabular-nums">{picked.size} selected</span>
                <Button
                  size="sm"
                  onClick={() =>
                    setPicked((prev) => {
                      const next = new Set(prev)
                      for (const e of paged) if (!later.has(e.number)) next.add(e.number)
                      return next
                    })
                  }
                >
                  Select page
                </Button>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Button variant="ghost" size="sm" disabled={download.isPending} onClick={stopSelecting}>
                  Cancel
                </Button>
                <span className="ml-auto flex flex-wrap items-center justify-end gap-2">
                  {guest && (
                    <Button
                      size="sm"
                      disabled={picked.size === 0}
                      onClick={() => {
                        const reason = cannotSave()
                        if (!reason) saveEpisodes([...picked].sort((a, b) => a - b).map((episode) => ({ animeId, episode })))
                        setNote(reason ?? `Saving ${picked.size} to this device: see Downloads, On this device`)
                        stopSelecting()
                      }}
                    >
                      Save to this device
                    </Button>
                  )}
                  <Button
                    variant="primary"
                    size="sm"
                    onClick={() => download.mutate([...picked].sort((a, b) => a - b))}
                    disabled={picked.size === 0 || download.isPending}
                  >
                    {download.isPending ? 'Queuing…' : 'Download selected'}
                  </Button>
                </span>
              </div>
            </>
          ) : (
            <>
              <div className="flex min-h-11 items-center justify-between gap-3">
                {progress}
                {selectable && (
                  <Button
                    size="sm"
                    onClick={() => {
                      setNote(null)
                      setConfirmAll(false)
                      setSelecting(true)
                    }}
                  >
                    Select<span className="sr-only"> episodes…</span>
                  </Button>
                )}
              </div>
              {released.length > 0 && (
                <div aria-hidden className="h-1 overflow-hidden rounded-full bg-base-800">
                  <div
                    className={cx(
                      'h-full rounded-full transition-[width] duration-500',
                      done ? 'bg-emerald-400' : 'bg-accent-500',
                    )}
                    style={{ width: `${(watchedCount / released.length) * 100}%` }}
                  />
                </div>
              )}
            </>
          )}

          {download.isError && (
            <Failure
              what="Couldn't queue them"
              error={download.error}
              pending={download.isPending}
              onRetry={() => download.mutate(download.variables ?? [])}
            />
          )}
          {note && (
            <p role="status" className="text-[13px] text-base-400">
              {note}
            </p>
          )}

          {tools && (
            <div className="flex flex-wrap items-center gap-2">
              {filterCount > 1 && <Filters value={show} onChange={filterBy} options={filterOptions} />}
              {spoilerSwitch && (
                <Button variant="ghost" size="sm" onClick={() => setSpoilers(showsAll ? '' : 'off')} disabled={setPref.isPending}>
                  {showsAll ? 'Hide spoilers again' : 'Show all titles'}
                </Button>
              )}
              {canMarkAll && !selecting && (
                <Button variant="ghost" size="sm" onClick={() => setConfirmAll(true)} disabled={confirmAll || markAll.isPending}>
                  Mark all watched
                </Button>
              )}
              {searchable && (
                <SearchInput
                  value={term}
                  onChange={(e) => search(e.target.value)}
                  placeholder="Find an episode"
                  aria-label="Search episodes"
                  className="w-full sm:ml-auto sm:w-56"
                />
              )}
            </div>
          )}

          {confirmAll && canMarkAll && (
            <div className="flex flex-wrap items-center justify-end gap-2 rounded-card bg-base-900 p-2.5">
              <span className="mr-auto text-[13px] text-base-300">
                Mark {allFrom} to {allTo} watched?
              </span>
              <ConfirmInline
                tone="accent"
                size="sm"
                label={`Mark ${allFrom}–${allTo}`}
                note={autoDeletes && released.some((e) => !e.watched && e.onDisk) ? 'Deletes their files' : undefined}
                onConfirm={() => markAll.mutate(lastOut.number)}
                onCancel={() => setConfirmAll(false)}
                pending={markAll.isPending}
              />
            </div>
          )}
          {markAll.isError && !confirmAll && (
            <Failure
              what="Couldn't mark them"
              error={markAll.error}
              pending={markAll.isPending}
              onRetry={() => markAll.mutate(markAll.variables ?? lastOut.number)}
            />
          )}
        </div>
      )}

      {compact && searchable && (
        <SearchInput
          value={term}
          onChange={(e) => search(e.target.value)}
          placeholder={`Search ${episodes.length} episodes`}
          aria-label="Search episodes"
        />
      )}

      {planned && (
        <div className="text-[13px] leading-5 text-base-500">
          <p>
            No episode details yet, so numbers may not match every release.{' '}
            <button
              type="button"
              aria-expanded={why}
              onClick={() => setWhy((v) => !v)}
              className="tap relative font-medium text-accent-300 hover:underline"
            >
              Why
            </button>
          </p>
          {why && (
            <p className="mt-1 text-base-400">
              Yoru knows only how many episodes the show has, so it numbers them itself, and a release may count them
              differently.
              {!allPlanned && ' Episodes Yoru hasn\'t matched to a release yet say "Not found yet".'}
            </p>
          )}
        </div>
      )}

      {pages > 1 && (
        <div className="flex items-center justify-center gap-1.5">
          <Button size="sm" disabled={at === 0} onClick={() => setPage(at - 1)}>
            Previous
          </Button>
          <RangeMenu
            page={at}
            pages={pages}
            total={filtered.length}
            rangeFor={(n) => {
              const slice = filtered.slice(n * PAGE_SIZE, (n + 1) * PAGE_SIZE)
              const first = slice[0]
              const last = slice[slice.length - 1]
              return first && last ? `${first.display} to ${last.display}` : ''
            }}
            onPick={setPage}
          />
          <Button size="sm" disabled={at >= pages - 1} onClick={() => setPage(at + 1)}>
            Next
          </Button>
        </div>
      )}

      <ul
        className={cx(
          'surface',
          compact ? 'max-h-[70vh] overflow-y-auto overscroll-contain scrollbar-thin' : 'overflow-hidden',
        )}
      >
        {paged.map((episode) => (
          <EpisodeRow
            key={episode.number}
            animeId={animeId}
            episode={episode}
            art={art}
            unaired={later.has(episode.number)}
            out={later.has(episode.number) ? outWhen(episode.airDate, now) : ''}
            outDay={later.has(episode.number) ? outWhen(episode.airDate, now, false) : ''}
            watched={episode.watched}
            marksFrom={
              firstUnwatched && firstUnwatched.number < episode.number && !later.has(episode.number)
                ? firstUnwatched.display || firstUnwatched.number
                : undefined
            }
            unmarksTo={
              lastWatched && lastWatched.number > episode.number ? lastWatched.display || lastWatched.number : undefined
            }
            active={episode.number === current}
            upNext={episode.number === upNext}
            compact={compact}
            selected={picked.has(episode.number)}
            onToggle={selecting ? () => toggle(episode.number) : undefined}
            queued={busy.has(episode.number)}
            failed={failedEps.has(episode.number)}
            listed={listed.has(episode.number)}
            onUpNext={() => toggleUpNext(episode.number)}
            notFound={!allPlanned && !!episode.planned && !episode.onDisk && !episode.local}
            guest={guest}
            here={
              savedHere.has(savedKey(animeId, episode.number))
                ? 'saved'
                : savingHere.has(savedKey(animeId, episode.number))
                  ? 'saving'
                  : undefined
            }
            autoDeletes={autoDeletes}
          />
        ))}
        {paged.length === 0 && (
          <li className="p-6 text-center text-sm text-base-500">
            {show === 'unwatched' ? 'All caught up.' : show === 'downloaded' ? 'Nothing downloaded yet.' : 'No episode matches that.'}
          </li>
        )}
      </ul>
    </div>
  )
}

function Filters({
  value,
  onChange,
  options,
}: {
  value: Show
  onChange: (v: Show) => void
  options: { value: Show; label: string; hidden?: boolean }[]
}) {
  return (
    <div role="tablist" aria-label="Show episodes" className="inline-flex gap-0.5 rounded-xl bg-base-900 p-1">
      {options
        .filter((o) => !o.hidden)
        .map((o) => (
          <button
            key={o.value}
            role="tab"
            aria-selected={value === o.value}
            onClick={() => onChange(o.value)}
            className={cx(
              'h-8 rounded-lg px-3 text-[13px] font-medium transition-colors pointer-coarse:min-h-11',
              value === o.value ? 'bg-base-750 text-base-100' : 'text-base-400 hover:text-base-100',
            )}
          >
            {o.label}
          </button>
        ))}
    </div>
  )
}

function RangeMenu({
  page,
  pages,
  total,
  rangeFor,
  onPick,
}: {
  page: number
  pages: number
  total: number
  rangeFor: (page: number) => string
  onPick: (page: number) => void
}) {
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const trigger = useRef<HTMLButtonElement>(null)

  const box = useAnchorBox(trigger, open)
  const listRef = useRef<HTMLDivElement>(null)
  useModalFocus(listRef, open)

  return (
    <div className="relative" ref={ref}>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={cx(buttonClass('ghost', 'sm'), 'min-w-36 tabular-nums')}
      >
        {rangeFor(page)}
        <span className="text-base-500">of {total}</span>
        <svg viewBox="0 0 24 24" className="size-3 opacity-70" aria-hidden>
          <path d="m6 9 6 6 6-6" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
        </svg>
      </button>

      {open && box &&
        createPortal(
          <div
            ref={listRef}
            role="listbox"
            data-portal-menu
            style={{
              top: Math.min(box.bottom + 6, window.innerHeight - 300),
              left: Math.max(8, Math.min(box.left, window.innerWidth - 184)),
            }}
            className="fixed z-50 max-h-72 w-44 animate-rise overflow-y-auto rounded-card bg-base-750 p-1 shadow-panel scrollbar-thin"
          >
            {Array.from({ length: pages }, (_, n) => (
              <button
                key={n}
                role="option"
                aria-selected={n === page}
                autoFocus={n === page}
                onClick={() => {
                  onPick(n)
                  setOpen(false)
                }}
                className={cx(
                  'w-full rounded-lg px-3 py-2 text-left text-sm tabular-nums transition-colors pointer-coarse:min-h-11',
                  n === page ? 'bg-accent-500 text-white' : 'text-base-100 hover:bg-base-700',
                )}
              >
                {rangeFor(n)}
              </button>
            ))}
          </div>,
          document.body,
        )}
    </div>
  )
}

const ICON = {
  play: 'M7 4.8v14.4a.8.8 0 0 0 1.2.7l11.5-7.2a.8.8 0 0 0 0-1.4L8.2 4.1a.8.8 0 0 0-1.2.7Z',
  check: 'm5 12.5 4.5 4.5L19 7',
  undo: 'M9 14 4 9l5-5M4 9h10.5a5.5 5.5 0 0 1 0 11H11',
  listAdd: 'M4 7h11M4 12h11M4 17h7M18 14v6m-3-3h6',
  listRemove: 'M4 7h11M4 12h11M4 17h7m4 0h6',
  download: 'M12 4v11m-4.5-4.5L12 15l4.5-4.5M5 20h14',
  clock: 'M12 7.5V12l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z',
  phone: 'M8.5 3h7A1.5 1.5 0 0 1 17 4.5v15a1.5 1.5 0 0 1-1.5 1.5h-7A1.5 1.5 0 0 1 7 19.5v-15A1.5 1.5 0 0 1 8.5 3ZM11 18h2',
  folder: 'M3.5 7.5A1.5 1.5 0 0 1 5 6h4l2 2h8a1.5 1.5 0 0 1 1.5 1.5v8A1.5 1.5 0 0 1 19 19H5a1.5 1.5 0 0 1-1.5-1.5Z',
  eye: 'M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12ZM12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z',
  eyeOff:
    'M3 3l18 18M10.6 5.6c.5-.1.9-.1 1.4-.1 6 0 9.5 6.5 9.5 6.5a17 17 0 0 1-2.7 3.5M6.6 6.6C3.9 8.4 2.5 12 2.5 12S6 18.5 12 18.5c1.6 0 3.1-.4 4.4-1.1M9.9 9.9a3 3 0 0 0 4.2 4.2',
  chat: 'M20 12a8 8 0 0 1-11.8 7L4 20l1-4.2A8 8 0 1 1 20 12Z',
}

function Glyph({ d, className }: { d: string; className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      className={className}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
    >
      <path d={d} />
    </svg>
  )
}

function EpisodeRow({
  animeId,
  episode,
  art,
  watched,
  marksFrom,
  unmarksTo,
  active,
  upNext,
  compact,
  selected,
  onToggle,
  queued,
  failed,
  unaired,
  out,
  outDay,
  listed,
  onUpNext,
  notFound,
  guest,
  here,
  autoDeletes,
}: {
  animeId: number
  episode: Episode
  art: Art
  watched: boolean
  listed: boolean
  onUpNext: () => void
  unaired: boolean
  out: string
  outDay: string
  marksFrom?: number
  unmarksTo?: number
  active: boolean
  upNext?: boolean
  compact?: boolean
  selected?: boolean
  onToggle?: () => void
  queued?: boolean
  failed?: boolean
  notFound: boolean
  guest: boolean
  here?: 'saved' | 'saving'
  autoDeletes: boolean
}) {
  const row = useRef<HTMLLIElement>(null)

  useEffect(() => {
    if (!active || !compact) return

    const id = requestAnimationFrame(() => {
      const el = row.current
      const list = el?.closest('ul')
      if (!el || !list) return
      const delta = el.getBoundingClientRect().top - list.getBoundingClientRect().top
      list.scrollTop += delta - list.clientHeight / 2 + el.clientHeight / 2
    })
    return () => cancelAnimationFrame(id)
  }, [active, compact])

  const number = episode.display || episode.number
  const isRevealed = useRevealed()
  const hidden = !!episode.spoiler && !active && !isRevealed(animeId, episode.number)
  const named = !!(episode.titleEn || episode.titleJa)
  const title = hidden ? 'Title hidden' : episode.titleEn || episode.titleJa || `Episode ${number}`
  const resumeAt = episode.resumable && typeof episode.position === 'number' && !unaired ? episode.position : undefined
  const played =
    resumeAt !== undefined && episode.duration ? Math.min(100, (resumeAt / episode.duration) * 100) : watched ? 100 : 0
  const pill = !compact

  const qc = useQueryClient()
  const fetch = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<{ queued: boolean }>('/api/download', { animeId, episode: number }),
    onSuccess: () => refreshDownloads(qc),
  })
  const resetFetch = fetch.reset
  const wasQueued = useRef(queued)
  useEffect(() => {
    if (failed || (wasQueued.current && !queued)) resetFetch()
    wasQueued.current = queued
  }, [failed, queued, resetFetch])
  const isQueued = (fetch.isSuccess && !failed) || queued
  const canDownload = !episode.onDisk && !episode.local

  const [confirming, setConfirming] = useState(false)
  const mark = useMutation({
    mutationFn: (next: boolean) =>
      api.post('/api/watched', { animeId, episode: episode.number, watched: next }),
    onSettled: () => {
      setConfirming(false)
      refreshListState(qc)
    },
  })

  const spans = watched ? unmarksTo !== undefined : marksFrom !== undefined
  const deletes = !watched && !!episode.onDisk && autoDeletes
  const tickLabel = watched
    ? unmarksTo !== undefined
      ? `Mark ${number} to ${unmarksTo} not watched`
      : 'Mark not watched'
    : marksFrom !== undefined
      ? `Mark ${marksFrom} to ${number} watched`
      : 'Mark watched'

  const digits = String(number).length
  const picture =
    art === 'tile' ? (
      <span
        className={cx(
          'relative grid size-11 shrink-0 place-items-center overflow-hidden rounded-poster font-bold tabular-nums',
          digits <= 2 ? 'text-xl' : digits === 3 ? 'text-base' : 'text-sm',
          active
            ? 'bg-accent-500/20 text-accent-300'
            : watched
              ? 'bg-emerald-400/15 text-emerald-300'
              : unaired
                ? 'bg-base-800 text-base-500'
                : 'bg-base-800 text-base-300',
        )}
      >
        {active ? <PlayingGlyph /> : number}
        {played > 0 && played < 100 && (
          <span className="absolute inset-x-0 bottom-0 h-0.5 bg-base-950/60">
            <span className="block h-full bg-accent-500" style={{ width: `${played}%` }} />
          </span>
        )}
      </span>
    ) : (
      <span
        className={cx(
          'relative aspect-video shrink-0 overflow-hidden rounded-poster bg-base-800',
          compact ? 'w-24' : 'w-28 sm:w-40',
        )}
      >
        {episode.still ? (
          <img
            src={episode.still}
            alt=""
            loading="lazy"
            decoding="async"
            className={cx(
              'size-full object-cover',
              hidden && 'scale-125 blur-xl',
              watched && !active && 'opacity-60',
            )}
          />
        ) : (
          <span
            className={cx(
              'grid size-full place-items-center font-bold tabular-nums',
              digits <= 2 ? 'text-xl' : 'text-base',
              watched ? 'text-emerald-300' : 'text-base-400',
            )}
          >
            {number}
          </span>
        )}
        {active ? (
          <span className="absolute inset-0 grid place-items-center bg-base-950/60 text-accent-300">
            <PlayingGlyph />
          </span>
        ) : (
          !unaired &&
          !onToggle && (
            <span className="absolute inset-0 grid place-items-center">
              <span className="grid size-7 place-items-center rounded-full bg-base-950/70 text-white">
                <PlayGlyph />
              </span>
            </span>
          )
        )}
        {played > 0 && played < 100 && (
          <span className="absolute inset-x-0 bottom-0 h-1 bg-base-950/60">
            <span className="block h-full bg-accent-500" style={{ width: `${played}%` }} />
          </span>
        )}
      </span>
    )

  const state: { node: ReactNode; className?: string } | null = unaired
    ? { node: out, className: 'font-medium text-sky-300' }
    : active
      ? { node: 'Now playing', className: 'font-medium text-accent-300' }
      : resumeAt !== undefined
        ? { node: `Resume ${clockTime(resumeAt)}`, className: 'font-medium text-accent-300' }
        : upNext && !listed
          ? { node: 'Next', className: 'font-medium text-accent-300' }
          : watched && !listed
            ? { node: 'Watched', className: cx('font-medium text-emerald-300', pill && 'sm:hidden') }
            : null

  const facts: { node: ReactNode; className?: string }[] = []
  if (state) facts.push(state)
  if (listed) facts.push({ node: 'In Up next', className: 'font-medium text-accent-300' })
  if (!unaired) {
    if (episode.onDisk) facts.push({ node: <Fact icon={ICON.download} word="Downloaded" compact={compact} /> })
    else if (episode.local) facts.push({ node: <Fact icon={ICON.folder} word="In your library" compact={compact} /> })
    else if (isQueued) facts.push({ node: <Fact icon={ICON.clock} word="Queued for download" compact={compact} /> })
    if (here) {
      facts.push({
        node: <Fact icon={ICON.phone} word={here === 'saved' ? 'On this device' : 'Saving to this device'} compact={compact} />,
      })
    }
    if (episode.runtime) facts.push({ node: `${episode.runtime} min` })
    if (!compact && episode.airDate) facts.push({ node: airDay(episode.airDate) })
    if (episode.score) {
      facts.push({
        node: <span title="MyAnimeList members' rating, out of 5">★ {episode.score.toFixed(1)}</span>,
      })
    }
    if (notFound) facts.push({ node: 'Not found yet' })
  }
  if (hasFiller(episode) || episode.recap) facts.push({ node: <EpisodeKind episode={episode} /> })

  const text = (
    <span className="block min-w-0 flex-1">
      <span
        className={cx(
          'text-sm font-medium',
          art === 'still' && !compact ? 'line-clamp-2' : 'line-clamp-1',
          unaired || hidden ? 'text-base-400' : watched && !active ? 'text-base-100/70' : 'text-base-100',
        )}
      >
        {art === 'still' && (named || hidden) && <span className="tabular-nums">{number}. </span>}
        {title}
      </span>
      {facts.length > 0 && (
        <span className="mt-0.5 block truncate text-[13px] text-base-500">
          {facts.map((fact, i) => (
            <span key={i} className={fact.className}>
              {fact.node}
              {i < facts.length - 1 && <span className="font-normal text-base-500"> · </span>}
            </span>
          ))}
        </span>
      )}
      {!compact &&
        episode.overview &&
        (hidden ? (
          <span className="mt-0.5 hidden text-[13px] text-base-500 sm:block">
            Title and synopsis hidden to avoid spoilers
          </span>
        ) : (
          <span
            className={cx(
              'mt-0.5 hidden text-[13px] leading-5 text-base-400',
              art === 'still' ? 'sm:line-clamp-2' : 'sm:line-clamp-1',
            )}
          >
            {episode.overview}
          </span>
        ))}
    </span>
  )

  const stateWords = [
    active ? 'playing' : upNext ? 'next' : '',
    watched ? 'watched' : '',
    resumeAt !== undefined ? `resume at ${clockTime(resumeAt)}` : '',
    episode.onDisk ? 'downloaded' : episode.local ? 'in your library' : isQueued ? 'queued for download' : '',
    here === 'saved' ? 'on this device' : here === 'saving' ? 'saving to this device' : '',
    listed ? 'in Up next' : '',
    episode.runtime ? `${episode.runtime} minutes` : '',
    episode.score ? `rated ${episode.score.toFixed(1)} of 5` : '',
    notFound ? 'not found yet' : '',
    hasFiller(episode) ? (episode.filler === 'mixed' ? 'part filler' : 'filler') : '',
    episode.recap ? 'recap' : '',
  ].filter(Boolean)

  const markFromMenu = () => (deletes ? setConfirming(true) : mark.mutate(!watched))
  const saveReason = cannotSave()
  const groups: (MoreItem | false)[][] = onToggle
    ? []
    : [
        [
          !active && {
            label: 'Play',
            to: `/watch/${animeId}/${episode.number}`,
            icon: <Glyph d={ICON.play} />,
            disabled: unaired,
            note: unaired ? <span className="text-sky-300">{outDay}</span> : undefined,
          },
        ],
        [
          {
            label: tickLabel,
            run: markFromMenu,
            icon: <Glyph d={watched ? ICON.undo : ICON.check} />,
            disabled: unaired || mark.isPending,
          },
        ],
        [
          {
            label: listed ? 'Remove from Up next' : 'Add to Up next',
            run: onUpNext,
            icon: <Glyph d={listed ? ICON.listRemove : ICON.listAdd} />,
            disabled: unaired && !listed,
          },
          canDownload && {
            label: isQueued ? 'Queued for download' : fetch.isPending ? 'Queuing…' : 'Download',
            run: () => fetch.mutate(),
            icon: <Glyph d={isQueued ? ICON.clock : ICON.download} />,
            disabled: unaired || fetch.isPending || isQueued,
            note: unaired ? <span className="text-sky-300">{outDay}</span> : undefined,
          },
          guest &&
            (here
              ? {
                  label: here === 'saved' ? 'On this device' : 'Saving to this device',
                  to: '/saved',
                  icon: <Glyph d={ICON.phone} />,
                }
              : {
                  label: 'Save to this device',
                  run: () => saveEpisodes([{ animeId, episode: episode.number }]),
                  icon: <Glyph d={ICON.phone} />,
                  disabled: unaired || !!saveReason,
                  note: unaired ? undefined : saveReason,
                }),
        ],
        [
          !!episode.spoiler &&
            !active && {
              label: hidden ? 'Show title and synopsis' : 'Hide title and synopsis',
              run: () => (hidden ? reveal : conceal)(animeId, episode.number),
              icon: <Glyph d={hidden ? ICON.eye : ICON.eyeOff} />,
            },
          !!episode.forumUrl && {
            label: 'Discuss on MyAnimeList',
            href: episode.forumUrl,
            icon: <Glyph d={ICON.chat} />,
          },
        ],
      ]
  const more = groups.flatMap((group) =>
    group.filter((item): item is MoreItem => !!item).map((item, i) => (i === 0 ? { ...item, divider: true } : item)),
  )
  const menu = more.length > 0 && (
    <MoreMenu
      label={`More for episode ${number}`}
      menuLabel={`Episode ${number}`}
      title={`Episode ${number}`}
      detail={unaired ? out : named && !hidden ? title : undefined}
      items={more}
      className="size-11 rounded-full sm:size-10"
    />
  )

  const toggleButton = !onToggle && (
    <button
      type="button"
      onClick={() => (confirming ? setConfirming(false) : spans || deletes ? setConfirming(true) : mark.mutate(!watched))}
      disabled={mark.isPending || unaired}
      aria-label={tickLabel}
      title={unaired ? out : tickLabel}
      className={cx(
        'tap press relative flex h-11 min-w-11 shrink-0 items-center justify-center gap-1.5 rounded-full transition-[background-color,color,transform] sm:h-10 sm:min-w-10',
        watched
          ? cx('text-emerald-400 hover:bg-emerald-400/10', pill && 'sm:bg-emerald-400/15 sm:pr-3.5 sm:pl-2.5 sm:hover:bg-emerald-400/25')
          : 'text-base-500 hover:bg-base-800 hover:text-base-200 disabled:text-base-600 disabled:hover:bg-transparent',
      )}
    >
      {watched ? <DoneGlyph /> : <RingGlyph />}
      {pill && watched && <span className="hidden text-[13px] font-medium text-emerald-300 sm:inline">Watched</span>}
    </button>
  )

  const controls = !onToggle && (
    <span className="flex shrink-0 items-center">
      {toggleButton}
      {menu}
    </span>
  )

  const pad = art === 'tile' ? (compact ? 'py-1.5' : 'py-2.5 sm:py-1.5') : 'py-2'
  const lead = 'flex min-w-0 flex-1 items-center gap-3 pr-2 pl-3'
  const inset = onToggle
    ? 'not-first:before:left-3'
    : art === 'tile'
      ? 'not-first:before:left-[4.25rem]'
      : compact
        ? 'not-first:before:left-[7.5rem]'
        : 'not-first:before:left-[8.5rem] sm:not-first:before:left-[11.5rem]'

  const failures = !onToggle && (confirming || mark.isError || fetch.isError) && (
    <div className="space-y-2 px-3 pb-2.5">
      {confirming && (
        <div className="flex flex-wrap items-center justify-end gap-2">
          <span className="mr-auto text-[13px] text-base-300">{tickLabel}?</span>
          <ConfirmInline
            tone="accent"
            size="sm"
            label={
              watched
                ? unmarksTo !== undefined
                  ? `Unmark ${number}–${unmarksTo}`
                  : `Unmark ${number}`
                : marksFrom !== undefined
                  ? `Mark ${marksFrom}–${number}`
                  : `Mark ${number}`
            }
            note={deletes ? 'Deletes the file' : undefined}
            onConfirm={() => mark.mutate(!watched)}
            onCancel={() => setConfirming(false)}
            pending={mark.isPending}
          />
        </div>
      )}
      {mark.isError && !confirming && (
        <Failure
          what="Couldn't mark"
          error={mark.error}
          pending={mark.isPending}
          onRetry={() => mark.mutate(mark.variables ?? !watched)}
        />
      )}
      {fetch.isError && (
        <Failure what="Couldn't queue" error={fetch.error} pending={fetch.isPending} onRetry={() => fetch.mutate()} />
      )}
    </div>
  )

  return (
    <li
      ref={row}
      className={cx(
        'relative not-first:before:absolute not-first:before:top-0 not-first:before:right-0 not-first:before:h-px not-first:before:bg-base-800',
        inset,
        active
          ? 'bg-accent-500/10 shadow-[inset_3px_0_0_var(--color-accent-500)]'
          : onToggle && selected
            ? 'bg-accent-500/10'
            : upNext && !unaired
              ? 'bg-base-850'
              : !unaired && 'hover:bg-base-850',
      )}
    >
      {unaired ? (
        <div className="flex items-center pr-1 sm:pr-2">
          <div className={cx(lead, pad)}>
            {picture}
            {text}
          </div>
          {controls}
        </div>
      ) : onToggle ? (
        <button
          type="button"
          onClick={onToggle}
          aria-pressed={selected}
          className={cx('flex w-full items-center gap-3 px-3 text-left', pad)}
        >
          <span
            aria-hidden
            className={cx(
              'grid size-6 shrink-0 place-items-center rounded-md border-2 text-white',
              selected ? 'border-accent-500 bg-accent-500' : 'border-base-500',
            )}
          >
            {selected && <Glyph d={ICON.check} className="size-4" />}
          </span>
          {picture}
          {text}
        </button>
      ) : (
        <div className="flex items-center pr-1 sm:pr-2">
          <Link
            to={`/watch/${animeId}/${episode.number}`}
            aria-current={active ? 'true' : undefined}
            aria-label={[hidden ? `Episode ${number}` : `Episode ${number}: ${title}`, ...stateWords].join(', ')}
            className={cx(
              lead,
              pad,
              'rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-accent-400 focus-visible:ring-inset',
            )}
          >
            {picture}
            {text}
          </Link>
          {controls}
        </div>
      )}
      {failures}
    </li>
  )
}

function Fact({ icon, word, compact }: { icon: string; word: string; compact?: boolean }) {
  return (
    <span className="inline-flex items-center gap-1 align-bottom text-base-400">
      <Glyph d={icon} className="size-3.5" />
      <span className={compact ? 'sr-only' : 'max-sm:sr-only'}>{word}</span>
    </span>
  )
}

function Failure({
  what,
  error,
  pending,
  onRetry,
}: {
  what: string
  error: unknown
  pending: boolean
  onRetry: () => void
}) {
  return (
    <div className="flex items-center justify-end gap-2">
      <p role="alert" className="mr-auto min-w-0 text-[13px] text-red-300">
        {what}: {(error as Error).message}
      </p>
      <Button size="sm" onClick={onRetry} disabled={pending} aria-label={`${what}. Retry`}>
        Retry
      </Button>
    </div>
  )
}

function RingGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-6 shrink-0" aria-hidden>
      <circle cx="12" cy="12" r="9.25" fill="none" stroke="currentColor" strokeWidth="1.75" />
    </svg>
  )
}

function DoneGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-6 shrink-0" aria-hidden>
      <circle cx="12" cy="12" r="10" fill="currentColor" />
      <path
        d="m7.5 12.5 3 3 6-6.5"
        fill="none"
        stroke="var(--color-base-950)"
        strokeWidth="2.4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function PlayGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-3.5 translate-x-px" aria-hidden>
      <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />
    </svg>
  )
}

function PlayingGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" fill="currentColor" aria-hidden>
      <rect x="5" y="10" width="3" height="9" rx="1" />
      <rect x="10.5" y="5" width="3" height="14" rx="1" />
      <rect x="16" y="12" width="3" height="7" rx="1" />
    </svg>
  )
}
