import { useCallback, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { cx } from '../lib/format'
import { TextInput } from './ui'
import { useAnchorBox, useDismiss, useModalFocus } from '../lib/hooks'

export function FilterMenu({
  label,
  values,
  options,
  labels,
  multiple = true,
  onChange,
}: {
  label: string
  values: string[]
  options: string[]
  labels?: Record<string, string>
  multiple?: boolean
  onChange: (values: string[]) => void
}) {
  const [open, setOpen] = useState(false)
  const [term, setTerm] = useState('')
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const trigger = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  useModalFocus(menuRef, open)

  const pretty = (option: string) => labels?.[option] ?? option.replace(/_/g, ' ').toLowerCase()
  const box = useAnchorBox(trigger, open)

  const wide = options.length > 12
  const shown = term
    ? options.filter((o) => pretty(o).toLowerCase().includes(term.toLowerCase()))
    : options

  const toggle = (option: string) => {
    if (!multiple) {
      onChange(values[0] === option ? [] : [option])
      setOpen(false)
      return
    }
    onChange(
      values.includes(option) ? values.filter((v) => v !== option) : [...values, option],
    )
  }

  return (
    <div className="relative" ref={ref}>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={cx(
          'flex h-9 items-center gap-1.5 rounded-full px-3.5 text-sm font-medium transition-colors pointer-coarse:min-h-11',
          values.length > 0
            ? 'bg-accent-500/15 text-accent-300 hover:bg-accent-500/25'
            : open
              ? 'bg-base-750 text-white'
              : 'bg-base-800 text-base-200 hover:bg-base-750 hover:text-white',
        )}
      >
        <span className="first-letter:uppercase">
          {!multiple && values.length === 1 ? pretty(values[0]) : label}
        </span>
        {multiple && values.length > 0 && (
          <span className="grid min-w-4 place-items-center rounded-full bg-accent-500 px-1 text-xs font-bold text-white tabular-nums">
            {values.length}
          </span>
        )}
        <svg viewBox="0 0 24 24" className="size-3.5 opacity-60" aria-hidden>
          <path d="m6 9 6 6 6-6" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
        </svg>
      </button>

      {open && box &&
        createPortal(
          <div
            ref={menuRef}
            role="listbox"
            data-portal-menu
            style={{
              top: Math.min(box.bottom + 6, window.innerHeight - 360),
              left: Math.max(
                8,
                Math.min(box.left, window.innerWidth - (wide ? 460 : 248)),
              ),
            }}
            className={cx(
              'fixed z-50 max-w-[calc(100vw-16px)] animate-rise rounded-xl bg-base-750 p-1.5 shadow-panel',
              wide ? 'w-[27rem]' : 'w-60',
            )}
          >
            {wide && (
              <TextInput
                autoFocus
                value={term}
                onChange={(e) => setTerm(e.target.value)}
                placeholder={`Filter ${label.toLowerCase()}…`}
                aria-label={`Filter ${label.toLowerCase()}`}
                className="mb-1.5 w-full"
              />
            )}

            {values.length > 0 && (
              <button
                type="button"
                onClick={() => onChange([])}
                className="mb-1 w-full rounded-lg px-2.5 py-1.5 text-left text-[13px] text-base-400 transition-colors hover:bg-base-700 hover:text-white pointer-coarse:min-h-11"
              >
                Clear {label.toLowerCase()}
              </button>
            )}

            <div
              className={cx(
                'max-h-80 overflow-y-auto scrollbar-thin',
                wide && 'grid grid-cols-2 gap-x-1',
              )}
            >
            {shown.map((option) => {
              const on = values.includes(option)
              return (
                <button
                  key={option}
                  type="button"
                  role="option"
                  aria-selected={on}
                  onClick={() => toggle(option)}
                  className={cx(
                    'flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-sm transition-colors pointer-coarse:min-h-11',
                    on ? 'bg-accent-500/20 text-accent-300' : 'text-base-200 hover:bg-base-700',
                  )}
                >
                  {multiple && (
                    <span
                      className={cx(
                        'grid size-4 shrink-0 place-items-center rounded border transition-colors',
                        on ? 'border-accent-400 bg-accent-500 text-white' : 'border-base-500',
                      )}
                    >
                      {on && (
                        <svg viewBox="0 0 24 24" className="size-3" aria-hidden>
                          <path
                            d="m5 13 4 4L19 7"
                            fill="none"
                            stroke="currentColor"
                            strokeWidth="3"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                          />
                        </svg>
                      )}
                    </span>
                  )}
                  <span className="first-letter:uppercase">{pretty(option)}</span>
                </button>
              )
            })}
            {shown.length === 0 && (
              <p className="col-span-2 px-2.5 py-3 text-center text-[13px] text-base-400">
                Nothing matches “{term}”.
              </p>
            )}
            </div>
          </div>,
          document.body,
        )}
    </div>
  )
}
