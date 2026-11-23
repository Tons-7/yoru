import { useState } from 'react'
import { createPortal } from 'react-dom'
import { cx } from '../lib/format'
import { useDismiss, useModalFocus } from '../lib/hooks'
import { readKey, writeKey } from '../lib/storage'
import { buttonClass } from '../lib/ui'
import { useUpdateStatus } from '../lib/update'
import { ReleaseNotes } from './ReleaseNotes'

const SEEN = 'yoru.whatsnew.seen'

export function WhatsNew({ hold }: { hold: boolean }) {
  const status = useUpdateStatus().data
  const [seen, setSeen] = useState(() => readKey(SEEN))
  const notes = (status?.whatsNew ?? []).filter((n) => n.notes.trim())
  const current = status?.current ?? ''
  if (hold || !status?.updatedFrom || notes.length === 0 || !current || seen === current) return null
  const close = () => {
    writeKey(SEEN, current)
    setSeen(current)
  }
  return <Dialog version={current} from={status.updatedFrom} notes={notes} onClose={close} />
}

function Dialog({
  version,
  from,
  notes,
  onClose,
}: {
  version: string
  from: string
  notes: { version: string; notes: string; published?: string }[]
  onClose: () => void
}) {
  const panel = useDismiss<HTMLDivElement>(onClose)
  useModalFocus(panel)
  return createPortal(
    <div className="fixed inset-0 z-70 flex items-center justify-center sm:p-6">
      <div className="absolute inset-0 bg-black/75 backdrop-blur-sm" />
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby="whatsnew-title"
        className="animate-rise relative flex h-full w-full flex-col overflow-hidden bg-base-900 shadow-2xl sm:h-auto sm:max-h-[85dvh] sm:max-w-2xl sm:rounded-sheet sm:ring-1 sm:ring-white/10"
      >
        <header className="relative shrink-0 overflow-hidden px-6 pt-[max(2rem,env(safe-area-inset-top))] pb-6 sm:pt-8">
          <div
            aria-hidden
            className="absolute inset-0 bg-[radial-gradient(120%_140%_at_0%_0%,var(--color-accent-500)_0%,transparent_60%)] opacity-35"
          />
          <p className="relative text-xs font-semibold tracking-widest text-accent-200 uppercase">What's new</p>
          <h2 id="whatsnew-title" className="relative mt-1 font-display text-3xl font-bold tracking-tight text-white sm:text-4xl">
            Yoru {version}
          </h2>
          <p className="relative mt-1.5 text-sm text-base-300">Updated from {from}</p>
        </header>
        <div className="min-h-0 flex-1 space-y-7 overflow-y-auto overscroll-contain px-6 pb-6">
          {notes.map((n) => (
            <section key={n.version}>
              {notes.length > 1 && (
                <h3 className="mb-2 flex items-baseline gap-2 border-b border-line pb-2 text-base font-semibold text-white">
                  {n.version}
                  {n.published && <span className="text-xs font-normal text-base-500">{day(n.published)}</span>}
                </h3>
              )}
              <div className="text-[15px] [&_h4]:text-base [&_li]:text-[15px] [&_p]:text-[15px]">
                <ReleaseNotes text={n.notes} />
              </div>
            </section>
          ))}
        </div>
        <footer className="shrink-0 border-t border-line px-6 pt-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
          <button type="button" onClick={onClose} className={cx(buttonClass('primary', 'lg'), 'w-full')}>
            Got it
          </button>
        </footer>
      </div>
    </div>,
    document.body,
  )
}

function day(iso: string) {
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' })
}
