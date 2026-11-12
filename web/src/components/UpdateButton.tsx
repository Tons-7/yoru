import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { cx } from '../lib/format'
import { useDismiss, useModalFocus } from '../lib/hooks'
import { useAccess } from '../lib/queries'
import { buttonClass } from '../lib/ui'
import { updatePercent, useRestartToUpdate, useUpdateActions, useUpdateOffered, useUpdateStatus } from '../lib/update'

const R = 10
const CIRCLE = 2 * Math.PI * R
const ABOUT = '/settings?tab=About'
const GIVE_UP_S = 90

const pill =
  'press flex h-9 shrink-0 items-center gap-2 rounded-full px-2 text-xs font-semibold whitespace-nowrap transition-colors focus:outline-hidden focus-visible:ring-2 focus-visible:ring-white pointer-coarse:h-11 lg:pr-3'

function Arrow({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden className={cx('size-3.5', className)}>
      <path d="M12 5v13m-5.5-5.5L12 18l5.5-5.5" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function Ring({ share }: { share: number | null }) {
  return (
    <span className="relative grid size-6 place-items-center overflow-hidden rounded-full">
      <svg viewBox="0 0 24 24" aria-hidden className={cx('absolute inset-0 size-6 -rotate-90', share == null && 'animate-spin motion-reduce:animate-none')}>
        <circle cx="12" cy="12" r={R} fill="none" strokeWidth="2" className="stroke-accent-400/25" />
        <circle
          cx="12"
          cy="12"
          r={R}
          fill="none"
          strokeWidth="2"
          strokeLinecap="round"
          strokeDasharray={CIRCLE}
          strokeDashoffset={CIRCLE * (1 - Math.min(1, Math.max(0.06, share ?? 0.25)))}
          className="stroke-accent-400 transition-[stroke-dashoffset] duration-500"
        />
      </svg>
      <Arrow className="size-3 animate-update-arrow motion-reduce:animate-none" />
    </span>
  )
}

export function UpdateButton({ compact = false }: { compact?: boolean }) {
  if (!useUpdateOffered()) return null
  return (
    <span className="-m-1.5 flex shrink-0 animate-update-in overflow-hidden p-1.5 motion-reduce:animate-none">
      <UpdatePill compact={compact} />
    </span>
  )
}

function UpdatePill({ compact }: { compact: boolean }) {
  const host = useAccess().data?.host === true
  const status = useUpdateStatus(host)
  const { download } = useUpdateActions()
  const { choose, checking, ask, close, restart } = useRestartToUpdate()
  const s = status.data
  if (!host || !s || !s.available || !s.latest) return null

  const version = s.staged || s.latest.version
  const label = (text: string) => <span className={compact ? 'sr-only' : 'max-lg:sr-only'}>{text}</span>

  if (s.stage === 'downloading' || s.stage === 'verifying') {
    const percent = updatePercent(s)
    const known = s.stage === 'downloading' && s.total > 0
    return (
      <Link
        to={ABOUT}
        data-update="downloading"
        aria-label={known ? `Downloading Yoru ${version}, ${percent}%` : `Downloading Yoru ${version}`}
        className={cx(pill, 'bg-accent-500/15 text-accent-300 hover:bg-accent-500/25')}
      >
        <Ring share={known ? percent / 100 : null} />
        <span aria-hidden className={cx('tabular-nums', compact ? 'hidden' : 'max-lg:hidden')}>
          {known ? `Downloading update ${percent}%` : 'Downloading update'}
        </span>
      </Link>
    )
  }

  if (s.stage === 'ready' || s.stage === 'restarting') {
    return (
      <>
        <button
          type="button"
          data-update="ready"
          onClick={() => void choose()}
          disabled={checking || restart.isPending || s.stage === 'restarting'}
          aria-label={`Restart Yoru to update to ${version}`}
          className={cx(pill, 'animate-update-ready bg-emerald-500/15 text-emerald-300 hover:bg-emerald-500/25 motion-reduce:animate-none')}
        >
          <span className="grid size-6 place-items-center rounded-full bg-emerald-400 text-base-950">
            <Arrow />
          </span>
          {label('Restart to update')}
        </button>
        {ask && <UpdateRestartDialog busy={ask} version={version} onClose={close} />}
      </>
    )
  }

  if (s.stage === 'failed') {
    return (
      <Link to={ABOUT} data-update="failed" aria-label="The update failed. Open Settings" className={cx(pill, 'bg-amber-400/10 text-amber-200 hover:bg-amber-400/20')}>
        <span className="grid size-6 place-items-center rounded-full bg-amber-400/20">
          <Arrow />
        </span>
        {label('Update failed')}
      </Link>
    )
  }

  return (
    <button
      type="button"
      data-update="available"
      onClick={() => download.mutate()}
      disabled={download.isPending}
      aria-label={`Download Yoru ${version}`}
      className={cx(pill, 'bg-accent-500/15 text-accent-300 hover:bg-accent-500/25')}
    >
      <span className="grid size-6 place-items-center rounded-full bg-accent-500 text-white">
        <Arrow />
      </span>
      {label('Update')}
    </button>
  )
}

export function UpdateRestartDialog({ busy, version, onClose }: { busy: string[]; version: string; onClose: () => void }) {
  const panel = useDismiss<HTMLDivElement>(onClose)
  useModalFocus(panel)
  const { restart } = useUpdateActions()
  return createPortal(
    <div className="fixed inset-0 z-[60] flex items-center justify-center p-4">
      <div className="absolute inset-0 animate-fade-in bg-black/70 backdrop-blur-sm" />
      <div
        ref={panel}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="update-title"
        aria-describedby="update-stops"
        className="relative w-full max-w-md animate-rise rounded-2xl border border-base-800 bg-base-900 p-7 shadow-2xl"
      >
        <h2 id="update-title" className="font-display text-2xl font-bold tracking-tight text-white">
          Restart Yoru to update?
        </h2>
        <div id="update-stops">
          <p className="mt-2 text-base text-base-300">Yoru {version} is ready and is back in a few seconds. Until then:</p>
          <ul className="mt-3 list-disc space-y-1.5 pl-5 text-base text-base-100">
            {busy.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
        </div>
        {restart.isError && <p className="mt-3 text-xs text-recap">{(restart.error as Error).message}</p>}
        <div className="mt-7 flex justify-end gap-3">
          <button type="button" onClick={onClose} className={cx(buttonClass('secondary'), 'h-11 px-5 text-[15px]')}>
            Not now
          </button>
          <button
            type="button"
            onClick={() => restart.mutate(undefined, { onSuccess: onClose })}
            disabled={restart.isPending}
            className={cx(buttonClass('primary'), 'h-11 px-5 text-[15px]')}
          >
            {restart.isPending ? 'Restarting…' : 'Restart now'}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  )
}

export function UpdateRestarting() {
  const host = useAccess().data?.host === true
  const s = useUpdateStatus(host).data
  const restarting = host && s?.stage === 'restarting'
  const current = s?.current
  const loaded = useRef('')
  const [stuck, setStuck] = useState(false)
  if (stuck && !restarting) setStuck(false)

  useEffect(() => {
    if (!current) return
    if (!loaded.current) loaded.current = current
    else if (current !== loaded.current) window.location.reload()
  }, [current])

  useEffect(() => {
    if (!restarting) return
    const timer = window.setTimeout(() => setStuck(true), GIVE_UP_S * 1000)
    return () => window.clearTimeout(timer)
  }, [restarting])

  if (!restarting) return null
  return createPortal(
    <div role="alertdialog" aria-modal="true" aria-labelledby="updating-title" className="fixed inset-0 z-[70] flex animate-fade-in flex-col items-center justify-center gap-4 bg-base-950/90 p-6 text-center backdrop-blur-sm">
      {!stuck && <span aria-hidden className="size-9 animate-spin rounded-full border-[3px] border-accent-400/25 border-t-accent-400 motion-reduce:animate-none" />}
      <h2 id="updating-title" className="font-display text-2xl font-bold text-white">
        {stuck ? "Yoru hasn't come back" : 'Updating Yoru'}
      </h2>
      <p className="max-w-sm text-sm text-base-300">
        {stuck ? 'Start Yoru again. It falls back to the previous version if the new one fails.' : 'Back in a few seconds.'}
      </p>
      {stuck && (
        <button type="button" onClick={() => window.location.reload()} className={buttonClass('primary')}>
          Reload
        </button>
      )}
    </div>,
    document.body,
  )
}
