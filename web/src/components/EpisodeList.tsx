import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { api, hasFiller, type Episode } from '../lib/api'
import { clockTime, cx, relativeTime } from '../lib/format'
import {
  refreshDownloads,
  refreshListState,
  useDownloadQueue,
  useDownloads,
  useNow,
  usePrefs,
  useSetPref,
  useUpNextEdit,
} from '../lib/queries'
import { isUnaired } from '../lib/episodes'
import { Badge, ConfirmInline, inputClass, TextInput } from './ui'
import { ListCheckGlyph, ListPlusGlyph } from './UpNext'
import { useInUpNext } from '../lib/upnext'
import { reveal, useRevealed } from '../lib/spoilers'
import { useAnchorBox, useDismiss, useModalFocus } from '../lib/hooks'

const PAGE_SIZE = 50

/** Only filler and recap get a badge (an episode can be both); a canon badge would bury them. */
export function EpisodeKind({ episode }: { episode: Episode }) {
  const filler = hasFiller(episode)
  if (!filler && !episode.recap) return null

  return (
    <span className="flex gap-1">
      {filler && <Badge tone="filler">{episode.filler === 'mixed' ? 'Mixed' : 'Filler'}</Badge>}
      {episode.recap && <Badge tone="recap">Recap</Badge>}
    </span>
  )
}

function airDay(unix?: number) {
  if (!unix) return ''
  return new Date(unix * 1000).toLocaleDateString(undefined, {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
  })
}

type Show = 'all' | 'unwatched' | 'downloaded'

