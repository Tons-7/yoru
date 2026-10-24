import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import type { UpNextItem } from '../lib/api'
import { clockTime, cx } from '../lib/format'
import { useAnchorBox, useDismiss, useModalFocus } from '../lib/hooks'
import { useUpNext, useUpNextEdit } from '../lib/queries'
import { toast } from '../lib/toast'
import { buttonClass } from '../lib/ui'
import { upNextSummary, useInUpNext } from '../lib/upnext'
import { Rail, RailItem } from './Rail'
import { ConfirmInline, ProgressBar } from './ui'

/** The episodes of one show on the list: they play together, in episode order. */
interface ShowGroup {
  animeId: number
  title: string
  cover?: string
  items: UpNextItem[]
}

function groupByShow(items: UpNextItem[]): ShowGroup[] {
  const groups: ShowGroup[] = []
  for (const it of items) {
    const last = groups[groups.length - 1]
    if (last?.animeId === it.animeId) last.items.push(it)
    else groups.push({ animeId: it.animeId, title: it.title, cover: it.cover, items: [it] })
  }
  return groups
}

/** + over a card's art: puts the episode on the list, or takes it off. */
export function UpNextToggle({ animeId, episode, title }: { animeId: number; episode: number; title: string }) {
  const listed = useInUpNext(animeId).has(episode)
  const { add, remove } = useUpNextEdit()
  return (
    <button
      type="button"
      onClick={() => (listed ? remove.mutate({ animeId, episode }) : add.mutate({ animeId, episode }))}
      aria-pressed={listed}
      aria-label={listed ? `Remove ${title} episode ${episode} from Up next` : `Add ${title} episode ${episode} to Up next`}
      title={listed ? 'In Up next' : 'Add to Up next'}
      className={cx(
        'tap absolute top-1.5 left-1.5 z-10 grid size-7 place-items-center rounded-md ring-1 backdrop-blur-sm transition-colors',
        listed
          ? 'bg-accent-500/90 text-white ring-accent-300/40 hover:bg-accent-500'
          : 'bg-base-950/80 text-base-200 ring-white/10 hover:bg-base-800 hover:text-white',
      )}
    >
      {listed ? <ListCheckGlyph className="size-3.5" /> : <ListPlusGlyph className="size-3.5" />}
    </button>
  )
}

/** Home's row of the list, numbered in play order; nothing at all while the list is empty. */
export function UpNextRail({ onEdit }: { onEdit: () => void }) {
  const list = useUpNext()
  const items = list.data?.items ?? []
  if (items.length === 0) return null
  return (
    <Rail
      title="Up next"
      meta={upNextSummary(items)}
      action={
        <button
          onClick={onEdit}
          className="flex items-center gap-1.5 rounded-full bg-base-900 px-3 py-1 text-xs font-semibold text-base-300 ring-1 ring-white/[0.07] transition-colors hover:bg-base-800 hover:text-white"
        >
          <ListPlusGlyph className="size-3.5" />
          Edit list
        </button>
      }
    >
      {items.map((item, i) => (
        <RailItem key={`${item.animeId}-${item.episode}`}>
          <UpNextCard item={item} place={i + 1} />
        </RailItem>
      ))}
    </Rail>
  )
}

