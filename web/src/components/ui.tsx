import {
  useContext,
  useEffect,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
} from 'react'
import { Link } from 'react-router-dom'
import { cx } from '../lib/format'
import { buttonClass, ControlLabel, type ButtonSize, type ButtonVariant } from '../lib/ui'

export function Button({
  variant = 'secondary',
  size = 'md',
  className,
  type = 'button',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: ButtonSize }) {
  return <button type={type} className={cx(buttonClass(variant, size), className)} {...props} />
}

export function LinkButton({
  to,
  variant = 'secondary',
  size = 'md',
  className,
  children,
  ...props
}: {
  to: string
  variant?: ButtonVariant
  size?: ButtonSize
  className?: string
  children: ReactNode
  title?: string
  style?: React.CSSProperties
  'aria-label'?: string
}) {
  return (
    <Link to={to} className={cx(buttonClass(variant, size), className)} {...props}>
      {children}
    </Link>
  )
}

export function FilterToggle({
  on,
  onChange,
  children,
}: {
  on: boolean
  onChange: (on: boolean) => void
  children: ReactNode
}) {
  return (
    <button
      type="button"
      aria-pressed={on}
      onClick={() => onChange(!on)}
      className={cx(
        'inline-flex items-center gap-1.5 rounded-full px-3 py-1.5 text-sm font-medium ring-1 transition-colors pointer-coarse:min-h-11',
        on
          ? 'bg-accent-500/20 text-accent-300 ring-accent-500/40'
          : 'bg-base-850 text-base-300 ring-white/[0.06] hover:text-white',
      )}
    >
      <span
        className={cx(
          'grid size-3.5 place-items-center rounded-full text-[9px] transition-colors',
          on ? 'bg-accent-500 text-white' : 'bg-base-700 text-transparent',
        )}
      >
        ✓
      </span>
      {children}
    </button>
  )
}

export function PageHeader({
  title,
  subtitle,
  meta,
  actions,
}: {
  title: string
  subtitle?: string
  meta?: ReactNode
  actions?: ReactNode
}) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-3">
      <div className="min-w-0">
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <h1 className="page-title">{title}</h1>
          {meta && <span className="text-sm text-base-500">{meta}</span>}
        </div>
        {subtitle && <p className="mt-1 text-sm text-base-400">{subtitle}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </div>
  )
}

export const inputClass =
  'h-10 rounded-xl bg-base-800 px-3 text-sm text-base-100 transition-[box-shadow] placeholder:text-base-500 focus:outline-hidden focus-visible:ring-2 focus-visible:ring-accent-400 disabled:opacity-50 pointer-coarse:h-12'

export function TextInput({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  const rowLabel = useContext(ControlLabel)
  return <input aria-label={rowLabel} className={cx(inputClass, className)} {...props} />
}

export function SearchInput({
  className,
  inputClassName,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & { inputClassName?: string }) {
  return (
    <div className={cx('relative', className)}>
      <svg
        viewBox="0 0 24 24"
        aria-hidden
        className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-base-500"
      >
        <circle cx="11" cy="11" r="6.5" fill="none" stroke="currentColor" strokeWidth="1.8" />
        <path d="m16 16 4.5 4.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      </svg>
      <input
        type="search"
        className={cx(inputClass, 'w-full py-2 pl-9 [&::-webkit-search-cancel-button]:hidden', inputClassName)}
        {...props}
      />
    </div>
  )
}

export function ConfirmInline({
  label,
  onConfirm,
  onCancel,
  tone = 'danger',
  size = 'md',
  pending = false,
  note,
}: {
  label: ReactNode
  onConfirm: () => void
  onCancel: () => void
  tone?: 'danger' | 'accent'
  size?: ButtonSize
  pending?: boolean
  note?: ReactNode
}) {
  return (
    <span
      className="contents"
      onKeyDown={(e) => {
        if (e.key !== 'Escape') return
        e.preventDefault()
        e.stopPropagation()
        onCancel()
      }}
    >
      {note && <span className="text-xs text-base-400 max-sm:basis-full">{note}</span>}
      <Button variant={tone === 'danger' ? 'danger' : 'primary'} size={size} onClick={onConfirm} disabled={pending}>
        {label}
      </Button>
      <Button variant="ghost" size={size} onClick={onCancel} autoFocus>
        Cancel
      </Button>
    </span>
  )
}

export function Pager({
  page,
  hasMore,
  last,
  onPage,
}: {
  page: number
  hasMore: boolean
  last?: number
  onPage: (page: number) => void
}) {
  if (page <= 1 && !hasMore) return null
  const go = (n: number) => {
    onPage(n)
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }
  return (
    <nav aria-label="Pages" className="flex items-center justify-center gap-3 pt-2">
      <Button disabled={page <= 1} onClick={() => go(page - 1)}>
        Previous
      </Button>
      <span className="text-sm text-base-400 tabular-nums">
        Page <span className="font-semibold text-white">{page}</span>
        {last !== undefined && ` of ${last.toLocaleString()}`}
      </span>
      <Button disabled={!hasMore} onClick={() => go(page + 1)}>
        Next
      </Button>
    </nav>
  )
}

export function Spinner({ className }: { className?: string }) {
  return (
    <div
      role="status"
      aria-label="Loading"
      className={cx(
        'size-5 animate-spin rounded-full border-2 border-base-600 border-t-accent-400',
        className,
      )}
    />
  )
}

export function Centered({ children }: { children: ReactNode }) {
  return <div className="flex min-h-64 items-center justify-center">{children}</div>
}

export function Empty({
  title,
  hint,
  icon,
  action,
}: {
  title: string
  hint?: string
  icon?: ReactNode
  action?: ReactNode
}) {
  return (
    <Centered>
      <div className="flex max-w-sm flex-col items-center px-4 text-center">
        <span className="mb-4 grid size-14 place-items-center rounded-2xl bg-base-900 text-base-400">
          {icon ?? <EmptyIcon />}
        </span>
        <p className="font-medium text-base-100">{title}</p>
        {hint && <p className="mt-1 text-sm text-balance text-base-400">{hint}</p>}
        {action && <div className="mt-4">{action}</div>}
      </div>
    </Centered>
  )
}

function EmptyIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-6" aria-hidden>
      <path
        d="M4 7.5 12 3l8 4.5v9L12 21l-8-4.5v-9Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
      <path d="M4 7.5 12 12l8-4.5M12 12v9" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
    </svg>
  )
}

