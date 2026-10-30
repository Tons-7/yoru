import { useCallback, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { cx } from '../lib/format'
import { useAnchorBox, useDismiss, useModalFocus } from '../lib/hooks'

export interface MoreItem {
  label: string
  run?: () => void
  href?: string
  disabled?: boolean
  danger?: boolean
}

export function MoreMenu({
  label,
  menuLabel,
  items,
  className,
}: {
  label: string
  menuLabel?: string
  items: MoreItem[]
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const trigger = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const box = useAnchorBox(trigger, open)
  useModalFocus(menu, open)

  const height = items.length * 44 + 10
  const below = !!box && box.bottom + 6 + height <= window.innerHeight - 8
  const row =
    'flex min-h-10 w-full items-center rounded-lg px-2.5 text-left text-sm transition-colors disabled:opacity-40 pointer-coarse:min-h-11'
  const tone = (danger?: boolean) => (danger ? 'text-red-300 hover:bg-red-500/10' : 'text-base-200 hover:bg-base-800')

  return (
    <div ref={ref} className="relative shrink-0">
      <button
        ref={trigger}
        type="button"
        data-focus="menu"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        onClick={() => setOpen((v) => !v)}
        className={cx(
          'tap relative grid place-items-center text-base-400 transition-colors hover:bg-base-800 hover:text-white aria-expanded:bg-base-800 aria-expanded:text-white',
          className ?? 'size-8 rounded-full',
        )}
      >
        <MoreGlyph />
      </button>
      {open &&
        box &&
        createPortal(
          <div
            ref={menu}
            role="menu"
            data-portal-menu
            aria-label={menuLabel ?? label}
            onKeyDown={(e) => {
              if (e.key !== 'Escape') return
              e.preventDefault()
              e.stopPropagation()
              setOpen(false)
              trigger.current?.focus()
            }}
            style={{
              top: below ? box.bottom + 6 : Math.max(8, box.top - 6 - height),
              left: Math.max(8, Math.min(box.right - 208, window.innerWidth - 216)),
            }}
            className="fixed z-[60] w-52 animate-rise rounded-xl border border-base-750 bg-base-850 p-1 shadow-panel"
          >
            {items.map((item) =>
              item.href ? (
                <a
                  key={item.label}
                  role="menuitem"
                  href={item.href}
                  target="_blank"
                  rel="noreferrer"
                  onClick={close}
                  className={cx(row, tone(item.danger))}
                >
                  {item.label}
                </a>
              ) : (
                <button
                  key={item.label}
                  type="button"
                  role="menuitem"
                  disabled={item.disabled}
                  onClick={() => {
                    setOpen(false)
                    item.run?.()
                  }}
                  className={cx(row, tone(item.danger))}
                >
                  {item.label}
                </button>
              ),
            )}
          </div>,
          document.body,
        )}
    </div>
  )
}

function MoreGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" fill="currentColor" aria-hidden>
      <circle cx="5" cy="12" r="1.8" />
      <circle cx="12" cy="12" r="1.8" />
      <circle cx="19" cy="12" r="1.8" />
    </svg>
  )
}