export function EpisodeList({
  animeId,
  episodes,
  current,
  compact,
  cover,
  selectable,
  upNext,
}: {
  animeId: number
  episodes: Episode[]
  current?: number
  compact?: boolean
  cover?: string | null
  /** Offer a select mode that queues a hand-picked set of episodes. */
  selectable?: boolean
  /** The episode the page's main button would play. */
  upNext?: number
}) {
  const now = useNow()
  const [term, setTerm] = useState('')
  const [show, setShow] = useState<Show>('all')
  const [page, setPage] = useState(0)

  const [selecting, setSelecting] = useState(false)
  const [picked, setPicked] = useState<Set<number>>(() => new Set())
  const [note, setNote] = useState<string | null>(null)
  const qc = useQueryClient()
  // Episodes already queued or arriving: a second Download would only queue them again.
  const queue = useDownloadQueue()
  const downloads = useDownloads()
  const busy = useMemo(() => {
    const set = new Set<number>()
    // A failed entry is not busy: its Download button retries it.
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
  // Spoilers for this show alone: shown all at once, or hidden again under the rule Settings sets.
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

  // Only the next unaired episode gets a row: it answers "when is the new one".
  const aired = useMemo(() => {
    const out: Episode[] = []
    let shownUpcoming = false
    for (const e of episodes) {
      if (isUnaired(e, now)) {
        if (shownUpcoming) continue
        shownUpcoming = true
      }
      out.push(e)
    }
    return out
  }, [episodes, now])

  const released = aired.filter((e) => !isUnaired(e, now))
  const watchedCount = released.filter((e) => e.watched).length
  const downloadedCount = released.filter((e) => e.onDisk).length

  const filtered = useMemo(() => {
    let list = aired
    if (show === 'unwatched') list = list.filter((e) => !e.watched)
    if (show === 'downloaded') list = list.filter((e) => e.onDisk)
    const q = term.trim().toLowerCase()
    if (!q) return list
    // A bare number should find that episode, not every title containing it.
    const asNumber = Number(q)
    if (Number.isInteger(asNumber) && asNumber > 0) {
      return list.filter((e) => String(e.display || e.number).includes(q))
    }
    return list.filter((e) =>
      `${e.titleEn ?? ''} ${e.titleJa ?? ''} ${e.overview ?? ''}`.toLowerCase().includes(q),
    )
  }, [aired, term, show])

  // Ticking N marks everything before it too; the first unwatched one says how far back that reaches.
  const firstUnwatched = useMemo(() => aired.find((e) => !e.watched && !isUnaired(e, now)), [aired, now])
  // Unticking N unwatches everything after it too, up to the last watched one.
  const lastWatched = useMemo(() => [...aired].reverse().find((e) => e.watched), [aired])

  const pages = Math.ceil(filtered.length / PAGE_SIZE)
  // Clamped: a narrower filter can leave fewer pages than the one open.
  const at = Math.min(page, Math.max(0, pages - 1))
  const paged = pages > 1 ? filtered.slice(at * PAGE_SIZE, (at + 1) * PAGE_SIZE) : filtered

  // Open on the page holding the episode being watched, once per episode (a refetch would undo manual
  // paging), set during render so the first paint is already that page.
  const [placedFor, setPlacedFor] = useState<number>()
  if (current && placedFor !== current) {
    const index = filtered.findIndex((e) => e.number === current)
    if (index >= 0) {
      setPlacedFor(current)
      setPage(Math.floor(index / PAGE_SIZE))
    }
  }
  // A new search or filter starts at its first page, from the change itself rather than an effect after it.
  const search = (value: string) => {
    setTerm(value)
    setPage(0)
  }
  const filterBy = (value: Show) => {
    setShow(value)
    setPage(0)
  }

  const searchable = episodes.length > 12

  // Only their count is known, so these rows look different and say so.
  const planned = episodes.length > 0 && episodes.every((e) => e.planned)

  return (
    <div className="space-y-3">
      {planned && (
        <p className="rounded-xl border border-base-800 bg-base-900/50 px-3 py-2 text-xs text-base-400">
          No episode data for this show, so episodes are numbered from its count and may differ from
          the releases. Episodes found in releases lose the "expected" mark.
        </p>
      )}

      {!compact && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          {/* Where you are in the show, before any row. */}
          <div className="min-w-40 flex-1">
            <p className="text-sm text-base-300">
              <span className="font-semibold text-white tabular-nums">{watchedCount}</span>
              <span className="text-base-500"> / {released.length} watched</span>
              {downloadedCount > 0 && (
                <span className="text-base-500"> · {downloadedCount} downloaded</span>
              )}
            </p>
            <div className="mt-1.5 h-1 max-w-64 overflow-hidden rounded-full bg-base-800">
              <div
                className="h-full rounded-full bg-gradient-to-r from-accent-500 to-accent-400 transition-[width] duration-500"
                style={{ width: `${released.length ? (watchedCount / released.length) * 100 : 0}%` }}
              />
            </div>
          </div>

          <Filters
            value={show}
            onChange={filterBy}
            options={[
              { value: 'all', label: 'All' },
              { value: 'unwatched', label: 'Unwatched', hidden: watchedCount === 0 },
              { value: 'downloaded', label: 'Downloaded', hidden: downloadedCount === 0 },
            ]}
          />

          {(anyHidden || showsAll) && (
            <button
              type="button"
              onClick={() => setSpoilers(showsAll ? '' : 'off')}
              disabled={setPref.isPending}
              className="rounded-lg px-2.5 py-1 text-xs text-base-400 ring-1 ring-base-700 transition-colors hover:bg-base-800 hover:text-white disabled:opacity-50 pointer-coarse:min-h-11"
            >
              {showsAll ? 'Hide spoilers again' : 'Show all titles'}
            </button>
          )}

          {searchable && (
            <div className="relative w-full sm:w-56">
              <SearchGlyph />
              <input
                value={term}
                onChange={(e) => search(e.target.value)}
                placeholder={`Find an episode`}
                aria-label="Search episodes"
                className={cx(inputClass, 'w-full pl-8')}
              />
            </div>
          )}
        </div>
      )}

      {compact && searchable && (
        <TextInput
          value={term}
          onChange={(e) => search(e.target.value)}
          placeholder={`Search ${episodes.length} episodes…`}
          aria-label="Search episodes"
          className="w-full"
        />
      )}

      {selectable && (
        <div className="flex flex-wrap items-center justify-between gap-2">
          {selecting ? (
            <>
              <span className="text-xs tabular-nums text-base-400">{picked.size} selected</span>
              {/* Apart on touch: Cancel beside Download selected was an easy mis-tap. */}
              <div className="flex items-center gap-1.5 pointer-coarse:gap-3">
                <PageStep
                  label="Select page"
                  disabled={false}
                  onClick={() =>
                    setPicked((prev) => {
                      const next = new Set(prev)
                      for (const e of paged) if (!isUnaired(e, now)) next.add(e.number)
                      return next
                    })
                  }
                />
                <PageStep label="Cancel" disabled={download.isPending} onClick={stopSelecting} />
                <button
                  onClick={() => download.mutate([...picked].sort((a, b) => a - b))}
                  disabled={picked.size === 0 || download.isPending}
                  className="rounded-lg bg-accent-500 px-3 py-1 text-xs font-semibold text-white transition-colors hover:bg-accent-600 disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:px-3.5"
                >
                  {download.isPending ? 'Queuing…' : 'Download selected'}
                </button>
              </div>
            </>
          ) : (
            <>
              <span className="text-xs text-base-500">{note}</span>
              <PageStep
                label="Select episodes…"
                disabled={false}
                onClick={() => {
                  setNote(null)
                  setSelecting(true)
                }}
              />
            </>
          )}
          {download.isError && (
            <p className="w-full text-xs text-recap">{(download.error as Error).message}</p>
          )}
        </div>
      )}

      {/* Above the list: paging from the bottom means scrolling the whole page. */}
      {pages > 1 && (
        <div className="flex items-center justify-center gap-1.5">
          <PageStep
            label="Previous"
            disabled={at === 0}
            onClick={() => setPage(at - 1)}
          />
          {/* A menu of ranges: a thousand episodes is twenty pages to step through. */}
          <RangeMenu
            page={at}
            pages={pages}
            total={filtered.length}
            rangeFor={(n) => {
              const slice = filtered.slice(n * PAGE_SIZE, (n + 1) * PAGE_SIZE)
              const first = slice[0]
              const last = slice[slice.length - 1]
              return first && last ? `${first.display} – ${last.display}` : ''
            }}
            onPick={setPage}
          />
          <PageStep
            label="Next"
            disabled={at >= pages - 1}
            onClick={() => setPage(at + 1)}
          />
        </div>
      )}

      <ul
        className={cx(
          compact
            ? 'surface max-h-[70vh] divide-y divide-white/[0.04] overflow-y-auto scrollbar-thin'
            : 'space-y-1.5',
        )}
      >
        {paged.map((episode) => (
          <EpisodeRow
            key={episode.number}
            animeId={animeId}
            episode={episode}
            unaired={isUnaired(episode, now)}
            watched={episode.watched}
            marksFrom={
              firstUnwatched && firstUnwatched.number < episode.number
                ? firstUnwatched.display || firstUnwatched.number
                : undefined
            }
            unmarksTo={
              lastWatched && lastWatched.number > episode.number ? lastWatched.display || lastWatched.number : undefined
            }
            active={episode.number === current}
            upNext={episode.number === upNext}
            compact={compact}
            cover={cover}
            selected={picked.has(episode.number)}
            onToggle={selecting ? () => toggle(episode.number) : undefined}
            queued={busy.has(episode.number)}
            failed={failedEps.has(episode.number)}
            listed={listed.has(episode.number)}
            onUpNext={() => toggleUpNext(episode.number)}
          />
        ))}
        {paged.length === 0 && (
          <li className="rounded-xl p-6 text-center text-sm text-base-500">
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
  const shown = options.filter((o) => !o.hidden)
  if (shown.length < 2) return null
  return (
    <div role="tablist" aria-label="Show episodes" className="flex gap-1 rounded-lg bg-base-900 p-1">
      {shown.map((o) => (
        <button
          key={o.value}
          role="tab"
          aria-selected={value === o.value}
          onClick={() => onChange(o.value)}
          className={cx(
            'rounded-md px-2.5 py-1 text-xs font-medium transition-colors pointer-coarse:min-h-11',
            value === o.value ? 'bg-base-750 text-white shadow-card' : 'text-base-400 hover:text-base-100',
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
  // Portalled to the end of the page: without this, Tab went on through the page, never into the list.
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
        className="flex min-w-36 items-center justify-center gap-1.5 rounded-lg px-2 py-1 text-xs tabular-nums text-base-300 transition-colors hover:bg-base-850 hover:text-white pointer-coarse:min-h-11"
      >
        {rangeFor(page)}
        <span className="text-base-600">of {total}</span>
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
            className="fixed z-50 max-h-72 w-44 animate-rise overflow-y-auto rounded-xl border border-base-750 bg-base-850 p-1 shadow-panel scrollbar-thin"
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
                  'w-full rounded-lg px-2.5 py-1.5 text-left text-sm tabular-nums transition-colors',
                  n === page ? 'bg-accent-500/15 text-accent-200' : 'text-base-200 hover:bg-base-800',
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

function PageStep({
  label,
  disabled,
  onClick,
}: {
  label: string
  disabled: boolean
  onClick: () => void
}) {
  return (
    <button
      disabled={disabled}
      onClick={onClick}
      className="rounded-lg bg-base-850 px-2.5 py-1 text-xs text-base-200 transition-colors hover:bg-base-750 disabled:opacity-40 pointer-coarse:min-h-11 pointer-coarse:px-3.5"
    >
      {label}
    </button>
  )
}

function EpisodeRow({
  animeId,
  episode,
  watched,
  marksFrom,
  unmarksTo,
  active,
  upNext,
  compact,
  cover,
  selected,
  onToggle,
  queued,
  failed,
  unaired,
  listed,
  onUpNext,
}: {
  animeId: number
  episode: Episode
  watched: boolean
  /** On the Up next list; onUpNext adds or takes it off. */
  listed: boolean
  onUpNext: () => void
  /** Not out yet, by the list's clock. */
  unaired: boolean
  /** Ticking this one also marks every episode from here. */
  marksFrom?: number
  /** Unticking this one also unwatches every episode up to this one. */
  unmarksTo?: number
  active: boolean
  upNext?: boolean
  compact?: boolean
  cover?: string | null
  selected?: boolean
  /** In select mode a row toggles instead of opening the player. */
  onToggle?: () => void
  /** Already in the queue or arriving, so not offered again. */
  queued?: boolean
  /** The queue gave up on it; Download tries again. */
  failed?: boolean
}) {
  const row = useRef<HTMLLIElement>(null)

  // Bring the watched episode into view by scrolling the list, not scrollIntoView (which drags the page).
  useEffect(() => {
    if (!active || !compact) return

    const id = requestAnimationFrame(() => {
      const el = row.current
      const list = el?.closest('ul')
      if (!el || !list) return
      // Relative to the list's box: offsetTop is from the nearest positioned
      // ancestor (the page here), so scrolling by that lands at the bottom.
      const delta = el.getBoundingClientRect().top - list.getBoundingClientRect().top
      list.scrollTop += delta - list.clientHeight / 2 + el.clientHeight / 2
    })
    return () => cancelAnimationFrame(id)
  }, [active, compact])

  const number = episode.display || episode.number
  // The one playing is never hidden: it's on screen anyway.
  const isRevealed = useRevealed()
  const hidden = !!episode.spoiler && !active && !isRevealed(animeId, episode.number)
  // The number is on the thumbnail already; saying the title is hidden explains the blur.
  const title = hidden ? 'Title hidden' : episode.titleEn || episode.titleJa || `Episode ${number}`
  // The server's rule, not the tick: a watched episode wound back is watched and resumable.
  const resumeAt = episode.resumable && typeof episode.position === 'number' && !unaired ? episode.position : undefined
  const played =
    resumeAt !== undefined && episode.duration ? Math.min(100, (resumeAt / episode.duration) * 100) : watched ? 100 : 0

  const qc = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const mark = useMutation({
    mutationFn: (next: boolean) =>
      api.post('/api/watched', { animeId, episode: episode.number, watched: next }),
    // Settled, not success: a failure may still have changed something.
    onSettled: () => {
      setConfirming(false)
      // Franchise too: marking the last one completes the show.
      refreshListState(qc)
    },
  })

  // Always the same box, still or not: a recent episode often has none for
  // days, and dropping the image reflows every row. Cover stands in, dimmed.
  const thumb = (
    <div
      className={cx(
        'relative shrink-0 overflow-hidden rounded-lg bg-base-850',
        compact ? 'aspect-video w-24' : 'aspect-video w-24 sm:w-40',
      )}
    >
      {(episode.still ?? cover) && (
        <img
          src={episode.still ?? cover ?? undefined}
          alt=""
          loading="lazy"
          className={cx(
            'size-full object-cover transition-[transform,filter] duration-300 group-hover/ep:scale-105',
            !episode.still && 'opacity-30 blur-[1px]',
            // Scaled up so the blur's soft edge stays inside the box.
            hidden && episode.still && 'scale-125 blur-xl',
            // Watched and not the one playing: set back, not hidden.
            watched && !active && 'brightness-[0.55] saturate-50',
          )}
        />
      )}
      <span className="absolute top-1 left-1 rounded-md bg-base-950/80 px-1.5 py-0.5 text-[10px] font-bold tracking-wide text-white tabular-nums backdrop-blur-sm">
        {compact ? number : `EP ${number}`}
      </span>
      {watched && !active && (
        <span className="absolute inset-0 grid place-items-center">
          <span className="grid size-7 place-items-center rounded-full bg-accent-500/90 text-white shadow-lg">
            <WatchedTick />
          </span>
        </span>
      )}
      {!unaired && !onToggle && (
        <span className="absolute inset-0 grid place-items-center bg-base-950/40 opacity-0 transition-opacity duration-200 group-hover/ep:opacity-100">
          <span className="grid size-9 place-items-center rounded-full bg-white/95 text-base-950 shadow-lg">
            <PlayGlyph />
          </span>
        </span>
      )}
      {played > 0 && played < 100 && (
        <span className="absolute inset-x-0 bottom-0 h-1 bg-black/60">
          <span className="block h-full bg-accent-400" style={{ width: `${played}%` }} />
        </span>
      )}
    </div>
  )

  const text = (
    <div className="min-w-0 flex-1">
      <div className="flex flex-wrap items-center gap-1.5">
        {upNext && !active && !unaired && (
          <span className="rounded-md bg-accent-500 px-1.5 py-0.5 text-[10px] font-bold tracking-wide text-white uppercase">
            Next
          </span>
        )}
        {active && (
          <span className="rounded-md bg-accent-500/20 px-1.5 py-0.5 text-[10px] font-bold tracking-wide text-accent-200 uppercase">
            Playing
          </span>
        )}
        <p
          className={cx(
            'min-w-0 line-clamp-1 text-sm font-medium',
            unaired || hidden ? 'text-base-400' : watched && !active ? 'text-base-400' : 'text-white',
          )}
        >
          {title}
        </p>
      </div>
      {!compact && episode.overview && (hidden ? (
        <p className="mt-1 hidden text-xs text-base-600 sm:block">Title and synopsis hidden to avoid spoilers</p>
      ) : (
        // Not on a phone: squeezed beside the still it came out as a few words.
        <p className="mt-1 line-clamp-2 hidden text-xs leading-relaxed text-base-500 sm:block">{episode.overview}</p>
      ))}
      <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-base-500">
        {unaired ? (
          <span className="font-medium text-sky-300">Airs {relativeTime(episode.airDate!)}</span>
        ) : (
          <>
            {listed && <span className="font-medium text-accent-300">In Up next</span>}
            {resumeAt !== undefined && (
              <span className="font-medium text-accent-300">Resume {clockTime(resumeAt)}</span>
            )}
            {episode.runtime ? <span>{episode.runtime} min</span> : null}
            {!compact && episode.airDate ? <span>{airDay(episode.airDate)}</span> : null}
            {episode.onDisk ? (
              <span className="flex items-center gap-1 text-emerald-300" title="On this machine">
                <DiskGlyph />
                {compact ? <span className="sr-only">On disk</span> : 'Downloaded'}
              </span>
            ) : (
              episode.local && (
                <span className="flex items-center gap-1 text-emerald-300" title="In one of your library folders">
                  <DiskGlyph />
                  {compact ? <span className="sr-only">In your library</span> : 'In your library'}
                </span>
              )
            )}
            {/* Nothing found: a number from the count, not an episode known to exist. */}
            {episode.planned && !episode.onDisk && !episode.local && <Badge>Expected</Badge>}
          </>
        )}
        <EpisodeKind episode={episode} />
      </div>
    </div>
  )

  // The row's link names the episode, so it says what the badges and dimming show too.
  const stateWords = [
    active ? 'playing' : upNext ? 'next' : '',
    watched ? 'watched' : '',
    resumeAt !== undefined ? `resume at ${clockTime(resumeAt)}` : '',
    episode.onDisk ? 'downloaded' : episode.local ? 'in your library' : '',
    listed ? 'in Up next' : '',
  ].filter(Boolean)

  // A tick reaching past this episode, either way, asks first: auto-delete and the trackers follow it.
  const spans = watched ? unmarksTo !== undefined : marksFrom !== undefined
  // So does one that deletes the file at once ("right after watching"), which unticking doesn't bring back.
  const prefs = usePrefs()
  const deletes = !watched && !!episode.onDisk && prefs.data?.effective['cache.autodelete'] === 'now'
  const tickLabel = watched
    ? unmarksTo !== undefined
      ? `Mark ${number} to ${unmarksTo} not watched`
      : 'Mark not watched'
    : marksFrom !== undefined
      ? `Mark ${marksFrom} to ${number} watched`
      : 'Mark watched'

  // Outside the link, or the tick opens the player. A failed tick says so in words, with the same press again.
  const tick =
    !unaired &&
    !onToggle &&
    (mark.isError && !confirming ? (
      <RetryButton
        what="Couldn't mark"
        why={(mark.error as Error).message}
        pending={mark.isPending}
        // The same ask again: the refresh after a failure can flip watched, and !watched would undo it.
        onRetry={() => mark.mutate(mark.variables ?? !watched)}
      />
    ) : confirming ? (
      <span className="flex shrink-0 items-center gap-1">
        <ConfirmInline
          tone="accent"
          size="sm"
          label={
            watched
              ? `Unmark ${number}–${unmarksTo}`
              : marksFrom !== undefined
                ? `Mark ${marksFrom}–${number}`
                : `Mark ${number}`
          }
          note={deletes ? 'Deletes the file' : undefined}
          onConfirm={() => mark.mutate(!watched)}
          onCancel={() => setConfirming(false)}
          pending={mark.isPending}
        />
      </span>
    ) : (
      <button
        type="button"
        onClick={() => (spans || deletes ? setConfirming(true) : mark.mutate(!watched))}
        disabled={mark.isPending}
        aria-label={tickLabel}
        title={tickLabel}
        className={cx(
          'tap relative grid size-8 shrink-0 place-items-center rounded-full transition-colors',
          watched
            ? 'bg-accent-500/15 text-accent-300 hover:bg-accent-500/25'
            : 'text-base-500 ring-1 ring-base-700 hover:bg-base-800 hover:text-base-100',
        )}
      >
        <WatchedTick />
      </button>
    ))

  // One click to queue this episode, beside the tick; the select mode is for many at once.
  const fetch = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<{ queued: boolean }>('/api/download', { animeId, episode: number }),
    onSuccess: () => refreshDownloads(qc),
  })
  // A click that queued it, until the queue answers: it failed, or its entry, once there, has gone.
  const resetFetch = fetch.reset
  const wasQueued = useRef(queued)
  useEffect(() => {
    if (failed || (wasQueued.current && !queued)) resetFetch()
    wasQueued.current = queued
  }, [failed, queued, resetFetch])
  const isQueued = (fetch.isSuccess && !failed) || queued
  const download =
    !unaired &&
    !onToggle &&
    !episode.onDisk &&
    !episode.local &&
    (fetch.isError ? (
      <RetryButton
        what="Couldn't queue"
        why={(fetch.error as Error).message}
        pending={fetch.isPending}
        onRetry={() => fetch.mutate()}
      />
    ) : (
      <button
        type="button"
        onClick={() => fetch.mutate()}
        disabled={fetch.isPending || isQueued}
        aria-label={isQueued ? `Episode ${number} queued` : `Download episode ${number}`}
        title={isQueued ? 'Queued' : 'Download'}
        className={cx(
          'tap relative grid size-8 shrink-0 place-items-center rounded-full transition-colors',
          isQueued ? 'text-emerald-300' : 'text-base-500 ring-1 ring-base-700 hover:bg-base-800 hover:text-base-100',
        )}
      >
        {isQueued ? <QueuedGlyph /> : <DownloadArrow />}
      </button>
    ))

  // Not in the player's narrow list, where a fourth button squeezes titles away; the show page has room.
  const showSpoiler = hidden && !onToggle && !compact && (
    <button
      type="button"
      onClick={() => reveal(animeId, episode.number)}
      aria-label={`Show episode ${number}'s title and synopsis`}
      title="Show title and synopsis"
      className="tap relative grid size-8 shrink-0 place-items-center rounded-full text-base-500 ring-1 ring-base-700 transition-colors hover:bg-base-800 hover:text-base-100"
    >
      <EyeGlyph />
    </button>
  )

  const queueUp = !unaired && !onToggle && (
    <button
      type="button"
      onClick={onUpNext}
      aria-pressed={listed}
      aria-label={listed ? `Remove episode ${number} from Up next` : `Add episode ${number} to Up next`}
      title={listed ? 'In Up next' : 'Add to Up next'}
      // 32 px drawn, 44 px to a finger: three of these beside the title leave it room on a phone.
      className={cx(
        'tap relative grid size-8 shrink-0 place-items-center rounded-full transition-colors',
        listed
          ? 'bg-accent-500/15 text-accent-300 hover:bg-accent-500/25'
          : 'text-base-500 ring-1 ring-base-700 hover:bg-base-800 hover:text-base-100',
      )}
    >
      {listed ? <ListCheckGlyph /> : <ListPlusGlyph />}
    </button>
  )

  const shell = cx(
    'group/ep flex items-center gap-3 transition-colors',
    compact ? 'p-2' : 'rounded-xl p-2 sm:p-2.5',
    active
      ? 'bg-accent-500/10 ring-1 ring-accent-500/40'
      : upNext && !compact
        ? 'bg-base-900/80 ring-1 ring-accent-500/30'
        : compact
          ? 'hover:bg-base-850'
          : 'bg-base-900/40 hover:bg-base-900',
  )

  return (
    <li ref={row}>
      {unaired ? (
        <div className={cx(shell, 'cursor-default opacity-75')}>
          {thumb}
          {text}
          {showSpoiler}
        </div>
      ) : onToggle ? (
        <button
          type="button"
          onClick={onToggle}
          aria-pressed={selected}
          className={cx(shell, 'w-full text-left', selected && 'bg-accent-500/10 ring-1 ring-accent-500/40')}
        >
          <span
            aria-hidden
            className={cx(
              'grid size-5 shrink-0 place-items-center rounded-md border text-[11px] text-white',
              selected ? 'border-accent-400 bg-accent-500' : 'border-base-600',
            )}
          >
            {selected && '✓'}
          </span>
          {thumb}
          {text}
        </button>
      ) : (
        <div className={cx(shell, 'pr-2 sm:pr-3')}>
          <Link
            to={`/watch/${animeId}/${episode.number}`}
            aria-current={active ? 'true' : undefined}
            aria-label={[hidden ? `Episode ${number}` : `Episode ${number}: ${title}`, ...stateWords].join(', ')}
            className="flex min-w-0 flex-1 items-center gap-3"
          >
            {thumb}
            {text}
          </Link>
          {showSpoiler}
          {queueUp}
          {download}
          {tick}
        </div>
      )}
    </li>
  )
}

/** A failed row action in words, with Retry; the server's reason on hover and for a screen reader. */
function RetryButton({ what, why, pending, onRetry }: { what: string; why: string; pending: boolean; onRetry: () => void }) {
  return (
    <span role="alert" className="flex shrink-0 items-center gap-1 text-xs text-recap" title={why}>
      <span className="max-sm:sr-only">{what}</span>
      <button
        type="button"
        onClick={onRetry}
        disabled={pending}
        aria-label={`${what}: ${why}. Retry`}
        className="tap relative rounded-md px-2 py-1 font-medium text-white ring-1 ring-recap/60 transition-colors hover:bg-recap/15"
      >
        Retry
      </button>
    </span>
  )
}

function DownloadArrow() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path
        d="M12 4v11m0 0 4-4m-4 4-4-4M5 19h14"
        fill="none"
        stroke="currentColor"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

// A clock: queued is waiting, unlike the tick beside it, which means watched.
function QueuedGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <circle cx="12" cy="12" r="8" fill="none" stroke="currentColor" strokeWidth="2" />
      <path d="M12 8v4l2.5 2" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  )
}

function WatchedTick() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path
        d="m5 13 4 4L19 7"
        fill="none"
        stroke="currentColor"
        strokeWidth="2.4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function PlayGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 translate-x-px" aria-hidden>
      <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />
    </svg>
  )
}

// A filled check, a state: the arrow beside it on the next row is the Download action.
function EyeGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path
        d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinejoin="round"
      />
      <circle cx="12" cy="12" r="2.8" fill="none" stroke="currentColor" strokeWidth="1.8" />
    </svg>
  )
}

function DiskGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-3" aria-hidden>
      <circle cx="12" cy="12" r="10" fill="currentColor" />
      <path
        d="m7.5 12.5 3 3 6-7"
        fill="none"
        stroke="var(--color-base-950)"
        strokeWidth="2.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function SearchGlyph() {
  return (
    <svg
      viewBox="0 0 24 24"
      className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-base-500"
      fill="none"
      stroke="currentColor"
      strokeWidth="2.2"
      aria-hidden
    >
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" strokeLinecap="round" />
    </svg>
  )
}
