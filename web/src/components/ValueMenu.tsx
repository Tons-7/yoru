import { useCallback, useContext, useId, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { cx } from '../lib/format'
import { useAnchorBox, useDismiss, useMediaQuery, useModalFocus } from '../lib/hooks'
import { ControlLabel } from '../lib/ui'
import { BottomSheet } from './BottomSheet'

export interface ValueOption {
  value: string
  label: string
  note?: string
}

export function ValueMenu({
  value,
  options,
  onChange,
  disabled,
  'aria-label': ariaLabel,
}: {
  value: string
  options: readonly ValueOption[]
  onChange: (value: string) => void
  disabled?: boolean
  'aria-label'?: string
}) {
  const rowLabel = useContext(ControlLabel)
  const label = ariaLabel ?? rowLabel ?? ''
  const wide = useMediaQuery('(min-width: 40rem)')
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const menu = open && wide
  const root = useDismiss<HTMLDivElement>(close, menu)
  const trigger = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const box = useAnchorBox(trigger, menu)
  useModalFocus(panel, menu)
  const list = useId()
  const current = options.find((o) => o.value === value) ?? options[0]

  const show = () => setOpen(true)
  const pick = (next: string) => {
    close()
    if (next !== current?.value) onChange(next)
  }

  const walk = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const all = [...e.currentTarget.querySelectorAll<HTMLElement>('[role="option"]')]
    const at = all.indexOf(e.currentTarget.ownerDocument.activeElement as HTMLElement)
    const moves: Record<string, number> = { ArrowDown: at + 1, ArrowUp: Math.max(at, 0) - 1, Home: 0, End: -1 }
    const to = moves[e.key]
    if (to === undefined || all.length === 0) return
    e.preventDefault()
    all[(to + all.length) % all.length].focus()
  }

  const items = (sheet: boolean) => (
    <div id={list} role="listbox" aria-label={label} onKeyDown={walk}>
      {options.map((o) => {
        const on = o.value === current?.value
        return (
          <button
            key={o.value}
            type="button"
            role="option"
            aria-selected={on}
            autoFocus={on}
            onClick={() => pick(o.value)}
            className={cx(
              'flex w-full items-center gap-3 rounded-lg px-3 text-left outline-none transition-colors hover:bg-base-700 focus-visible:bg-base-700 focus-visible:ring-2 focus-visible:ring-accent-400 focus-visible:ring-inset',
              sheet ? 'min-h-14 text-[15px]' : 'min-h-10 text-sm pointer-coarse:min-h-11',
              on ? 'text-accent-300' : 'text-base-100',
            )}
          >
            <span className="min-w-0 flex-1 py-2">
              <span className="block">{o.label}</span>
              {o.note && <span className="block text-[13px] text-base-400">{o.note}</span>}
            </span>
            <svg viewBox="0 0 24 24" className={cx('size-4 shrink-0', !on && 'invisible')} aria-hidden>
              <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        )
      })}
    </div>
  )

  const height = 8 + options.length * 44 + options.filter((o) => o.note).length * 20
  const below = !!box && box.bottom + 6 + height <= window.innerHeight - 8

  return (
    <div ref={root} className="relative flex min-w-0 max-w-full justify-end">
      <button
        ref={trigger}
        type="button"
        role="combobox"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? list : undefined}
        aria-label={label}
        disabled={disabled}
        onClick={() => (open ? close() : show())}
        onKeyDown={(e) => {
          if (open || (e.key !== 'ArrowDown' && e.key !== 'ArrowUp')) return
          e.preventDefault()
          show()
        }}
        className="tap press relative flex min-h-10 max-w-full items-center gap-1 rounded-lg py-1 pl-2 text-right text-[15px] text-base-400 transition-colors hover:text-base-100 focus:outline-hidden focus-visible:ring-2 focus-visible:ring-accent-400 disabled:opacity-50 aria-expanded:text-base-100 sm:text-sm"
      >
        <span className="min-w-0 [overflow-wrap:anywhere]">{current?.label}</span>
        <svg viewBox="0 0 24 24" className="size-4 shrink-0 text-base-500" aria-hidden>
          <path d="m9.5 6 6 6-6 6" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      {menu &&
        box &&
        createPortal(
          <div
            ref={panel}
            data-portal-menu
            style={{
              top: below ? box.bottom + 6 : Math.max(8, box.top - 6 - height),
              right: Math.max(8, document.documentElement.clientWidth - box.right),
            }}
            className="fixed z-[60] max-h-[calc(100dvh-1rem)] w-max max-w-[min(22rem,calc(100vw-1rem))] min-w-48 animate-rise overflow-y-auto rounded-card bg-base-750 p-1 shadow-panel scrollbar-thin"
          >
            {items(false)}
          </div>,
          document.body,
        )}
      {open && !wide && (
        <BottomSheet title={label} onClose={close}>
          {items(true)}
        </BottomSheet>
      )}
    </div>
  )
}
