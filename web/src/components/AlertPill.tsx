import { cx } from '../lib/format'

export function BellGlyph({ filled, className }: { filled: boolean; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={cx('size-4 shrink-0', className)} aria-hidden>
      <path
        d="M6 16V11a6 6 0 1 1 12 0v5l1.5 2h-15L6 16Zm4 3.5a2 2 0 0 0 4 0"
        fill={filled ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

export function AlertPill({
  on,
  title,
  onToggle,
  pending = false,
  disabled = false,
  wide = false,
  className,
}: {
  on: boolean
  title: string
  onToggle: () => void
  pending?: boolean
  disabled?: boolean
  wide?: boolean
  className?: string
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      disabled={pending || disabled}
      aria-pressed={on}
      aria-label={`${on ? 'Alert on' : 'Alert'}: ${title}`}
      className={cx(
        'press relative inline-flex h-9 shrink-0 items-center justify-center gap-1.5 px-3 text-[13px] font-medium whitespace-nowrap ring-1 transition-colors disabled:opacity-50 pointer-coarse:h-11',
        wide ? 'w-full rounded-lg' : 'tap rounded-full',
        on
          ? 'bg-accent-500/20 text-accent-300 ring-accent-400/40 hover:bg-accent-500/30'
          : 'text-base-100 ring-white/15 hover:bg-white/[0.06]',
        className,
      )}
    >
      <BellGlyph filled={on} />
      {on ? 'Alert on' : 'Alert'}
    </button>
  )
}
