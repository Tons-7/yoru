import type { Activity } from '../lib/activity'
import { cx } from '../lib/format'

export function Avatar({ name, className, letter = 'text-sm' }: { name?: string; className?: string; letter?: string }) {
  const initial = name ? Array.from(name.trim())[0]?.toUpperCase() : undefined
  return (
    <span
      aria-hidden
      className={cx(
        'grid shrink-0 place-items-center rounded-full',
        initial
          ? cx('bg-gradient-to-br from-accent-400 to-accent-600 font-display font-semibold text-white', letter)
          : 'bg-base-800 text-base-200',
        className,
      )}
    >
      {initial ?? <UserIcon />}
    </span>
  )
}

export function UserIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-[60%]" aria-hidden>
      <circle cx="12" cy="8.5" r="3.5" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path d="M5 20c1-3.5 3.8-5 7-5s6 1.5 7 5" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  )
}

const R = 8
const CIRCLE = 2 * Math.PI * R

export function MiniRing({ activity }: { activity: Activity }) {
  if (activity.count === 0) return null
  const share = activity.progress ?? 0.25
  return (
    <svg
      viewBox="0 0 20 20"
      aria-hidden
      className={cx('size-5 shrink-0 -rotate-90', activity.progress == null && 'animate-spin')}
    >
      <circle cx="10" cy="10" r={R} fill="none" strokeWidth="2.5" className="stroke-base-700" />
      <circle
        cx="10"
        cy="10"
        r={R}
        fill="none"
        strokeWidth="2.5"
        strokeLinecap="round"
        strokeDasharray={CIRCLE}
        strokeDashoffset={CIRCLE * (1 - Math.min(1, Math.max(0.04, share)))}
        className={cx('transition-[stroke-dashoffset] duration-500', activity.trouble ? 'stroke-amber-400' : 'stroke-accent-400')}
      />
    </svg>
  )
}

export function Chevron({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 20 20" className={cx('size-4 shrink-0 text-base-500', className)} aria-hidden>
      <path d="m8 5 5 5-5 5" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}
