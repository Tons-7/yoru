import { useEffect, type ReactNode } from 'react'
import { bytes, cx } from '../lib/format'
import { SORTS, episodeCount, type Sort } from '../lib/shelf'
import { Button, Select } from './ui'

export const iconButton =
  'tap relative grid size-7 shrink-0 place-items-center rounded-md text-base-400 transition-colors hover:bg-base-800 hover:text-white pointer-coarse:size-11'

const stroke = { fill: 'none', stroke: 'currentColor', strokeWidth: 2, strokeLinecap: 'round', strokeLinejoin: 'round' } as const

export function DownloadIcon({ className = 'size-6' }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <path d="M12 4v11m0 0 4.5-4.5M12 15l-4.5-4.5M5 20h14" {...stroke} strokeWidth="1.8" />
    </svg>
  )
}

export function CheckGlyph({ className = 'size-4' }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <path d="m5 12.5 4.5 4.5L19 7.5" {...stroke} strokeWidth="2.4" />
    </svg>
  )
}

export function PauseGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M9 5v14M15 5v14" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />
    </svg>
  )
}

export function PlayGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M8 5.5v13l10-6.5-10-6.5Z" fill="currentColor" />
    </svg>
  )
}

export function CrossGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="m6 6 12 12M18 6 6 18" {...stroke} />
    </svg>
  )
}

export function RetryGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M20 12a8 8 0 1 1-2.3-5.7M20 4v5h-5" {...stroke} />
    </svg>
  )
}

export function ChevronGlyph({ open }: { open: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className={cx('size-4 shrink-0 transition-transform', open && 'rotate-90')} aria-hidden>
      <path d="m9 6 6 6-6 6" {...stroke} />
    </svg>
  )
}

export function Poster({ src, small = false }: { src?: string | null; small?: boolean }) {
  const size = small ? 'h-15 w-10' : 'h-18 w-12'
  return src ? (
    <img src={src} alt="" loading="lazy" className={cx(size, 'shrink-0 rounded-poster object-cover shadow-card')} />
  ) : (
    <span aria-hidden className={cx(size, 'shrink-0 rounded-poster bg-base-800')} />
  )
}

export function CheckBox({
  checked,
  onChange,
  label,
  disabled,
}: {
  checked: boolean
  onChange: (checked: boolean) => void
  label: string
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      className="tap relative grid size-8 shrink-0 place-items-center rounded-md disabled:opacity-40 pointer-coarse:size-11"
    >
      <span
        className={cx(
          'grid size-6 place-items-center rounded-md text-white ring-2 transition-colors ring-inset',
          checked ? 'bg-accent-500 ring-accent-500' : 'ring-base-600',
        )}
      >
        {checked && <CheckGlyph />}
      </span>
    </button>
  )
}

export function ShelfTools({
  sort,
  onSort,
  selecting,
  onSelecting,
  children,
}: {
  sort: Sort
  onSort: (sort: Sort) => void
  selecting: boolean
  onSelecting: (on: boolean) => void
  children?: ReactNode
}) {
  return (
    <div className="flex items-center justify-between gap-2">
      <Select value={sort} onChange={(v) => onSort(v as Sort)} aria-label="Sort" className="w-40">
        {SORTS.map((s) => (
          <option key={s.value} value={s.value}>
            Sort: {s.label}
          </option>
        ))}
      </Select>
      <div className="flex items-center gap-2">
        {children}
        <Button variant="ghost" onClick={() => onSelecting(!selecting)} aria-pressed={selecting}>
          {selecting ? 'Done' : 'Select'}
        </Button>
      </div>
    </div>
  )
}

export function SelectBar({
  count,
  size,
  confirming,
  onRemove,
  onConfirm,
  onKeep,
  onCancel,
}: {
  count: number
  size: number
  confirming: boolean
  onRemove: () => void
  onConfirm: () => void
  onKeep: () => void
  onCancel: () => void
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Delete' || count === 0 || confirming) return
      const t = e.target as HTMLElement | null
      if (t?.closest('input, textarea, select, [contenteditable="true"]')) return
      e.preventDefault()
      onRemove()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [count, confirming, onRemove])

  return (
    <div className="sticky bottom-[calc(4.5rem+env(safe-area-inset-bottom))] z-30 sm:bottom-4">
      <div
        role="toolbar"
        aria-label="Selected episodes"
        className="flex flex-wrap items-center gap-2 rounded-card bg-base-750 p-2 pl-4 shadow-panel"
      >
        {confirming ? (
          <>
            <p className="min-w-0 flex-1 text-sm text-base-100">
              Remove {episodeCount(count)} for good?
            </p>
            <Button variant="danger" onClick={onConfirm}>
              Remove {episodeCount(count)}
            </Button>
            <Button variant="ghost" onClick={onKeep} autoFocus>
              Keep
            </Button>
          </>
        ) : (
          <>
            <p className="min-w-0 flex-1 text-sm text-base-400">
              {count === 0 ? 'Tick the episodes to remove' : `${episodeCount(count)} selected`}
            </p>
            <Button
              variant="ghost"
              onClick={onRemove}
              disabled={count === 0}
              className="text-red-300 tabular-nums hover:text-red-200"
            >
              {count === 0 ? 'Remove' : `Remove ${episodeCount(count)} · ${bytes(size)}`}
            </Button>
            <Button onClick={onCancel}>Cancel</Button>
          </>
        )}
      </div>
    </div>
  )
}

export function StorageCard({
  used,
  other = 0,
  free,
  total,
  line,
  low,
  onFree,
}: {
  used: number
  other?: number
  free: number | null
  total?: number | null
  line: ReactNode
  low: boolean
  onFree?: () => void
}) {
  const whole = total || used + other + (free ?? 0)
  const share = (n: number) => `${Math.min(100, (n / whole) * 100)}%`
  return (
    <section aria-label="Space" className="surface space-y-3 p-4">
      {whole > 0 && free != null && (
        <div aria-hidden className="flex h-2 gap-0.5 overflow-hidden rounded-full bg-base-800 lg:h-3">
          {used > 0 && <span style={{ width: share(used) }} className={cx('h-full min-w-1', low ? 'bg-amber-400' : 'bg-accent-500')} />}
          {other > 0 && <span style={{ width: share(other) }} className="h-full min-w-1 bg-base-600" />}
        </div>
      )}
      <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1">
        <p className={cx('min-w-0 text-sm tabular-nums', low ? 'text-amber-300' : 'text-base-400')}>{line}</p>
        {onFree && (
          <button
            type="button"
            onClick={onFree}
            className="tap relative -mx-2 shrink-0 rounded-lg px-2 py-1.5 text-sm font-medium text-accent-300 transition-colors hover:bg-base-800 pointer-coarse:min-h-11 lg:hidden"
          >
            Free up space
          </button>
        )}
      </div>
    </section>
  )
}

export function PanelCard({ title, children, className }: { title: string; children: ReactNode; className?: string }) {
  return (
    <section aria-label={title} className={cx('surface space-y-3 p-4', className)}>
      <h2 className="text-sm font-semibold text-base-100">{title}</h2>
      {children}
    </section>
  )
}

export function FreeRow({
  title,
  detail,
  action,
}: {
  title: ReactNode
  detail: ReactNode
  action: ReactNode
}) {
  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-2 py-2.5">
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm text-base-100">{title}</p>
        <p className="text-[13px] text-base-400 tabular-nums">{detail}</p>
      </div>
      <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">{action}</div>
    </li>
  )
}
