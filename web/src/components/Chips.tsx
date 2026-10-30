import type { ReactNode } from 'react'
import { cx } from '../lib/format'

const chip =
  'press tap relative inline-flex h-9 shrink-0 items-center gap-1.5 rounded-full px-3.5 text-sm font-medium whitespace-nowrap ring-1 transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-400'

const tone = (on: boolean) =>
  on
    ? 'bg-accent-500/20 text-accent-300 ring-accent-400/50'
    : 'bg-base-850 text-base-200 ring-white/[0.08] hover:bg-base-800 hover:text-white'

export function ChipRow({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cx(
        'no-scrollbar -mx-4 flex items-center gap-2 overflow-x-auto px-4 py-1 sm:mx-0 sm:flex-wrap sm:overflow-visible sm:px-0',
        className,
      )}
    >
      {children}
    </div>
  )
}

export function ToggleChip({ on, onChange, children }: { on: boolean; onChange: (on: boolean) => void; children: ReactNode }) {
  return (
    <button type="button" aria-pressed={on} onClick={() => onChange(!on)} className={cx(chip, tone(on))}>
      <svg viewBox="0 0 24 24" className={cx('size-3.5', !on && 'hidden')} aria-hidden>
        <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2.6" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      {children}
    </button>
  )
}

export function ChoiceChips<T extends string>({
  label,
  options,
  value,
  onChange,
}: {
  label: string
  options: ReadonlyArray<{ value: T; label: string }>
  value: T
  onChange: (value: T) => void
}) {
  return (
    <div role="radiogroup" aria-label={label} className="contents">
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={option.value === value}
          onClick={() => onChange(option.value)}
          className={cx(chip, tone(option.value === value))}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

export function ChipDivider() {
  return <span aria-hidden className="h-5 w-px shrink-0 bg-base-750" />
}
