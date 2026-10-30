import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { api, type UpNextItem } from '../lib/api'
import { cx } from '../lib/format'
import { useDismiss, useModalFocus } from '../lib/hooks'
import { refreshListState, useUpNext, useUpNextEdit } from '../lib/queries'
import { toast } from '../lib/toast'
import { buttonClass } from '../lib/ui'
import { upNextSummary, useInUpNext } from '../lib/upnext'
import { MoreMenu } from './MoreMenu'
import { Rail, RailItem } from './Rail'
import { ResumeCard } from './ResumeCard'
import { ConfirmInline } from './ui'

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

function queuedShows(items: UpNextItem[]) {
  const shows = new Map<number, { first: UpNextItem; more: number }>()
  for (const it of items) {
    const show = shows.get(it.animeId)
    if (show) show.more++
    else shows.set(it.animeId, { first: it, more: 0 })
  }
  return [...shows.values()]
}

export function UpNextRail({
  onEdit,
  after = [],
  folded = false,
  onUnfold,
}: {
  onEdit: () => void
  after?: Array<{ key: string; node: ReactNode }>
  folded?: boolean
  onUnfold?: () => void
}) {
  const list = useUpNext()
  const items = list.data?.items ?? []
  if (list.isPending || (items.length === 0 && after.length === 0)) return null
  return (
    <Rail
      title="Up next"
      folded={folded}
      onUnfold={onUnfold}
      meta={items.length > 0 ? upNextSummary(items) : undefined}
      action={
        <button
          type="button"
          onClick={onEdit}
          className={buttonClass('secondary', 'sm')}
          title={items.length > 0 ? undefined : 'Line up episodes to play one after another'}
        >
          <ListPlusGlyph className="size-3.5" />
          {items.length > 0 ? 'Edit list' : 'Line up episodes'}
        </button>
      }
    >
      {queuedShows(items).map(({ first, more }) => (
        <RailItem key={`${first.animeId}-${first.episode}`} size="wide">
          <UpNextCard item={first} more={more} />
        </RailItem>
      ))}
      {after.map(({ key, node }) => (
        <RailItem key={key} size="wide">
          {node}
        </RailItem>
      ))}
    </Rail>
  )
}

function UpNextCard({ item, more }: { item: UpNextItem; more: number }) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { add, remove } = useUpNextEdit()
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
  const watched = useMutation({
    mutationFn: () => api.post('/api/watched', { animeId: item.animeId, episode: item.episode, watched: true }),
    onSuccess: () => {
      refreshListState(qc)
      toast(`Marked episode ${item.episode} of ${item.title} watched`, 'info')
    },
  })

  return (
    <ResumeCard
      animeId={item.animeId}
      title={item.title}
      cover={item.cover}
      episode={item.episode}
      position={item.position}
      duration={item.duration}
      minutes={item.minutes}
      extra={more > 0 ? `${more} more queued` : item.episodeTitle && !item.spoiler ? item.episodeTitle : null}
      menu={
        <MoreMenu
          label={`More for ${item.title} episode ${item.episode}`}
          menuLabel={`${item.title} episode ${item.episode}`}
          className="size-11 rounded-full"
          items={[
            { label: 'Go to show', run: () => navigate(`/anime/${item.animeId}`) },
            { label: 'Mark as watched', run: () => watched.mutate(), disabled: watched.isPending },
            { label: 'Remove from Up next', run: drop, disabled: remove.isPending, danger: true },
          ]}
        />
      }
    />
  )
}

