import { Fragment, useCallback, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { cx } from '../lib/format'
import { menuItems, useAnchorBox, useDismiss, useMediaQuery, useModalFocus } from '../lib/hooks'
import { BottomSheet } from './BottomSheet'

export interface MoreItem {
  label: string
  run?: () => void
  href?: string
  to?: string
  disabled?: boolean
  danger?: boolean
  icon?: ReactNode
  note?: ReactNode
  divider?: boolean
}

const MOVES: Record<string, (at: number) => number> = {
  ArrowDown: (at) => at + 1,
  ArrowUp: (at) => Math.max(at, 0) - 1,
  Home: () => 0,
  End: () => -1,
}

export function MoreMenu({
  label,
  menuLabel,
  title,
  detail,
  items,
  className,
}: {
  label: string
  menuLabel?: string
  title?: string
  detail?: ReactNode
  items: MoreItem[]
  className?: string
}) {
  const wide = useMediaQuery('(min-width: 40rem)')
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const menu = open && wide
  const sheet = open && !wide
  const ref = useDismiss<HTMLDivElement>(close, menu)
  const trigger = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  const box = useAnchorBox(trigger, menu)
  useModalFocus(panel, menu)

  const height =
    8 +
    (title ? 62 : 0) +
    items.length * 44 +
    items.filter((item) => item.note).length * 18 +
    items.filter((item, i) => item.divider && i > 0).length * 9
  const below = !!box && box.bottom + 6 + height <= window.innerHeight - 8

  const onKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      close()
      trigger.current?.focus()
      return
    }
    const move = MOVES[e.key]
    const all = menuItems(e.currentTarget).filter((el) => !(el as HTMLButtonElement).disabled)
    if (!move || all.length === 0) return
    e.preventDefault()
    const at = all.indexOf(e.currentTarget.ownerDocument.activeElement as HTMLElement)
    all[(move(at) + all.length) % all.length].focus()
  }

  const row = (item: MoreItem) =>
    cx(
      'flex w-full items-center gap-3 rounded-lg px-3 text-left outline-none transition-colors focus-visible:ring-2 focus-visible:ring-accent-400 focus-visible:ring-inset',
      sheet ? 'min-h-14 text-[15px]' : 'min-h-10 text-sm pointer-coarse:min-h-11',
      item.disabled
        ? 'cursor-default text-base-500'
        : item.danger
          ? 'text-red-300 hover:bg-red-500/10 focus-visible:bg-red-500/10'
          : 'text-base-100 hover:bg-base-700 focus-visible:bg-base-700',
    )

  const body = (item: MoreItem) => (
    <>
      {item.icon && (
        <span
          aria-hidden
          className={cx(
            'shrink-0 [&>svg]:size-full',
            sheet ? 'size-6' : 'size-5',
            item.disabled ? 'text-base-600' : !item.danger && 'text-base-400',
          )}
        >
          {item.icon}
        </span>
      )}
      <span className="min-w-0 flex-1 py-2">
        <span className="block">{item.label}</span>
        {item.note && <span className="block text-[13px] text-base-400">{item.note}</span>}
      </span>
    </>
  )

  const list = (
    <div role="menu" aria-label={menuLabel ?? label} onKeyDown={onKeyDown}>
      {items.map((item, i) => (
        <Fragment key={item.label}>
          {item.divider && i > 0 && <div role="separator" className="mx-2 my-1 h-px bg-base-700" />}
          {item.href && !item.disabled ? (
            <a role="menuitem" href={item.href} target="_blank" rel="noreferrer" onClick={close} className={row(item)}>
              {body(item)}
            </a>
          ) : item.to && !item.disabled ? (
            <Link role="menuitem" to={item.to} onClick={close} className={row(item)}>
              {body(item)}
            </Link>
          ) : (
            <button
              type="button"
              role="menuitem"
              disabled={item.disabled}
              onClick={() => {
                close()
                item.run?.()
              }}
              className={row(item)}
            >
              {body(item)}
            </button>
          )}
        </Fragment>
      ))}
    </div>
  )

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
          'tap press relative grid place-items-center text-base-400 transition-[background-color,color,transform] hover:bg-base-800 hover:text-white aria-expanded:bg-base-800 aria-expanded:text-white',
          className ?? 'size-8 rounded-full',
        )}
      >
        <MoreGlyph />
      </button>
      {menu &&
        box &&
        createPortal(
          <div
            ref={panel}
            data-portal-menu
            style={{
              top: below ? box.bottom + 6 : Math.max(8, box.top - 6 - height),
              left: Math.max(8, Math.min(box.right - 240, window.innerWidth - 248)),
            }}
            className="fixed z-[60] max-h-[calc(100dvh-1rem)] w-60 animate-rise overflow-y-auto rounded-card bg-base-750 p-1 shadow-panel"
          >
            {title && (
              <>
                <div className="px-3 pt-2 pb-2">
                  <p className="truncate text-sm font-semibold text-base-100">{title}</p>
                  {detail && <p className="truncate text-[13px] text-base-400">{detail}</p>}
                </div>
                <div aria-hidden className="mx-2 mb-1 h-px bg-base-700" />
              </>
            )}
            {list}
          </div>,
          document.body,
        )}
      {sheet && (
        <BottomSheet title={title ?? menuLabel ?? label} detail={detail} onClose={close}>
          {list}
        </BottomSheet>
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
