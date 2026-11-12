import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { cx } from '../lib/format'
import { nextClass, stepPath, type Screen } from '../lib/setupGuide'
import { Switch } from './ui'

export function Tick({ className, label }: { className?: string; label?: string }) {
  return (
    <svg viewBox="0 0 24 24" role={label ? 'img' : undefined} aria-label={label} aria-hidden={label ? undefined : true} className={cx('size-[15px]', className)}>
      <path d="m5 12 5 5 9-10" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

export function Frame({ title, lead, children }: { title: string; lead?: ReactNode; children: ReactNode }) {
  return (
    <>
      <div className="flex flex-col gap-2.5">
        <h1 tabIndex={-1} className="font-display text-3xl font-bold tracking-tight text-white outline-none sm:text-[38px] sm:leading-tight">
          {title}
        </h1>
        {lead && <p className="max-w-[620px] text-base leading-normal text-base-300">{lead}</p>}
      </div>
      {children}
    </>
  )
}

export function Footer({ back, skip, children }: { back: Screen; skip?: Screen; children: ReactNode }) {
  return (
    <footer className="mt-auto flex flex-wrap items-center gap-3 border-t border-base-850 pt-5 pb-7">
      <Link to={stepPath(back)} className="inline-flex h-11 items-center rounded-xl px-4 text-[15px] font-medium text-base-200 transition-colors hover:text-white">
        Back
      </Link>
      <span className="flex-1" />
      {skip && (
        <Link to={stepPath(skip)} className="inline-flex h-11 items-center px-3 text-sm text-base-400 transition-colors hover:text-white">
          Skip this step
        </Link>
      )}
      {children}
    </footer>
  )
}

export function Continue({ onClick, label = 'Continue' }: { onClick: () => void; label?: string }) {
  return (
    <button type="button" onClick={onClick} className={nextClass}>
      {label}
      <svg viewBox="0 0 24 24" aria-hidden className="size-4">
        <path d="M5 12h14M13 6l6 6-6 6" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </button>
  )
}

export function Card({ className, children, label }: { className?: string; children: ReactNode; label?: string }) {
  return (
    <section aria-label={label} className={cx('rounded-2xl border border-base-800 bg-base-900', className)}>
      {children}
    </section>
  )
}

export function Choices<T extends string>({
  label,
  name,
  value,
  options,
  onChange,
  columns = 3,
}: {
  label: string
  name: string
  value: string
  options: ReadonlyArray<{ value: T; title: string; text?: string }>
  onChange: (value: T) => void
  columns?: 2 | 3
}) {
  return (
    <fieldset className="min-w-0">
      <legend className="mb-2.5 text-[13px] font-medium text-base-300">{label}</legend>
      <div className={cx('grid gap-3', columns === 2 ? 'sm:grid-cols-2' : 'sm:grid-cols-3')}>
        {options.map((option) => {
          const active = option.value === value
          return (
            <label
              key={option.value}
              className={cx(
                'flex min-h-[60px] cursor-pointer items-center gap-3 rounded-[14px] border px-4 py-2 transition-colors',
                active ? 'border-accent-500 bg-accent-500/10' : 'border-base-800 bg-base-900 hover:border-base-700',
              )}
            >
              <input
                type="radio"
                name={name}
                checked={active}
                onChange={() => onChange(option.value)}
                className="size-[18px] shrink-0 accent-accent-400"
              />
              <span className="min-w-0">
                <span className="block text-[15px] font-semibold text-white">{option.title}</span>
                {option.text && <span className="block text-xs text-base-400">{option.text}</span>}
              </span>
            </label>
          )
        })}
      </div>
    </fieldset>
  )
}

export function ToggleRow({
  title,
  hint,
  on,
  onChange,
  disabled,
  children,
}: {
  title: string
  hint?: string
  on?: boolean
  onChange?: (on: boolean) => void
  disabled?: boolean
  children?: ReactNode
}) {
  return (
    <li className="flex min-h-[76px] flex-wrap items-center gap-x-6 gap-y-2 border-b border-base-850 px-5 py-3 last:border-b-0 sm:px-7">
      <span className="min-w-0 flex-1 basis-56">
        <span className="block text-[15px] font-semibold text-white">{title}</span>
        {hint && <span className="mt-0.5 block text-[13px] text-base-400">{hint}</span>}
      </span>
      {children ?? <Switch on={!!on} onChange={(v) => onChange?.(v)} disabled={disabled} aria-label={title} />}
    </li>
  )
}
