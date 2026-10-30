import { useState } from 'react'
import { Link } from 'react-router-dom'
import { bytes, cx } from '../lib/format'
import { cellular } from '../player/network'

const button = 'rounded-md px-3 py-1.5 text-sm pointer-coarse:min-h-11'
const primary = cx(button, 'bg-white font-semibold text-base-950 hover:bg-base-100')
const quiet = cx(button, 'text-base-300 hover:bg-base-800 hover:text-white')

// Where the browser can't tell mobile data from Wi-Fi, the rule applies whenever away from home: said so.
const where = () =>
  cellular() === null
    ? { title: "You're away from home", note: "This device doesn't say whether it's on mobile data." }
    : { title: "You're on mobile data", note: '' }

/** Before streaming on mobile data: the episode's size and the choice, or Wi-Fi only's stop and way past. */
export function MobileDataCard({
  card,
  original,
  saver,
  onPlay,
  onCancel,
}: {
  card: 'ask' | 'wifi'
  /** The file's bytes, what playing it as it is costs; unknown for some sources. */
  original?: number
  saver: number
  onPlay: (how: 'full' | 'saver', remember: boolean) => void
  onCancel: () => void
}) {
  const [remember, setRemember] = useState(false)
  const { title, note } = where()
  return (
    <div className="absolute inset-0 z-20 grid place-items-center bg-black/70 p-4">
      <div role="dialog" aria-label={title} className="w-80 max-w-full animate-rise rounded-sheet bg-base-950/95 p-4 shadow-xl shadow-black/60 ring-1 ring-white/10">
        <p className="text-sm font-semibold text-base-100">{title}</p>
        {card === 'ask' ? (
          <>
            <p className="mt-1 text-xs text-base-400">
              {original ? `This episode is about ${bytes(original)}. ` : ''}Data saver plays it at 480p, about {bytes(saver)}.
              {note && ` ${note}`}
            </p>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <button onClick={() => onPlay('full', remember)} className={primary}>
                Play
              </button>
              <button onClick={() => onPlay('saver', remember)} className={cx(button, 'bg-base-800 text-white hover:bg-base-700')}>
                Play with data saver
              </button>
              <button onClick={onCancel} className={quiet}>
                Cancel
              </button>
            </div>
            <label className="mt-3 flex items-center gap-2 text-xs text-base-300 pointer-coarse:min-h-11">
              <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} className="size-4 accent-accent-500" />
              Remember on this device
            </label>
          </>
        ) : (
          <>
            <p className="mt-1 text-xs text-base-400">
              This device streams on Wi-Fi only. Change it under <Link to="/settings?tab=Devices" className="underline">Settings, Devices</Link>.
              {note && ` ${note}`}
            </p>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <button onClick={() => onPlay('saver', false)} className={primary}>
                Play once with data saver
              </button>
              <button onClick={() => onPlay('full', false)} className={quiet}>
                Play once anyway
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

/** A note over the playing picture, with one action and a close: mid-episode, nothing stops on its own. */
export function NetworkNote({ text, action, onAction, onClose }: { text: string; action: string; onAction: () => void; onClose: () => void }) {
  return (
    <div role="status" className="absolute top-4 left-1/2 z-20 flex w-max max-w-[calc(100%-2rem)] -translate-x-1/2 animate-rise flex-wrap items-center gap-2 rounded-xl bg-base-950/90 py-1.5 ring-1 ring-white/10 pr-1.5 pl-3 text-sm text-base-100 shadow-xl shadow-black/60 backdrop-blur-sm">
      <span>{text}</span>
      <button onClick={onAction} className={cx(button, 'bg-white font-semibold text-base-950 hover:bg-base-100')}>
        {action}
      </button>
      <button onClick={onClose} aria-label="Close" className={quiet}>
        ✕
      </button>
    </div>
  )
}
