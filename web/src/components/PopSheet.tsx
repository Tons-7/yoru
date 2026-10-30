import { useRef, type ReactNode, type RefObject } from 'react'
import { createPortal } from 'react-dom'
import { cx } from '../lib/format'
import { useAnchorBox, useDismiss, useMediaQuery, useModalFocus } from '../lib/hooks'
import { BottomSheet } from './BottomSheet'

export function PopSheet({
  open,
  onClose,
  title,
  anchor,
  width = 288,
  children,
}: {
  open: boolean
  onClose: () => void
  title: string
  anchor: RefObject<HTMLElement | null>
  width?: number
  children: ReactNode
}) {
  const wide = useMediaQuery('(min-width: 40rem)')
  const root = useDismiss<HTMLDivElement>(onClose, open && wide)
  const panel = useRef<HTMLDivElement>(null)
  useModalFocus(panel, open && wide)
  const box = useAnchorBox(anchor, open && wide)

  if (!wide) {
    return open ? (
      <BottomSheet panel title={title} onClose={onClose}>
        {children}
      </BottomSheet>
    ) : null
  }
  if (!open || !box) return null

  const below = window.innerHeight - box.bottom - 16
  const above = box.top - 80
  const down = below >= 320 || below >= above
  return createPortal(
    <div ref={root} className="fixed inset-0 z-50">
      <div aria-hidden className="absolute inset-0" onClick={onClose} />
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        style={{
          left: Math.max(8, Math.min(box.left, window.innerWidth - width - 8)),
          width,
          maxHeight: down ? below : above,
          ...(down ? { top: box.bottom + 8 } : { bottom: window.innerHeight - box.top + 8 }),
        }}
        className={cx(
          'absolute animate-rise overflow-y-auto rounded-card bg-base-750 p-1.5 shadow-panel scrollbar-thin',
          down ? 'origin-top-left' : 'origin-bottom-left',
        )}
      >
        {children}
      </div>
    </div>,
    document.body,
  )
}
