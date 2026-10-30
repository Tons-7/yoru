import { useCallback, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { ConfirmInline } from './ui'
import { LIST_STATUSES, type ListStatus } from '../lib/api'
import { useRemoveStatus, useSetStatus } from '../lib/queries'
import { cx } from '../lib/format'
import { useAnchorBox, useDismiss, useModalFocus } from '../lib/hooks'

export function StatusMenu({
  animeId,
  current,
  compact,
  quiet,
  tile,
  onDone,
}: {
  animeId: number
  current?: string | null
  compact?: boolean
  quiet?: boolean
  tile?: boolean
  onDone?: () => void
}) {
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const trigger = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  useModalFocus(menuRef, open)
  const setStatus = useSetStatus({ inline: true })
  const [confirmRewatch, setConfirmRewatch] = useState(false)
  const remove = useRemoveStatus()
  const [confirmRemove, setConfirmRemove] = useState(false)

  const active = LIST_STATUSES.find((s) => s.value === current)

  const box = useAnchorBox(trigger, open)
  const menu = box && {
    top: Math.min(box.bottom + 6, window.innerHeight - 290),
    left: Math.max(8, Math.min(box.right - 176, window.innerWidth - 184)),
  }

  return (
    <div className="relative" ref={ref}>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={active ? `Listed as ${active.label}` : 'Add to list'}
        onClick={(e) => {
          e.preventDefault()
          e.stopPropagation()
          setOpen((v) => !v)
          setConfirmRewatch(false)
          setConfirmRemove(false)
          setStatus.reset()
          remove.reset()
        }}
        className={cx(
          'flex items-center gap-1.5 font-medium transition-colors focus:outline-hidden focus-visible:ring-2 focus-visible:ring-white',
          compact
            ? 'tap relative size-8 justify-center rounded-full'
            : 'press h-10 rounded-xl px-4 text-sm pointer-coarse:min-h-11',
          tile &&
            'justify-center max-sm:h-auto max-sm:min-h-16 max-sm:w-full max-sm:flex-col max-sm:gap-1 max-sm:px-1 max-sm:py-2 max-sm:text-xs [&_svg:first-child]:max-sm:size-5',
          compact
            ? active && !quiet
              ? 'bg-accent-500 text-white shadow-md shadow-black/40 hover:bg-accent-600'
              : 'bg-base-950/60 text-white/90 backdrop-blur-sm hover:bg-base-950/80 hover:text-white'
            : 'bg-base-800 text-base-100 hover:bg-base-750',
        )}
      >
        <BookmarkIcon filled={!!active} />
        {!compact && <span>{active?.label ?? 'Add to list'}</span>}
        {!compact && (
          <svg viewBox="0 0 20 20" className={cx('size-3.5 text-base-400', tile && 'max-sm:hidden')} aria-hidden>
            <path d="M6 8l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        )}
      </button>

      {open && menu && createPortal(
        <div
          ref={menuRef}
          role="menu"
          data-portal-menu
          style={{ top: menu.top, left: menu.left }}
          className="fixed z-50 w-48 origin-top-right animate-rise overflow-hidden rounded-xl bg-base-750 p-1 shadow-panel"
        >
          {LIST_STATUSES.map((status) => (
            <button
              key={status.value}
              role="menuitem"
              disabled={setStatus.isPending}
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                if (status.value === 'REPEATING' && current !== 'REPEATING') {
                  setConfirmRewatch(true)
                  return
                }
                setConfirmRewatch(false)
                setStatus.mutate(
                  { animeId, status: status.value as ListStatus },
                  {
                    onSuccess: () => {
                      setOpen(false)
                      onDone?.()
                    },
                  },
                )
              }}
              className={cx(
                'flex min-h-10 w-full items-center justify-between rounded-lg px-2.5 text-left text-sm transition-colors pointer-coarse:min-h-11',
                status.value === current
                  ? 'text-accent-300'
                  : 'text-base-100 hover:bg-base-700',
              )}
            >
              {status.label}
              {status.value === current && <CheckIcon />}
            </button>
          ))}
          {confirmRewatch && (
            <Confirming note="Progress and ticks reset">
              <ConfirmInline
                label={setStatus.isPending ? 'Starting…' : 'Rewatch'}
                tone="accent"
                size="sm"
                pending={setStatus.isPending}
                onConfirm={() =>
                  setStatus.mutate(
                    { animeId, status: 'REPEATING' },
                    {
                      onSuccess: () => {
                        setConfirmRewatch(false)
                        setOpen(false)
                        onDone?.()
                      },
                    },
                  )
                }
                onCancel={() => setConfirmRewatch(false)}
              />
            </Confirming>
          )}
          {current &&
            (confirmRemove ? (
              <Confirming note="Also removes it from AniList and MyAnimeList">
                <ConfirmInline
                  label={remove.isPending ? 'Removing…' : 'Remove'}
                  size="sm"
                  pending={remove.isPending}
                  onConfirm={() =>
                    remove.mutate(animeId, {
                      onSuccess: () => {
                        setConfirmRemove(false)
                        setOpen(false)
                        onDone?.()
                      },
                    })
                  }
                  onCancel={() => setConfirmRemove(false)}
                />
              </Confirming>
            ) : (
              <button
                role="menuitem"
                onClick={(e) => {
                  e.preventDefault()
                  e.stopPropagation()
                  setConfirmRemove(true)
                }}
                className="mt-1 flex min-h-10 w-full items-center rounded-lg px-2.5 text-left text-sm text-red-300 transition-colors hover:bg-recap/15 pointer-coarse:min-h-11"
              >
                Remove from list
              </button>
            ))}
          {(setStatus.isError || remove.isError) && (
            <p className="mt-1 border-t border-base-700 px-2.5 pt-1.5 pb-1 text-xs text-red-300">
              {((setStatus.error ?? remove.error) as Error).message}
            </p>
          )}

        </div>,
        document.body,
      )}
    </div>
  )
}

function Confirming({ note, children }: { note: string; children: ReactNode }) {
  return (
    <div
      onClick={(e) => e.stopPropagation()}
      className="mt-1 flex flex-wrap items-center gap-2 border-t border-base-700 px-2.5 py-2"
    >
      <span className="basis-full text-xs text-base-400">{note}</span>
      {children}
    </div>
  )
}

function BookmarkIcon({ filled }: { filled: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0" aria-hidden>
      <path
        d="M6 4.5A1.5 1.5 0 0 1 7.5 3h9A1.5 1.5 0 0 1 18 4.5v16l-6-4-6 4v-16Z"
        fill={filled ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function CheckIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path
        d="m5 13 4 4L19 7"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
