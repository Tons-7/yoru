import { useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { bytes, cx } from '../lib/format'
import { cellular } from '../player/network'
import { overlayButton as button } from '../lib/ui'
import { useModalFocus } from '../lib/hooks'
import { Switch } from './ui'

const primary = cx(button, 'bg-white font-semibold text-base-950 hover:bg-base-100')
const quiet = cx(button, 'text-base-300 hover:bg-base-800 hover:text-white')

const where = () =>
  cellular() === null
    ? { title: "You're away from home", note: "This device doesn't say whether it's on mobile data." }
    : { title: "You're on mobile data", note: '' }

export function MobileDataCard({
  card,
  original,
  saver,
  onPlay,
  onCancel,
}: {
  card: 'ask' | 'wifi'
  original?: number
  saver: number
  onPlay: (how: 'full' | 'saver', remember: boolean) => void
  onCancel: () => void
}) {
  const [remember, setRemember] = useState(false)
  const { title, note } = where()
  const sheet = useRef<HTMLDivElement>(null)
  useModalFocus(sheet)
  return (
    <div className="absolute inset-0 z-20 grid place-items-center bg-black/70 p-4">
      <div
        ref={sheet}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        onKeyDown={(e) => {
          if (e.key === 'Escape') onCancel()
        }}
        className="w-80 max-w-full animate-rise rounded-sheet bg-base-950/95 p-4 shadow-xl shadow-black/60 ring-1 ring-white/10">
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
            <div className="mt-3 flex min-h-11 items-center justify-between gap-2 text-xs text-base-300">
              Remember on this device
              <Switch on={remember} onChange={setRemember} aria-label="Remember on this device" />
            </div>
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
              <button onClick={onCancel} className={quiet}>
                Not now
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}

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