export function UpNextSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const cancelDrag = useRef<(() => void) | null>(null)
  const close = useCallback(() => {
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

  const [drag, setDrag] = useState<{ id: number; order: number[] } | null>(null)
  const dragging = useRef<{ id: number; start: number[]; order: number[] } | null>(null)
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

  const refocus = useRef<{ id: number; control: 'grip' | 'menu' } | null>(null)
  useEffect(() => {
    const want = refocus.current
    if (!want) return
    refocus.current = null
    rows.current?.querySelector<HTMLElement>(`[data-show="${want.id}"] [data-focus="${want.control}"]`)?.focus()
  }, [shownKey])

  const moveTo = (ids: number[], animeId: number, to: number, control?: 'grip' | 'menu') => {
    const from = ids.indexOf(animeId)
    if (from < 0 || to === from || to < 0 || to >= ids.length) return
    if (control) refocus.current = { id: animeId, control }
    const next = ids.filter((id) => id !== animeId)
    next.splice(to, 0, animeId)
    setPending(next)
    edit.move.mutate({ animeId, to })
  }

  const startDrag = (e: React.PointerEvent, id: number) => {
    e.preventDefault()
    const start = shown.map((g) => g.animeId)
    dragging.current = { id, start, order: start }
    setDrag(dragging.current)
    const onMove = (ev: PointerEvent) => {
      const d = dragging.current
      if (!d || !rows.current) return
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
  useEffect(() => {
    if (!open) cancelDrag.current?.()
  }, [open])
  useEffect(() => () => cancelDrag.current?.(), [])

  if (!open) return null

  return createPortal(
    <div className="fixed inset-0 z-50">
      <div className="animate-fade-in absolute inset-0 bg-black/60" />
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label="Edit Up next"
        className="animate-sheet absolute inset-x-0 bottom-0 flex max-h-[85dvh] flex-col rounded-t-sheet bg-base-750 shadow-panel sm:animate-fade-in sm:inset-y-0 sm:right-0 sm:left-auto sm:max-h-none sm:w-[26rem] sm:rounded-none"
      >
        <div aria-hidden className="mx-auto mt-2.5 h-1 w-10 shrink-0 rounded-full bg-base-600 sm:hidden" />
        <div className="flex items-start justify-between gap-3 px-4 pt-3 sm:px-5 sm:pt-5">
          <div className="min-w-0">
            <h2 className="font-display text-xl font-semibold text-white">Up next</h2>
            <p className="mt-0.5 text-sm text-base-400 tabular-nums">{upNextSummary(items)}</p>
          </div>
          <button
            type="button"
            onClick={close}
            aria-label="Close"
            className="grid size-11 shrink-0 place-items-center rounded-full bg-base-800 text-base-300 transition-colors hover:bg-base-700 hover:text-white"
          >
            <CrossGlyph />
          </button>
        </div>
        <p className="px-4 pt-1 pb-3 text-[13px] leading-relaxed text-base-400 sm:px-5">
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
            <li className="rounded-xl bg-base-800 p-4 text-sm text-base-400">
              Nothing up next. Add an episode with + on its row, or the next episodes of what you're watching.
            </li>
          )}
          {list.isError && (
            <li className="flex items-center justify-between gap-3 rounded-xl bg-base-800 p-4 text-sm text-base-400">
              Couldn't load the list.
              <button type="button" onClick={() => list.refetch()} className={buttonClass('secondary', 'sm')}>
                Try again
              </button>
            </li>
          )}
        </ul>

        <div className="flex flex-wrap items-center gap-2 px-4 pt-3 pb-[max(1rem,env(safe-area-inset-bottom))] shadow-[0_-1px_0_rgb(255_255_255/0.06)] sm:px-5">
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
        dragging ? 'bg-base-700 shadow-panel ring-1 ring-accent-500/50' : 'bg-base-800',
      )}
    >
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
        className="tap relative grid size-7 shrink-0 cursor-grab touch-none place-items-center rounded-md text-base-500 transition-colors hover:bg-base-750 hover:text-base-200 active:cursor-grabbing"
      >
        <GripGlyph />
      </button>

      <div className="aspect-video w-16 shrink-0 overflow-hidden rounded-md bg-base-750">
        {group.cover && <img src={group.cover} alt="" loading="lazy" className="size-full object-cover" />}
      </div>

      <div className="min-w-0 flex-1">
        <p className="truncate text-sm text-base-100">{group.title}</p>
        <p className="truncate text-[13px] text-base-500 tabular-nums">
          {label}
          {minutes > 0 ? ` · ${minutes} min` : ''}
        </p>
        {group.items.length > 1 && (
          <div className="mt-1.5 flex flex-wrap gap-1">
            {group.items.map((it) => (
              <button
                type="button"
                key={it.episode}
                onClick={() => remove.mutate({ animeId: it.animeId, episode: it.episode })}
                aria-label={`Remove episode ${it.episode} of ${group.title}`}
                className="tap relative flex h-7 items-center gap-1 rounded-md bg-base-750 px-2 text-xs text-base-300 transition-colors hover:bg-base-700 hover:text-white"
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
  return (
    <MoreMenu
      label={`More for ${title}`}
      menuLabel={`${title} in Up next`}
      className="size-7 rounded-md"
      items={[
        { label: 'Move to top', run: onTop, disabled: first },
        { label: 'Move up', run: onUp, disabled: first },
        { label: 'Move down', run: onDown, disabled: last },
        { label: 'Remove', run: onRemove, danger: true },
      ]}
    />
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

function ListCheckGlyph({ className = 'size-4' }: { className?: string }) {
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
