import type { ReactNode } from 'react'
import { cx } from '../lib/format'
import { Switch } from '../components/ui'

export function PanelGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section aria-label={title} className="border-t border-white/[0.06] pb-1 first:border-t-0">
      <h3 className="px-4 pt-3 pb-1 text-[13px] font-semibold text-base-400">{title}</h3>
      {children}
    </section>
  )
}

function Label({ label, caption }: { label: ReactNode; caption?: ReactNode }) {
  return (
    <span className="min-w-0 flex-1 text-sm text-base-100">
      {label}
      {caption && <span className="mt-0.5 block text-xs text-base-400">{caption}</span>}
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
      className="flex min-h-12 w-full items-center gap-3 px-4 py-2 text-left transition-colors hover:bg-white/[0.06]"
    >
      <Label label={label} caption={caption} />
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
      className="flex min-h-12 w-full items-center gap-3 px-4 py-2 text-left transition-colors hover:bg-white/[0.06]"
    >
      <Label label={label} caption={caption} />
      <span className="shrink-0 text-xs text-base-300">{value}</span>
      <span aria-hidden className="text-base-500">
        ›
      </span>
    </button>
  )
}