function UpNextCard({ item, place }: { item: UpNextItem; place: number }) {
  const { add, remove } = useUpNextEdit()
  // At once, with an Undo: the ✕ beside it on Continue watching asks, and this one used to just go.
  const drop = () => {
    const v = { animeId: item.animeId, episode: item.episode }
    remove.mutate(v, {
      onSuccess: () =>
        toast(`Removed episode ${item.episode} of ${item.title} from Up next`, 'info', {
          label: 'Undo',
          run: () => add.mutate(v),
        }),
    })
  }
  const watch = `/watch/${item.animeId}/${item.episode}`
  const started = (item.position ?? 0) > 0 && (item.duration ?? 0) > 0
  const percent = started ? Math.min(100, (item.position! / item.duration!) * 100) : 0

  return (
    <div className="group/upnext relative">
      <button
        onClick={drop}
        disabled={remove.isPending}
        aria-label={`Remove ${item.title} episode ${item.episode} from Up next`}
        title="Remove from Up next"
        className="tap absolute top-1.5 right-1.5 z-10 grid size-7 place-items-center rounded-md bg-base-950/80 text-base-300 ring-1 ring-white/10 backdrop-blur-sm transition-colors hover:bg-base-800 hover:text-white"
      >
        <CrossGlyph />
      </button>

      <Link to={watch} aria-label={`Play ${item.title} episode ${item.episode}`} className="block">
        <div className="relative aspect-video overflow-hidden rounded-card bg-base-850 ring-1 ring-base-800">
          {item.cover && (
            <img
              src={item.cover}
              alt=""
              loading="lazy"
              className="size-full object-cover object-center transition-transform duration-300 group-hover/upnext:scale-105"
            />
          )}
          <div className="absolute inset-0 bg-gradient-to-t from-base-950 via-base-950/30 to-transparent" />
          <span className="absolute top-2 left-2 flex gap-1">
            <span className="grid h-5 min-w-5 place-items-center rounded-md bg-accent-500 px-1 text-[11px] font-bold text-white tabular-nums">
              {place}
            </span>
            <span className="rounded-md bg-base-950/80 px-1.5 py-0.5 text-[10px] font-bold tracking-wide text-white backdrop-blur-sm">
              EP {item.episode}
            </span>
          </span>
          <div className="absolute inset-x-0 bottom-0 p-2.5">
            <p className="mb-1.5 text-[11px] font-medium text-base-200">
              {started
                ? `${clockTime(Math.max(0, item.duration! - item.position!))} left`
                : item.minutes
                  ? `${item.minutes} min`
                  : ''}
            </p>
            {percent > 0 && <ProgressBar value={percent} className="h-1.5 bg-white/15" />}
          </div>
        </div>
      </Link>

      <Link to={`/anime/${item.animeId}`} className="mt-2 block">
        <p className="line-clamp-1 text-sm leading-snug text-base-200 transition-colors hover:text-white">{item.title}</p>
      </Link>
      <p className="mt-0.5 line-clamp-1 text-xs text-base-500">
        Episode {item.episode}
        {item.episodeTitle && !item.spoiler ? ` · ${item.episodeTitle}` : ''}
      </p>
    </div>
  )
}

