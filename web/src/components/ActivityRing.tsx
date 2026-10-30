import type { ReactNode } from 'react'
import type { Activity } from '../lib/activity'
import { cx } from '../lib/format'

const R = 12
const CIRCLE = 2 * Math.PI * R

export function ActivityMark({ activity, done, children }: { activity: Activity; done: boolean; children: ReactNode }) {
  const busy = activity.count > 0
  const share = activity.progress ?? 0.25
  return (
    <span className="relative grid size-7 place-items-center">
      {busy && (
        <svg
          viewBox="0 0 28 28"
          aria-hidden
          className={cx('absolute inset-0 size-7 -rotate-90', activity.progress == null && 'animate-spin')}
        >
          <circle cx="14" cy="14" r={R} fill="none" strokeWidth="2" className="stroke-base-700" />
          <circle
            cx="14"
            cy="14"
            r={R}
            fill="none"
            strokeWidth="2"
            strokeLinecap="round"
            strokeDasharray={CIRCLE}
            strokeDashoffset={CIRCLE * (1 - Math.min(1, Math.max(0.04, share)))}
            className={cx('transition-[stroke-dashoffset] duration-500', activity.trouble ? 'stroke-amber-400' : 'stroke-accent-400')}
          />
        </svg>
      )}
      <span className={cx('grid place-items-center', busy && '[&>svg]:size-3.5')}>{children}</span>
      {busy ? (
        <span
          aria-hidden
          className="absolute -top-1.5 -right-2.5 grid h-[18px] min-w-[18px] place-items-center rounded-full bg-accent-500 px-1 text-xs leading-none font-semibold text-white tabular-nums ring-2 ring-base-950"
        >
          {activity.count}
        </span>
      ) : activity.trouble > 0 ? (
        <span aria-hidden className="absolute top-0 -right-0.5 size-2.5 rounded-full bg-amber-400 ring-2 ring-base-950" />
      ) : (
        done && <span aria-hidden className="absolute top-0 -right-0.5 size-2.5 rounded-full bg-emerald-400 ring-2 ring-base-950" />
      )}
    </span>
  )
}