export function ErrorState({ error, retry }: { error: unknown; retry?: () => void }) {
  const message = error instanceof Error ? error.message : 'Something went wrong'
  return (
    <Centered>
      <div className="flex max-w-md flex-col items-center px-4 text-center">
        <span className="mb-4 grid size-14 place-items-center rounded-2xl bg-recap/10 text-red-300 ring-1 ring-recap/25">
          <svg viewBox="0 0 24 24" className="size-6" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden>
            <path d="M12 8v5m0 3.5v.01M10.3 3.9 2.6 17.5A2 2 0 0 0 4.3 20.5h15.4a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </span>
        <p className="font-medium text-base-100">That didn't load</p>
        <p className="mt-1 text-sm text-base-400">{message}</p>
        {retry && (
          <Button className="mt-4" onClick={retry}>
            Try again
          </Button>
        )}
      </div>
    </Centered>
  )
}

interface SaveState {
  isPending: boolean
  isSuccess: boolean
  isError: boolean
  error: unknown
  submittedAt: number
}

export function SaveHint({ state }: { state: SaveState }) {
  const [faded, setFaded] = useState<number>()
  useEffect(() => {
    if (!state.isSuccess) return
    const t = window.setTimeout(() => setFaded(state.submittedAt), 2000)
    return () => window.clearTimeout(t)
  }, [state.isSuccess, state.submittedAt])
  const shown = state.isSuccess && faded !== state.submittedAt
  if (state.isPending) return <span className="text-xs text-base-500">Saving…</span>
  if (state.isError) {
    return <span className="text-xs text-recap">{state.error instanceof Error ? state.error.message : 'Not saved'}</span>
  }
  return shown ? <span className="text-xs text-emerald-300">Saved</span> : null
}

export function RefreshFailed({ retry }: { retry: () => void }) {
  return (
    <p role="status" className="flex flex-wrap items-center gap-2 text-xs text-base-400">
      <span>Couldn't refresh, showing what was loaded.</span>
      <button onClick={retry} className="text-accent-300 underline-offset-2 hover:underline">
        Try again
      </button>
    </p>
  )
}

export function Skeleton({ className }: { className?: string }) {
  return (
    <div
      data-loading
      className={cx(
        'animate-shimmer rounded-lg bg-base-850 bg-[linear-gradient(90deg,transparent_25%,var(--color-base-800)_50%,transparent_75%)] bg-[length:200%_100%]',
        className,
      )}
    />
  )
}

function keepInStrip(el: HTMLElement | null) {
  const strip = el?.parentElement
  if (!el || !strip) return
  const left = el.getBoundingClientRect().left - strip.getBoundingClientRect().left + strip.scrollLeft
  if (left < strip.scrollLeft) strip.scrollLeft = left
  else if (left + el.offsetWidth > strip.scrollLeft + strip.clientWidth) strip.scrollLeft = left + el.offsetWidth - strip.clientWidth
}