/** Editing the list: a side sheet on a wide screen, a bottom sheet on a phone. */
export function UpNextSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  // Ends a drag without moving anything; set while a grip is held.
  const cancelDrag = useRef<(() => void) | null>(null)
  const close = useCallback(() => {
    // Escape mid-drag drops the drag, not the sheet, whose drop would still have reordered.
    if (cancelDrag.current) return cancelDrag.current()
    onClose()
  }, [onClose])
  const panel = useDismiss<HTMLDivElement>(close, open)
  useModalFocus(panel, open)
  const list = useUpNext()
  const edit = useUpNextEdit()
  const [clearing, setClearing] = useState(false)

  const items = useMemo(() => list.data?.items ?? [], [list.data])
  const groups = useMemo(() => groupByShow(items), [items])

  // While a grip is held: the shows in the order the drag has them.
  const [drag, setDrag] = useState<{ id: number; order: number[] } | null>(null)
  const dragging = useRef<{ id: number; start: number[]; order: number[] } | null>(null)
  // The order moves will leave, shown until the list data has it, so no row jumps back while they land.
  const [pending, setPending] = useState<number[] | null>(null)
  const groupsKey = groups.map((g) => g.animeId).join()
  if (pending && (pending.join() === groupsKey || edit.move.isError || (!edit.move.isPending && !list.isFetching))) {
    setPending(null)
  }
  const rows = useRef<HTMLUListElement>(null)
  const byId = useMemo(() => new Map(groups.map((g) => [g.animeId, g])), [groups])
  const order = drag?.order ?? pending
  const shown = order ? order.map((id) => byId.get(id)).filter((g): g is ShowGroup => !!g) : groups
  const shownKey = shown.map((g) => g.animeId).join()

  // A moved row's node is put back in the list, which drops focus; the control used gets it again.
  const refocus = useRef<{ id: number; control: 'grip' | 'menu' } | null>(null)
  useEffect(() => {
    const want = refocus.current
    if (!want) return
    refocus.current = null
    rows.current?.querySelector<HTMLElement>(`[data-show="${want.id}"] [data-focus="${want.control}"]`)?.focus()
  }, [shownKey])

  // From the order on screen, which can be ahead of the list data while earlier moves land.
  const moveTo = (ids: number[], animeId: number, to: number, control?: 'grip' | 'menu') => {
    const from = ids.indexOf(animeId)
    if (from < 0 || to === from || to < 0 || to >= ids.length) return
    if (control) refocus.current = { id: animeId, control }
    const next = ids.filter((id) => id !== animeId)
    next.splice(to, 0, animeId)
    setPending(next)
    edit.move.mutate({ animeId, to })
  }

  // On the window, not the grip: React moves the dragged row's node, which would lose a pointer capture.
  const startDrag = (e: React.PointerEvent, id: number) => {
    e.preventDefault()
    const start = shown.map((g) => g.animeId)
    dragging.current = { id, start, order: start }
    setDrag(dragging.current)
    const onMove = (ev: PointerEvent) => {
      const d = dragging.current
      if (!d || !rows.current) return
      // Where it goes: past every other show whose middle is above the pointer.
      let to = 0
      for (const row of rows.current.querySelectorAll<HTMLElement>('[data-show]')) {
        if (Number(row.dataset.show) === d.id) continue
        const box = row.getBoundingClientRect()
        if (ev.clientY > box.top + box.height / 2) to++
      }
      if (to === d.order.indexOf(d.id)) return
      const order = d.order.filter((x) => x !== d.id)
      order.splice(to, 0, d.id)
      dragging.current = { ...d, order }
      setDrag(dragging.current)
    }
    const stop = () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onEnd)
      window.removeEventListener('pointercancel', onEnd)
      dragging.current = null
      cancelDrag.current = null
      setDrag(null)
    }
    const onEnd = (ev: PointerEvent) => {
      const d = dragging.current
      stop()
      if (d && ev.type === 'pointerup') moveTo(d.start, d.id, d.order.indexOf(d.id))
    }
    cancelDrag.current = stop
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onEnd)
    window.addEventListener('pointercancel', onEnd)
  }
  // Closed or gone with a grip still held: the drag ends there.
  useEffect(() => {
    if (!open) cancelDrag.current?.()
  }, [open])
  useEffect(() => () => cancelDrag.current?.(), [])

  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-50">
      <div className="absolute inset-0 bg-black/60 backdrop-blur-[2px]" />
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label="Edit Up next"
        className="animate-fade-in absolute inset-x-0 bottom-0 flex max-h-[85dvh] flex-col rounded-t-2xl border border-base-800 bg-base-900 shadow-2xl sm:inset-y-0 sm:right-0 sm:left-auto sm:max-h-none sm:w-[26rem] sm:rounded-none sm:border-y-0 sm:border-r-0"
      >
        <div aria-hidden className="mx-auto mt-2.5 h-1 w-10 rounded-full bg-base-700 sm:hidden" />
        <div className="flex items-start justify-between gap-3 px-4 pt-3 sm:px-5 sm:pt-5">
          <div className="min-w-0">
            <h2 className="font-display text-xl font-semibold text-white">Up next</h2>
            <p className="mt-0.5 text-sm text-base-500">{upNextSummary(items)}</p>
          </div>
          <button
            onClick={close}
            aria-label="Close"
            className="grid size-11 shrink-0 place-items-center rounded-full bg-base-850 text-base-300 transition-colors hover:bg-base-800 hover:text-white"
          >
            <CrossGlyph />
          </button>
        </div>
        <p className="px-4 pt-1 pb-3 text-xs leading-relaxed text-base-500 sm:px-5">
          Plays top to bottom after the episode you're on. Watched episodes leave the list by themselves.
        </p>

        <ul ref={rows} className="min-h-0 flex-1 space-y-1.5 overflow-y-auto overscroll-contain px-3 pb-2 scrollbar-thin">
          {shown.map((g, i) => (
            <ShowRow
              key={g.animeId}
              group={g}
              place={i}
              last={i === shown.length - 1}
              dragging={drag?.id === g.animeId}
              onGripDown={(e) => startDrag(e, g.animeId)}
              onMove={(to, control) => moveTo(shown.map((s) => s.animeId), g.animeId, to, control)}
            />
          ))}
          {list.isSuccess && items.length === 0 && (
            <li className="rounded-xl bg-base-850/60 p-4 text-sm text-base-400">
              Nothing up next. Add an episode with + on its row, or the next episodes of what you're watching.
            </li>
          )}
          {list.isError && (
            <li className="flex items-center justify-between gap-3 rounded-xl bg-base-850/60 p-4 text-sm text-base-400">
              Couldn't load the list.
              <button onClick={() => list.refetch()} className={buttonClass('secondary', 'sm')}>
                Try again
              </button>
            </li>
          )}
        </ul>

        <div className="flex flex-wrap items-center gap-2 border-t border-white/[0.05] px-4 pt-3 pb-[max(1rem,env(safe-area-inset-bottom))] sm:px-5">
          {clearing ? (
            <ConfirmInline
              label="Clear list"
              note={`All ${items.length} go.`}
              onConfirm={() => edit.clear.mutate(undefined, { onSettled: () => setClearing(false) })}
              onCancel={() => setClearing(false)}
              pending={edit.clear.isPending}
            />
          ) : (
            <>
              <button
                onClick={() => edit.fill.mutate()}
                disabled={edit.fill.isPending}
                title="One episode of each show you're watching, the next you haven't seen"
                className={cx(buttonClass('primary'), 'min-h-11 flex-1')}
              >
                {edit.fill.isPending ? 'Adding…' : 'Add next episodes'}
              </button>
              <button
                onClick={() => setClearing(true)}
                disabled={items.length === 0}
                className={cx(buttonClass('secondary'), 'min-h-11')}
              >
                Clear list
              </button>
            </>
          )}
        </div>
      </div>
    </div>,
    document.body,
  )
}

