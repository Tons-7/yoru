import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { cx } from '../lib/format'
import { onToast, TOAST_MS, type Toast } from '../lib/toast'
import { usePortalHome } from './keys'

/** Shows an action's failure when its own screen has no place for it. */
export function Toaster() {
  const [toasts, setToasts] = useState<Toast[]>([])
  // Inside the fullscreen player when there is one: outside it nothing shows.
  const home = usePortalHome()

  useEffect(
    () =>
      onToast((t) => {
        // The same failure twice in a row is one message; one with an action is its own.
        setToasts((prev) =>
          !t.action && prev.some((p) => p.message === t.message && !p.action) ? prev : [...prev.slice(-2), t],
        )
        window.setTimeout(() => setToasts((prev) => prev.filter((p) => p.id !== t.id)), TOAST_MS)
      }),
    [],
  )

  return createPortal(
    <div
      aria-live="polite"
      // Bottom-left from sm up: the device access requests hold the bottom-right corner.
      className="pointer-events-none fixed right-4 bottom-4 z-[90] flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2 max-sm:bottom-[calc(4.5rem+env(safe-area-inset-bottom))] sm:right-auto sm:left-4"
    >
      {toasts.map((t) => (
        <div
          key={t.id}
          role={t.tone === 'error' ? 'alert' : 'status'}
          className={cx(
            'pointer-events-auto flex animate-rise items-start gap-3 rounded-xl bg-base-850/95 px-4 py-3 text-sm shadow-lift ring-1 backdrop-blur-xl',
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
                setToasts((prev) => prev.filter((p) => p.id !== t.id))
              }}
              className="-my-0.5 rounded-md px-2 py-0.5 font-medium text-accent-300 hover:bg-white/10"
            >
              {t.action.label}
            </button>
          )}
          <button
            onClick={() => setToasts((prev) => prev.filter((p) => p.id !== t.id))}
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