export function Segmented<T extends string>({
  options,
  value,
  onChange,
  size = 'md',
  full = false,
  className,
  label,
}: {
  options: ReadonlyArray<{ value: T; label: string; count?: number }>
  value: T
  onChange: (value: T) => void
  size?: 'sm' | 'md'
  full?: boolean
  className?: string
  label?: string
}) {
  return (
    <div
      role={label ? 'radiogroup' : 'tablist'}
      aria-label={label}
      className={cx(
        'max-w-full gap-0.5 overflow-x-auto rounded-xl bg-base-900 p-1 no-scrollbar',
        full ? 'flex w-full' : 'inline-flex',
        className,
      )}
    >
      {options.map((option) => {
        const active = option.value === value
        return (
          <button
            key={option.value}
            role={label ? 'radio' : 'tab'}
            aria-selected={label ? undefined : active}
            aria-checked={label ? active : undefined}
            ref={active ? keepInStrip : undefined}
            onClick={() => onChange(option.value)}
            className={cx(
              'shrink-0 rounded-lg font-medium whitespace-nowrap transition-colors pointer-coarse:min-h-11',
              full && 'flex-1',
              size === 'sm' ? 'h-7 px-2.5 text-xs' : 'h-8 px-3.5 text-sm',
              active
                ? 'bg-base-750 font-semibold text-white'
                : option.count === 0
                  ? 'text-base-500 hover:text-base-300'
                  : 'text-base-300 hover:text-white',
            )}
          >
            {option.label}
            {option.count !== undefined && (
              <span className={cx('ml-1.5 tabular-nums', active ? 'text-base-300' : 'text-base-500')}>
                {option.count}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}

export function ProgressBar({ value, className, label }: { value: number; className?: string; label?: string }) {
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(Math.min(100, Math.max(0, value)))}
      className={cx('h-1 w-full overflow-hidden rounded-full bg-base-800', className)}
    >
      <div
        className="h-full rounded-full bg-accent-400 transition-[width] duration-300"
        style={{ width: `${Math.min(100, Math.max(0, value))}%` }}
      />
    </div>
  )
}

export function Badge({
  children,
  tone = 'neutral',
}: {
  children: ReactNode
  tone?: 'neutral' | 'filler' | 'recap' | 'accent' | 'success' | 'warning' | 'sky' | 'overlay' | 'overlay-success' | 'overlay-neutral'
}) {
  const tones = {
    neutral: 'bg-base-800 text-base-300',
    filler: 'bg-filler/15 text-amber-300',
    recap: 'bg-recap/15 text-red-300',
    accent: 'bg-accent-500/20 text-accent-300',
    success: 'bg-emerald-500/15 text-emerald-300',
    warning: 'bg-amber-500/15 text-amber-300',
    sky: 'bg-sky-400/15 text-sky-300',
    overlay: 'bg-base-950/85 text-base-100 backdrop-blur-sm',
    'overlay-success': 'bg-base-950/85 text-emerald-300 backdrop-blur-sm',
    'overlay-neutral': 'bg-base-950/85 text-base-200 backdrop-blur-sm',
  }
  return (
    <span
      className={cx(
        'rounded-md px-1.5 py-0.5 text-xs leading-tight font-semibold whitespace-nowrap',
        tones[tone],
      )}
    >
      {children}
    </span>
  )
}

export function Select({
  value,
  onChange,
  children,
  className,
  'aria-label': label,
}: {
  value: string
  onChange: (value: string) => void
  children: ReactNode
  className?: string
  'aria-label'?: string
}) {
  const rowLabel = useContext(ControlLabel)
  return (
    <div className={cx('relative shrink-0', className)}>
      <select
        value={value}
        aria-label={label ?? rowLabel}
        onChange={(e) => onChange(e.target.value)}
        className="h-10 w-full appearance-none rounded-xl bg-base-800 pr-8 pl-3 text-sm text-base-100 transition-colors hover:bg-base-750 focus:outline-hidden focus-visible:ring-2 focus-visible:ring-accent-400 pointer-coarse:h-12"
      >
        {children}
      </select>
      <svg
        viewBox="0 0 20 20"
        aria-hidden
        className="pointer-events-none absolute top-1/2 right-2.5 size-3.5 -translate-y-1/2 text-base-400"
      >
        <path
          d="M6 8l4 4 4-4"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.75"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
    </div>
  )
}

export function LabelledSwitch({ label, on, onChange }: { label: string; on: boolean; onChange: (on: boolean) => void }) {
  return (
    <label className="flex cursor-pointer items-center gap-2.5 text-sm text-base-200 pointer-coarse:min-h-11">
      {label}
      <Switch on={on} onChange={onChange} aria-label={label} />
    </label>
  )
}

export function Switch({
  on,
  onChange,
  disabled,
  'aria-label': label,
}: {
  on: boolean
  onChange: (on: boolean) => void
  disabled?: boolean
  'aria-label'?: string
}) {
  const rowLabel = useContext(ControlLabel)
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label ?? rowLabel}
      disabled={disabled}
      onClick={() => onChange(!on)}
      className={cx(
        'tap relative h-6 w-11 shrink-0 rounded-full transition-colors duration-[120ms] disabled:opacity-60 pointer-coarse:h-8 pointer-coarse:w-[3.25rem]',
        'focus:outline-hidden focus-visible:ring-2 focus-visible:ring-white focus-visible:ring-offset-2 focus-visible:ring-offset-base-950',
        on ? 'bg-accent-500' : 'bg-base-700',
      )}
    >
      <span
        className={cx(
          'absolute top-0.5 left-0.5 size-5 rounded-full bg-white shadow-sm transition-transform duration-[120ms] pointer-coarse:size-7',
          on && 'translate-x-5',
        )}
      />
    </button>
  )
}
