import type { ReactNode } from 'react'
import { cx } from '../lib/format'

export interface Starting {
  step: 0 | 1 | 2
  status: string
  seconds: number
  percent?: number
  stuck?: string
}

const STEPS = ['Find a release', 'Connect', 'Load'] as const

const BUTTON =
  'press relative inline-flex min-h-11 items-center justify-center rounded-xl px-4 text-sm font-semibold transition-colors'

export function StartingSteps({
  starting,
  backdrop,
  onPick,
  onCancel,
  onRetry,
  className,
  children,
}: {
  starting: Starting
  backdrop?: string
  onPick?: () => void
  onCancel?: () => void
  onRetry?: () => void
  className?: string
  children?: ReactNode
}) {
  const { step, status, seconds, percent, stuck } = starting
  const clock = seconds >= 20 ? `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}` : ''
  const known = step === 2 && percent != null && percent > 0
  return (
    <div
      className={cx(
        'absolute inset-0 flex flex-col items-center justify-center overflow-hidden bg-black px-4 pt-12 pb-3 text-center sm:p-6',
        className,
      )}
    >
      {backdrop && (
        <img
          src={backdrop}
          alt=""
          aria-hidden
          className="pointer-events-none absolute inset-0 size-full scale-110 object-cover opacity-20 blur-xl"
        />
      )}
      <div className="relative grid max-w-md justify-items-center gap-2 sm:gap-3">
        {stuck ? (
          <p className="text-sm font-semibold text-white sm:text-[15px]">{stuck}</p>
        ) : (
          <ol aria-label="Starting the episode" className="flex flex-wrap items-center justify-center gap-x-2 gap-y-1 text-[13px] sm:text-sm">
            {STEPS.map((label, i) => (
              <li
                key={label}
                aria-current={i === step ? 'step' : undefined}
                className={cx(
                  'flex items-center gap-1.5',
                  i < step ? 'text-emerald-300' : i === step ? 'font-semibold text-white' : 'text-base-400',
                )}
              >
                {i > 0 && <span aria-hidden className="mr-0.5 h-px w-3 bg-white/25 sm:w-5" />}
                {i < step && (
                  <svg viewBox="0 0 24 24" aria-hidden className="size-4">
                    <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                )}
                {label}
                {i === step && known && <span className="tabular-nums">{Math.round(percent)}%</span>}
                {i < step && <span className="sr-only">, done</span>}
              </li>
            ))}
          </ol>
        )}
        {!stuck && (
          <div
            role="progressbar"
            aria-label={STEPS[step]}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={known ? Math.round(percent) : undefined}
            className="h-1 w-[200px] overflow-hidden rounded-full bg-white/15"
          >
            {known ? (
              <div className="h-full rounded-full bg-accent-400 transition-[width] duration-300" style={{ width: `${percent}%` }} />
            ) : (
              <div className="h-full w-full animate-shimmer bg-[linear-gradient(90deg,transparent_15%,var(--color-accent-400)_50%,transparent_85%)] bg-[length:200%_100%]" />
            )}
          </div>
        )}
        <p role="status" className="line-clamp-3 text-[13px] break-words text-base-200 tabular-nums sm:text-[15px]">
          {status}
          {clock && ` · ${clock}`}
        </p>
        {(onRetry || onPick || onCancel) && (
          <div className="flex flex-wrap justify-center gap-2 pt-0.5">
            {stuck && onRetry && (
              <button type="button" onClick={onRetry} className={cx(BUTTON, 'bg-white text-base-950 hover:bg-base-100')}>
                Retry
              </button>
            )}
            {onPick && (
              <button type="button" onClick={onPick} className={cx(BUTTON, 'bg-white/15 text-white ring-1 ring-white/20 hover:bg-white/25')}>
                Pick a release
              </button>
            )}
            {onCancel && (
              <button type="button" onClick={onCancel} className={cx(BUTTON, 'bg-white/15 text-white ring-1 ring-white/20 hover:bg-white/25')}>
                Cancel
              </button>
            )}
          </div>
        )}
        {children}
      </div>
    </div>
  )
}