function ShowRow({
  group,
  place,
  last,
  dragging,
  onGripDown,
  onMove,
}: {
  group: ShowGroup
  place: number
  last: boolean
  dragging: boolean
  onGripDown: (e: React.PointerEvent) => void
  /** control: the one focus goes back to once the row has moved. */
  onMove: (to: number, control: 'grip' | 'menu') => void
}) {
  const { remove } = useUpNextEdit()
  const minutes = group.items.reduce((sum, it) => sum + (it.minutes ?? 0), 0)
  const episodes = group.items.map((it) => it.episode)
  const label =
    episodes.length === 1 ? `Ep ${episodes[0]}` : `Episodes ${episodes.join(', ')}`

  return (
    <li
      data-show={group.animeId}
      className={cx(
        'flex items-center gap-2 rounded-xl p-2 transition-[background-color,box-shadow]',
        dragging ? 'bg-base-800 shadow-panel ring-1 ring-accent-500/50' : 'bg-base-850/60',
      )}
    >
      {/* The grip alone drags, so a phone still scrolls the list from anywhere else. */}
      <button
        type="button"
        aria-label={`Reorder ${group.title}; arrow keys move it`}
        title="Drag to reorder"
        data-focus="grip"
        onPointerDown={onGripDown}
        onKeyDown={(e) => {
          if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return
          e.preventDefault()
          onMove(place + (e.key === 'ArrowUp' ? -1 : 1), 'grip')
        }}
        className="tap relative grid size-7 shrink-0 cursor-grab touch-none place-items-center rounded-md text-base-500 transition-colors hover:bg-base-800 hover:text-base-200 active:cursor-grabbing"
      >
        <GripGlyph />
      </button>

      <div className="aspect-video w-16 shrink-0 overflow-hidden rounded-md bg-base-800">
        {group.cover && <img src={group.cover} alt="" loading="lazy" className="size-full object-cover" />}
      </div>

      <div className="min-w-0 flex-1">
        <p className="truncate text-sm text-base-100">{group.title}</p>
        <p className="truncate text-xs text-base-500">
          {label}
          {minutes > 0 ? ` · ${minutes} min` : ''}
        </p>
        {group.items.length > 1 && (
          <div className="mt-1.5 flex flex-wrap gap-1">
            {group.items.map((it) => (
              <button
                key={it.episode}
                onClick={() => remove.mutate({ animeId: it.animeId, episode: it.episode })}
                aria-label={`Remove episode ${it.episode} of ${group.title}`}
                className="tap relative flex h-7 items-center gap-1 rounded-md bg-base-800 px-2 text-xs text-base-300 transition-colors hover:bg-base-750 hover:text-white"
              >
                Ep {it.episode}
                <CrossGlyph className="size-3" />
              </button>
            ))}
          </div>
        )}
      </div>

      <RowMenu
        title={group.title}
        first={place === 0}
        last={last}
        onTop={() => onMove(0, 'menu')}
        onUp={() => onMove(place - 1, 'menu')}
        onDown={() => onMove(place + 1, 'menu')}
        onRemove={() => remove.mutate({ animeId: group.animeId })}
      />
    </li>
  )
}

