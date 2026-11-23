import {
  useContext,
  useEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type KeyboardEvent,
  type ReactNode,
} from 'react'
import { Link } from 'react-router-dom'
import { cx, motion } from '../lib/format'
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

export { ToggleChip as FilterToggle } from './Chips'

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
  onClear,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & { inputClassName?: string; onClear?: () => void }) {
  const box = useRef<HTMLInputElement>(null)
  const filled = onClear && String(props.value ?? '') !== ''
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
        autoComplete="off"
        data-lpignore="true"
        data-1p-ignore="true"
        data-form-type="other"
        className={cx(inputClass, 'w-full py-2 pl-9 [&::-webkit-search-cancel-button]:hidden', onClear && 'pr-9', inputClassName)}
        {...props}
        ref={box}
        onKeyDown={(e) => {
          props.onKeyDown?.(e)
          if (e.key === 'Escape' && filled && !e.defaultPrevented) onClear()
        }}
      />
      {filled && (
        <button
          type="button"
          aria-label="Clear search"
          onClick={() => {
            onClear()
            box.current?.focus()
          }}
          className="tap absolute top-1/2 right-1.5 grid size-7 -translate-y-1/2 place-items-center rounded-full text-base-400 transition-colors hover:bg-base-700 hover:text-white"
        >
          <svg viewBox="0 0 20 20" aria-hidden className="size-3.5">
            <path d="M5 5l10 10M15 5L5 15" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
          </svg>
        </button>
      )}
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
  const cancel = (e: { currentTarget: Element }) => {
    const around = e.currentTarget.closest('span.contents')?.parentElement ?? null
    onCancel()
    requestAnimationFrame(() => {
      if (around?.isConnected && !around.contains(document.activeElement)) {
        around.querySelector<HTMLElement>('button:not([disabled]), a[href], [tabindex="0"]')?.focus()
      }
    })
  }
  return (
    <span
      className="contents"
      onKeyDown={(e) => {
        if (e.key !== 'Escape') return
        e.preventDefault()
        e.stopPropagation()
        cancel(e)
      }}
    >
      {note && <span className="text-xs text-base-400 max-sm:basis-full">{note}</span>}
      <Button variant={tone === 'danger' ? 'danger' : 'primary'} size={size} onClick={onConfirm} disabled={pending}>
        {label}
      </Button>
      <Button variant="ghost" size={size} onClick={cancel} autoFocus>
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
    window.scrollTo({ top: 0, behavior: motion() })
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
  card = false,
}: {
  title: string
  hint?: ReactNode
  icon?: ReactNode
  action?: ReactNode
  card?: boolean
}) {
  if (card) {
    return (
      <section className="surface flex flex-col items-center px-6 py-8 text-center">
        <span className="mb-4 grid size-14 place-items-center rounded-2xl bg-base-800 text-base-300">
          {icon ?? <EmptyIcon />}
        </span>
        <p className="text-[17px] font-semibold text-base-100">{title}</p>
        {hint && <p className="mt-1 max-w-sm text-[15px] text-balance text-base-400">{hint}</p>}
        {action && <div className="mt-5 flex flex-wrap justify-center gap-2">{action}</div>}
      </section>
    )
  }
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
    return <span className="text-xs text-red-300">{state.error instanceof Error ? state.error.message : 'Not saved'}</span>
  }
  return shown ? <span className="text-xs text-emerald-300">Saved</span> : null
}

