import { useEffect, useEffectEvent, useRef } from 'react'
import { createPortal } from 'react-dom'
import { useModalFocus } from '../lib/hooks'

export function TrailerOverlay({ videoId, onClose }: { videoId: string; onClose: () => void }) {
  const close = useEffectEvent(() => onClose())
  const dialog = useRef<HTMLDivElement>(null)
  useModalFocus(dialog)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && close()
    document.addEventListener('keydown', onKey)
    const Watcher = (window as { CloseWatcher?: new () => { onclose: (() => void) | null; destroy: () => void } }).CloseWatcher
    const back = Watcher ? new Watcher() : null
    if (back) back.onclose = () => close()
    return () => {
      back?.destroy()
      document.removeEventListener('keydown', onKey)
    }
  }, [])

  return createPortal(
    <div
      ref={dialog}
      role="dialog"
      aria-modal="true"
      aria-label="Trailer"
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/85 p-4 backdrop-blur-sm"
      onClick={onClose}
    >
      <button
        onClick={onClose}
        aria-label="Close trailer"
        className="fixed top-3 right-3 z-10 grid size-11 place-items-center rounded-full bg-base-900/90 text-lg text-base-100 ring-1 ring-white/15 hover:bg-base-800 hover:text-white"
      >
        ✕
      </button>
      <div
        className="relative w-full max-w-4xl"
        style={{ maxWidth: 'min(56rem, calc((100dvh - 2rem) * 16 / 9))' }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="aspect-video overflow-hidden rounded-xl bg-black shadow-2xl">
          <iframe
            title="Trailer"
            src={`https://www.youtube-nocookie.com/embed/${videoId}?autoplay=1&rel=0`}
            allow="autoplay; encrypted-media; fullscreen; picture-in-picture"
            allowFullScreen
            className="size-full"
          />
        </div>
      </div>
    </div>,
    document.body,
  )
}
