import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { cx } from '../lib/format'
import { useDismiss, useMediaQuery, useModalFocus } from '../lib/hooks'

export function BottomSheet({
  title,
  detail,
  onClose,
  children,
  panel = false,
  side = false,
  tall = false,
  footer,
}: {
  title: ReactNode
  detail?: ReactNode
  onClose: () => void
  children: ReactNode
  panel?: boolean
  side?: boolean
  tall?: boolean
  footer?: ReactNode
}) {
  const sheet = useDismiss<HTMLDivElement>(onClose)
  useModalFocus(sheet)
  const heading = useId()
  const docked = useMediaQuery('(min-width: 40rem)') && side
  const [drop, setDrop] = useState(0)
  const [dragging, setDragging] = useState(false)
  const stopDrag = useRef<(() => void) | null>(null)
  useEffect(() => () => stopDrag.current?.(), [])

  const startDrag = (e: React.PointerEvent) => {
    if (e.button !== 0) return
    const from = e.clientY
    let moved = 0
    const onMove = (ev: PointerEvent) => {
      moved = Math.max(0, ev.clientY - from)
      setDrop(moved)
    }
    const stop = () => {
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onEnd)
      window.removeEventListener('pointercancel', onEnd)
      stopDrag.current = null
      setDragging(false)
    }
    const onEnd = () => {
      stop()
      if (moved > 80) onClose()
      else setDrop(0)
    }
    stopDrag.current = stop
    setDragging(true)
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onEnd)
    window.addEventListener('pointercancel', onEnd)
  }

  const bottom = panel ? 'pb-[max(1rem,env(safe-area-inset-bottom))]' : 'pb-[max(0.75rem,env(safe-area-inset-bottom))]'

  return createPortal(
    <>
      <div
        aria-hidden
        data-portal-menu
        onClick={onClose}
        className="fixed inset-0 z-50 animate-fade-in touch-none bg-black/60"
      />
      <div
        ref={sheet}
        role="dialog"
        aria-modal="true"
        aria-labelledby={heading}
        data-portal-menu
        onKeyDown={(e) => {
          if (e.key !== 'Escape') return
          e.preventDefault()
          e.stopPropagation()
          onClose()
        }}
        style={drop && !docked ? { transform: `translateY(${drop}px)` } : undefined}
        className={cx(
          'fixed inset-x-0 bottom-0 z-50 flex animate-sheet flex-col rounded-t-sheet bg-base-750 shadow-panel',
          tall ? 'h-[90dvh]' : 'max-h-[85dvh]',
          side &&
            'sm:animate-fade-in sm:inset-y-0 sm:right-0 sm:left-auto sm:h-auto sm:max-h-none sm:w-[26rem] sm:rounded-none',
          !dragging && 'transition-transform duration-200',
        )}
      >
        <div
          onPointerDown={docked ? undefined : startDrag}
          className={cx('shrink-0', !docked && 'cursor-grab touch-none')}
        >
          {!docked && <div aria-hidden className="mx-auto mt-2 h-1 w-10 rounded-full bg-base-600" />}
          <div className={cx('pr-16 pb-3', docked ? 'pt-6' : 'pt-3', panel ? 'pl-4 sm:pl-5' : 'pl-5')}>
            <h2
              id={heading}
              className={
                panel ? 'font-display text-xl font-semibold text-white' : 'truncate text-base font-semibold text-base-100'
              }
            >
              {title}
            </h2>
            {detail && <p className={cx('mt-0.5 text-sm text-base-400', !panel && 'truncate')}>{detail}</p>}
          </div>
        </div>
        <div
          className={cx(
            'min-h-0 flex-1 overflow-y-auto overscroll-contain',
            panel ? 'px-4 scrollbar-thin sm:px-5' : 'px-2',
            footer ? 'pb-4' : bottom,
          )}
        >
          {children}
        </div>
        {footer && (
          <div
            className={cx(
              'flex flex-wrap items-center gap-2 bg-base-750 px-4 pt-3 shadow-[0_-1px_0_rgb(255_255_255/0.06)] sm:px-5',
              bottom,
            )}
          >
            {footer}
          </div>
        )}
        <button
          type="button"
          onClick={onClose}
          aria-label="Close"
          className="press absolute top-4 right-3 grid size-11 place-items-center rounded-full bg-base-800 text-base-300 transition-[background-color,color,transform] hover:bg-base-700 hover:text-white"
        >
          <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
            <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />
          </svg>
        </button>
      </div>
    </>,
    document.body,
  )
}
