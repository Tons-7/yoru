import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { cx } from '../lib/format'
import { onDismiss, onToast, TOAST_MS, type Toast } from '../lib/toast'
import { usePortalHome } from './keys'

const LEAVE_MS = 160

export function Toaster() {
  const [toasts, setToasts] = useState<Toast[]>([])
  const [leaving, setLeaving] = useState<ReadonlySet<number>>(new Set())
  const home = usePortalHome()

  const dismiss = (id: number) => {
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
          if (!t.action && prev.some((p) => p.message === t.message && !p.action)) return prev
          const next = [...prev, t]
          while (next.length > 3) {
            const plain = next.findIndex((p, i) => !p.action && i < next.length - 1)
            if (plain < 0) break
            next.splice(plain, 1)
          }
          return next
        })
        window.setTimeout(() => dismiss(t.id), TOAST_MS)
      }),
    [],
  )
  useEffect(() => onDismiss(dismiss), [])

  return createPortal(
    <div
      aria-live="polite"
      className="pointer-events-none fixed right-4 bottom-4 z-[90] flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2 max-sm:bottom-[calc(4.5rem+env(safe-area-inset-bottom))] sm:right-auto sm:left-4"
    >
      {toasts.map((t) => (
        <div
          key={t.id}
          role={t.tone === 'error' ? 'alert' : 'status'}
          className={cx(
            'pointer-events-auto flex items-start gap-3 rounded-xl bg-base-850/95 px-4 py-3 text-sm shadow-lift ring-1 backdrop-blur-xl',
            leaving.has(t.id) ? 'pointer-events-none animate-leave' : 'animate-rise',
            t.tone === 'error' ? 'ring-recap/40' : 'ring-white/10',
          )}
        >
          <span
            className={cx('mt-1.5 size-2 shrink-0 rounded-full', t.tone === 'error' ? 'bg-recap' : 'bg-accent-400')}
          />
          <p className="min-w-0 flex-1 text-base-100">{t.message}</p>
          {t.action && (
            <button
              onClick={() => {
                t.action!.run()
                dismiss(t.id)
              }}
              className="-my-0.5 rounded-md px-2 py-0.5 font-medium text-accent-300 hover:bg-white/10"
            >
              {t.action.label}
            </button>
          )}
          <button
            onClick={() => dismiss(t.id)}
            aria-label="Dismiss"
            className="-mr-1 rounded px-1 text-base-500 hover:text-white"
          >
            ✕
          </button>
        </div>
      ))}
    </div>,
    home,
  )
}