/** One "…" per show: the moves a drag makes, for remotes, keyboards and anyone who can't drag. */
function RowMenu({
  title,
  first,
  last,
  onTop,
  onUp,
  onDown,
  onRemove,
}: {
  title: string
  first: boolean
  last: boolean
  onTop: () => void
  onUp: () => void
  onDown: () => void
  onRemove: () => void
}) {
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const trigger = useRef<HTMLButtonElement>(null)
  const box = useAnchorBox(trigger, open)

  const items = [
    { label: 'Move to top', run: onTop, disabled: first },
    { label: 'Move up', run: onUp, disabled: first },
    { label: 'Move down', run: onDown, disabled: last },
    { label: 'Remove', run: onRemove, disabled: false },
  ]

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        ref={trigger}
        type="button"
        data-focus="menu"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`More for ${title}`}
        onClick={() => setOpen((v) => !v)}
        className="tap relative grid size-7 place-items-center rounded-md text-base-400 transition-colors hover:bg-base-800 hover:text-white"
      >
        <MoreGlyph />
      </button>
      {open &&
        box &&
        createPortal(
          <div
            role="menu"
            data-portal-menu
            aria-label={`${title} in Up next`}
            onKeyDown={(e) => {
              if (e.key !== 'Escape') return
              // Only the menu: the sheet around it stays.
              e.stopPropagation()
              setOpen(false)
              trigger.current?.focus()
            }}
            style={{
              top: Math.min(box.bottom + 6, window.innerHeight - 196),
              left: Math.max(8, Math.min(box.right - 176, window.innerWidth - 184)),
            }}
            className="fixed z-[60] w-44 animate-rise rounded-xl border border-base-750 bg-base-850 p-1 shadow-panel"
          >
            {items.map((item, i) => (
              <button
                key={item.label}
                role="menuitem"
                autoFocus={i === items.findIndex((x) => !x.disabled)}
                disabled={item.disabled}
                onClick={() => {
                  setOpen(false)
                  item.run()
                }}
                className={cx(
                  'flex min-h-10 w-full items-center rounded-lg px-2.5 text-left text-sm transition-colors disabled:opacity-40',
                  item.label === 'Remove' ? 'text-red-300 hover:bg-red-500/10' : 'text-base-200 hover:bg-base-800',
                )}
              >
                {item.label}
              </button>
            ))}
          </div>,
          document.body,
        )}
    </div>
  )
}

export function ListPlusGlyph({ className = 'size-4' }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <path
        d="M4 7h11M4 12h11M4 17h7M18 14v6m-3-3h6"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
      />
    </svg>
  )
}

/** The list with a tick: this episode is on it. */
export function ListCheckGlyph({ className = 'size-4' }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <path
        d="M4 7h11M4 12h11M4 17h6m4 0 2.5 2.5L21 15"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function CrossGlyph({ className = 'size-3.5' }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />
    </svg>
  )
}

function GripGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" fill="currentColor" aria-hidden>
      {[6, 12, 18].map((y) => (
        <g key={y}>
          <circle cx="9" cy={y} r="1.6" />
          <circle cx="15" cy={y} r="1.6" />
        </g>
      ))}
    </svg>
  )
}

function MoreGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" fill="currentColor" aria-hidden>
      <circle cx="5" cy="12" r="1.8" />
      <circle cx="12" cy="12" r="1.8" />
      <circle cx="19" cy="12" r="1.8" />
    </svg>
  )
}
