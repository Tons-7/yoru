import { useCallback, useEffect, useRef, useState } from 'react'
import { cx } from '../lib/format'
import { buttonClass } from '../lib/ui'
import { homeRowLabel, type HomeLayout, type HomeRowId } from './homeLayout'
import { MoreMenu } from './MoreMenu'
import { Sheet } from './Sheet'
import { Switch } from './ui'

export function EditHomeSheet({ open, onClose, layout }: { open: boolean; onClose: () => void; layout: HomeLayout }) {
  const rows = useRef<HTMLUListElement>(null)
  const [drag, setDrag] = useState<{ id: HomeRowId; order: HomeRowId[] } | null>(null)
  const dragging = useRef<{ id: HomeRowId; order: HomeRowId[] } | null>(null)
  const cancelDrag = useRef<(() => void) | null>(null)
  const refocus = useRef<{ id: HomeRowId; control: 'grip' | 'menu' } | null>(null)
  const shown = drag?.order ?? layout.order
  const shownKey = shown.join()

  useEffect(() => {
    const want = refocus.current
    if (!want) return
    refocus.current = null
    rows.current?.querySelector<HTMLElement>(`[data-row="${want.id}"] [data-focus="${want.control}"]`)?.focus()
  }, [shownKey])

  const close = useCallback(() => {
    if (cancelDrag.current) return cancelDrag.current()
    onClose()
  }, [onClose])

  const moveTo = (id: HomeRowId, to: number, control: 'grip' | 'menu') => {
    refocus.current = { id, control }
    layout.move(id, to)
  }

  const startDrag = (e: React.PointerEvent, id: HomeRowId) => {
    e.preventDefault()
    dragging.current = { id, order: layout.order }
    setDrag(dragging.current)
    const onMove = (ev: PointerEvent) => {
      const d = dragging.current
      if (!d || !rows.current) return
      let to = 0
      for (const row of rows.current.querySelectorAll<HTMLElement>('[data-row]')) {
        if (row.dataset.row === d.id) continue
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
      if (d && ev.type === 'pointerup') layout.reorder(d.order)
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

  return (
    <Sheet
      open={open}
      onClose={close}
      title="Edit Home"
      subtitle="Turn rows on or off, and move them."
      footer={
        <>
          <p className="min-w-0 flex-1 basis-48 text-[13px] text-base-400">
            Off hides a row on every device. Order and folds are this device's.
          </p>
          <button type="button" onClick={layout.resetOrder} disabled={!layout.custom} className={buttonClass('secondary')}>
            Default order
          </button>
        </>
      }
    >
      {layout.saveFailed && (
        <p className="mb-3 flex flex-wrap items-center justify-between gap-2 rounded-xl bg-red-500/10 px-3 py-2 text-sm text-red-300">
          Couldn't save which rows are on.
          <button type="button" onClick={layout.retrySave} className={buttonClass('secondary', 'sm')}>
            Try again
          </button>
        </p>
      )}
      <ul ref={rows} className="space-y-1.5">
        {shown.map((id, i) => {
          const on = layout.shown(id)
          const folded = layout.folded(id)
          const name = homeRowLabel(id)
          return (
            <li
              key={id}
              data-row={id}
              className={cx(
                'flex items-center gap-1 rounded-xl py-1 pr-2 pl-1 transition-[background-color,box-shadow]',
                drag?.id === id ? 'bg-base-700 shadow-panel ring-1 ring-accent-500/50' : 'bg-base-800',
              )}
            >
              <button
                type="button"
                aria-label={`Reorder ${name}; arrow keys move it`}
                title="Drag to reorder"
                data-focus="grip"
                onPointerDown={(e) => startDrag(e, id)}
                onKeyDown={(e) => {
                  if (e.key !== 'ArrowUp' && e.key !== 'ArrowDown') return
                  e.preventDefault()
                  moveTo(id, i + (e.key === 'ArrowUp' ? -1 : 1), 'grip')
                }}
                className="grid size-11 shrink-0 cursor-grab touch-none place-items-center rounded-lg text-base-500 transition-colors hover:bg-base-750 hover:text-base-200 active:cursor-grabbing"
              >
                <GripGlyph />
              </button>
              <div className="min-w-0 flex-1">
                <p className={cx('truncate text-sm font-medium', on ? 'text-base-100' : 'text-base-400')}>{name}</p>
                {(!on || folded) && (
                  <p className="truncate text-[13px] text-base-500">{on ? 'Folded on this device' : 'Off on every device'}</p>
                )}
              </div>
              <MoreMenu
                label={`Move ${name}`}
                menuLabel={`${name} on Home`}
                className="size-11 rounded-full"
                items={[
                  { label: 'Move to top', run: () => moveTo(id, 0, 'menu'), disabled: i === 0 },
                  { label: 'Move up', run: () => moveTo(id, i - 1, 'menu'), disabled: i === 0 },
                  { label: 'Move down', run: () => moveTo(id, i + 1, 'menu'), disabled: i === shown.length - 1 },
                  folded
                    ? { label: 'Unfold', run: () => layout.setFold(id, false) }
                    : { label: 'Fold to its heading', run: () => layout.setFold(id, true), disabled: !on },
                ]}
              />
              <Switch on={on} onChange={(v) => layout.setShown(id, v)} aria-label={name} />
            </li>
          )
        })}
      </ul>
    </Sheet>
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
