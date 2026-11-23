import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { cx } from '../lib/format'
import { ACTION_TOAST_MS, onDismiss, onToast, TOAST_MS, type Toast } from '../lib/toast'
import { usePortalHome } from './keys'

const LEAVE_MS = 160

export function Toaster() {
  const [toasts, setToasts] = useState<Toast[]>([])
  const [leaving, setLeaving] = useState<ReadonlySet<number>>(new Set())
  const home = usePortalHome()
  const timers = useRef(new Map<number, number>())
  const schedule = (t: Toast) => {
    window.clearTimeout(timers.current.get(t.id))
    timers.current.set(t.id, window.setTimeout(() => dismiss(t.id), t.action ? ACTION_TOAST_MS : TOAST_MS))
  }
  const hold = (id: number) => window.clearTimeout(timers.current.get(id))

  const dismiss = (id: number) => {
    window.clearTimeout(timers.current.get(id))
    timers.current.delete(id)
    setLeaving((prev) => new Set(prev).add(id))
    window.setTimeout(() => {
      setToasts((prev) => prev.filter((p) => p.id !== id))
      setLeaving((prev) => {
        const next = new Set(prev)
        next.delete(id)
        return next
      })
    }, LEAVE_MS)
  }

  useEffect(
    () =>
      onToast((t) => {
        setToasts((prev) => {
          const kept = t.action ? prev : prev.filter((p) => !(p.message === t.message && !p.action))
          const next = [...kept, t]
          while (next.length > 3) {
            const plain = next.findIndex((p, i) => !p.action && i < next.length - 1)
            if (plain < 0) break
            next.splice(plain, 1)
          }
          return next
        })
        schedule(t)
      }),
    [],
  )
  useEffect(() => onDismiss(dismiss), [])

  return createPortal(
    <div
      aria-live="polite"
      className="pointer-events-none fixed right-4 bottom-4 z-90 flex w-[calc(100vw-2rem)] flex-col gap-2 max-sm:bottom-[calc(4.5rem+env(safe-area-inset-bottom))] sm:right-auto sm:left-4 sm:w-auto sm:max-w-[min(32rem,calc(100vw-2rem))] sm:items-start"
    >
      {toasts.map((t) => (
        <div
          key={t.id}
          onPointerEnter={() => hold(t.id)}
          onPointerLeave={() => !leaving.has(t.id) && schedule(t)}
          onFocus={() => hold(t.id)}
          onBlur={() => !leaving.has(t.id) && schedule(t)}
          className={cx(
            'pointer-events-auto flex items-start gap-3 rounded-xl bg-base-850/95 px-4 py-3 text-sm shadow-lift ring-1 backdrop-blur-xl',
            leaving.has(t.id) ? 'pointer-events-none animate-leave' : 'animate-rise',
            t.tone === 'error' ? 'ring-recap/40' : 'ring-white/10',
          )}
        >
          <span
            className={cx('mt-1.5 size-2 shrink-0 rounded-full', t.tone === 'error' ? 'bg-recap' : 'bg-accent-400')}
          />
          <p className="min-w-0 flex-1 text-base-100 sm:flex-initial">{t.message}</p>
          {t.action && (
            <button
              onClick={() => {
                t.action!.run()
                dismiss(t.id)
              }}
              className="-my-0.5 rounded-md px-3 py-0.5 font-medium text-accent-300 hover:bg-white/10 pointer-coarse:-my-2.5 pointer-coarse:min-h-11"
            >
              {t.action.label}
            </button>
          )}
          <button
            onClick={() => dismiss(t.id)}
            aria-label="Dismiss"
            className="-mr-1 grid size-6 shrink-0 place-items-center rounded text-base-500 hover:text-white pointer-coarse:-my-2.5 pointer-coarse:-mr-2.5 pointer-coarse:size-11"
          >
            <svg viewBox="0 0 16 16" className="size-3.5" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
              <path d="M4 4l8 8M12 4l-8 8" />
            </svg>
          </button>
        </div>
      ))}
    </div>,
    home,
  )
}