export function RefreshFailed({ retry }: { retry: () => void }) {
  return (
    <div role="status" className="flex flex-wrap items-center gap-2 text-[13px] text-base-400">
      <span>Couldn't refresh, showing what was loaded.</span>
      <Button size="sm" onClick={retry}>
        Try again
      </Button>
    </div>
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
  const rowLabel = useContext(ControlLabel)
  const name = label ?? rowLabel ?? undefined
  const step = (e: KeyboardEvent<HTMLDivElement>) => {
    const at = options.findIndex((o) => o.value === value)
    const next =
      e.key === 'ArrowRight' || e.key === 'ArrowDown'
        ? (at + 1) % options.length
        : e.key === 'ArrowLeft' || e.key === 'ArrowUp'
          ? (at - 1 + options.length) % options.length
          : e.key === 'Home'
            ? 0
            : e.key === 'End'
              ? options.length - 1
              : -1
    if (next < 0 || next === at) return
    e.preventDefault()
    onChange(options[next].value)
    const buttons = e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="radio"]')
    buttons[next]?.focus()
  }
  return (
    <div
      role="radiogroup"
      aria-label={name}
      onKeyDown={step}
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
            role="radio"
            aria-checked={active}
            tabIndex={active ? 0 : -1}
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

export function SwitchTrack({ on }: { on: boolean }) {
  return (
    <span
      aria-hidden
      className={cx(
        'relative h-6 w-11 shrink-0 rounded-full transition-colors duration-120 pointer-coarse:h-8 pointer-coarse:w-[3.25rem]',
        on ? 'bg-accent-500' : 'bg-base-700',
      )}
    >
      <span
        className={cx(
          'absolute top-0.5 left-0.5 size-5 rounded-full bg-white shadow-sm transition-transform duration-120 pointer-coarse:size-7',
          on && 'translate-x-5',
        )}
      />
    </span>
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
        'tap relative h-6 w-11 shrink-0 rounded-full transition-colors duration-120 disabled:opacity-60 pointer-coarse:h-8 pointer-coarse:w-[3.25rem]',
        'focus:outline-hidden focus-visible:ring-2 focus-visible:ring-white focus-visible:ring-offset-2 focus-visible:ring-offset-base-950',
        on ? 'bg-accent-500' : 'bg-base-700',
      )}
    >
      <span
        className={cx(
          'absolute top-0.5 left-0.5 size-5 rounded-full bg-white shadow-sm transition-transform duration-120 pointer-coarse:size-7',
          on && 'translate-x-5',
        )}
      />
    </button>
  )
}

export function CloseButton({ onClick, label = 'Close' }: { onClick: () => void; label?: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      className="tap relative grid size-9 shrink-0 place-items-center rounded-full text-base-400 transition-colors hover:bg-base-800 hover:text-base-100 pointer-coarse:size-11"
    >
      <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden>
        <path d="m6 6 12 12M18 6 6 18" />
      </svg>
    </button>
  )
}

export function ScorePicker({
  value,
  onChange,
  pending = false,
  small = false,
  label = 'Your score',
}: {
  value: number
  onChange: (score: number) => void
  pending?: boolean
  small?: boolean
  label?: string
}) {
  const cell = small ? 'h-8 text-xs pointer-coarse:h-11' : 'h-10 text-sm pointer-coarse:h-11'
  return (
    <div role="group" aria-label={label} className="grid grid-cols-5 gap-1.5">
      {Array.from({ length: 10 }, (_, i) => i + 1).map((n) => (
        <button
          key={n}
          type="button"
          aria-pressed={n === value}
          aria-label={`Score ${n}`}
          disabled={pending}
          onClick={() => onChange(n === value ? 0 : n)}
          className={cx(
            'rounded-lg font-semibold tabular-nums transition-colors',
            cell,
            n === value ? 'bg-accent-500 text-white' : 'bg-base-800 text-base-100 hover:bg-base-700',
          )}
        >
          {n}
        </button>
      ))}
      <button
        type="button"
        aria-pressed={value === 0}
        disabled={pending}
        onClick={() => onChange(0)}
        className={cx(
          'col-span-5 rounded-lg font-medium transition-colors',
          cell,
          value === 0 ? 'bg-base-700 text-base-100' : 'bg-base-800 text-base-300 hover:bg-base-700',
        )}
      >
        No score
      </button>
    </div>
  )
}
