import type { ReactNode } from 'react'
import { cx } from '../lib/format'
import { Switch } from '../components/ui'

export function PanelGroup({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="border-t border-white/[0.06] pb-2 first:border-t-0">
      <h3 className="flex items-baseline justify-between gap-2 px-4 pt-3 pb-1.5 text-[11px] font-semibold tracking-wider text-base-400 uppercase">
        {title}
        {note && <span className="font-medium tracking-normal text-base-500 normal-case">{note}</span>}
      </h3>
      {children}
    </section>
  )
}

function Label({ label, caption, selected }: { label: ReactNode; caption?: ReactNode; selected?: boolean }) {
  return (
    <span className={cx('min-w-0 flex-1 text-sm', selected === undefined ? 'text-base-100' : selected ? 'font-medium text-white' : 'text-base-200')}>
      {label}
      {caption && <span className="mt-0.5 block text-xs font-normal text-base-400">{caption}</span>}
    </span>
  )
}

export function Choices({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="radiogroup" aria-label={label}>
      {children}
    </div>
  )
}

export function ChoiceRow({
  label,
  caption,
  selected,
  onClick,
}: {
  label: ReactNode
  caption?: ReactNode
  selected: boolean
  onClick: () => void
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onClick}
      className={cx(
        'mx-2 flex min-h-10 w-[calc(100%-1rem)] items-center gap-3 rounded-lg px-2.5 py-1.5 text-left transition-colors pointer-coarse:min-h-12',
        selected ? 'bg-white/[0.08]' : 'hover:bg-white/[0.05]',
      )}
    >
      <Label label={label} caption={caption} selected={selected} />
      <svg viewBox="0 0 24 24" aria-hidden className={cx('size-5 shrink-0 text-accent-300', !selected && 'invisible')}>
        <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </button>
  )
}

export function SegmentRow<T extends string>({
  label,
  caption,
  value,
  options,
  onChange,
}: {
  label: string
  caption?: ReactNode
  value: T
  options: ReadonlyArray<{ value: T; label: string; name?: string }>
  onChange: (value: T) => void
}) {
  return (
    <div className="flex min-h-12 flex-wrap items-center gap-x-3 gap-y-1.5 px-4 py-2">
      <Label label={label} caption={caption} />
      <div role="radiogroup" aria-label={label} className="flex shrink-0 gap-0.5 rounded-lg bg-base-900 p-0.5">
        {options.map((o) => {
          const on = o.value === value
          return (
            <button
              key={o.value}
              type="button"
              role="radio"
              aria-checked={on}
              aria-label={o.name}
              onClick={() => onChange(o.value)}
              className={cx(
                'h-9 min-w-10 rounded-md px-2.5 text-xs font-medium whitespace-nowrap transition-colors pointer-coarse:h-11',
                on ? 'bg-base-700 text-white' : 'text-base-300 hover:text-white',
              )}
            >
              {o.label}
            </button>
          )
        })}
      </div>
    </div>
  )
}

export function SwitchRow({
  label,
  caption,
  on,
  onChange,
}: {
  label: string
  caption?: ReactNode
  on: boolean
  onChange: (on: boolean) => void
}) {
  return (
    <div className="flex min-h-12 items-center gap-3 px-4 py-2">
      <Label label={label} caption={caption} />
      <Switch on={on} onChange={onChange} aria-label={label} />
    </div>
  )
}

export function ActionRow({
  label,
  caption,
  value,
  onClick,
}: {
  label: string
  caption?: ReactNode
  value: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex min-h-12 w-full items-center gap-3 px-4 py-2 text-left transition-colors hover:bg-white/[0.05]"
    >
      <Label label={label} caption={caption} />
      <span className="min-w-0 truncate text-xs text-base-300">{value}</span>
      <span aria-hidden className="text-base-500">
        ›
      </span>
    </button>
  )
}
