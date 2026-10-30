import { createPortal } from 'react-dom'
import { bytes } from '../lib/format'
import { useDismiss, useModalFocus } from '../lib/hooks'
import { buttonClass } from '../lib/ui'
import { useExitYoru, type ExitStops } from './exit'

export function ExitDialog({ stops, onClose }: { stops: ExitStops; onClose: () => void }) {
  const panel = useDismiss<HTMLDivElement>(onClose)
  useModalFocus(panel)
  const { exit, exiting, failed } = useExitYoru()
  return createPortal(
    <div className="fixed inset-0 z-[60] flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
      <div
        ref={panel}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="exit-title"
        aria-describedby="exit-stops"
        className="animate-fade-in relative w-full max-w-sm rounded-xl border border-base-800 bg-base-900 p-5 shadow-2xl"
      >
        <h2 id="exit-title" className="text-base font-semibold text-white">
          Exit Yoru?
        </h2>
        <div id="exit-stops">
          {stops.unknown && (
            <p className="mt-1 text-sm text-base-400">Yoru couldn't check for downloads or other devices watching.</p>
          )}
          {(stops.downloads > 0 || stops.watching.length > 0) && (
            <p className="mt-1 text-sm text-base-400">These stop until you open Yoru again:</p>
          )}
          <ul className="mt-2 list-disc space-y-1 pl-5 text-sm text-base-200">
            {stops.downloads > 0 && (
              <li>
                {stops.downloads === 1 ? '1 download' : `${stops.downloads} downloads`}
                {stops.left > 0 && ` (${bytes(stops.left)} left)`}
              </li>
            )}
            {stops.watching.map((w, i) => (
              <li key={`${w.device}-${i}`}>
                {w.device} is watching {w.title || 'an episode'}
                {w.episode > 0 && `, episode ${w.episode}`}
              </li>
            ))}
          </ul>
        </div>
        {failed && <p className="mt-3 text-xs text-recap">{failed}</p>}
        <div className="mt-5 flex justify-end gap-2">
          <button type="button" onClick={onClose} className={buttonClass('secondary')}>
            Cancel
          </button>
          <button type="button" onClick={exit} disabled={exiting} className={buttonClass('danger')}>
            {exiting ? 'Exiting…' : 'Exit Yoru'}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}
