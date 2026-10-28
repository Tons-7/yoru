import { useCallback, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { ConfirmInline } from './ui'
import { LIST_STATUSES, type ListStatus } from '../lib/api'
import { useRemoveStatus, useSetStatus } from '../lib/queries'
import { cx } from '../lib/format'
import { useAnchorBox, useDismiss, useModalFocus } from '../lib/hooks'

/** The bookmark control is the list status (watching, completed, …), not a separate flag. */
export function StatusMenu({
  animeId,
  current,
  compact,
  onDone,
}: {
  animeId: number
  current?: string | null
  compact?: boolean
  onDone?: () => void
}) {
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const trigger = useRef<HTMLButtonElement>(null)
  // The portalled menu ends the tab order, so opening moves focus in and closing returns it.
  const menuRef = useRef<HTMLDivElement>(null)
  useModalFocus(menuRef, open)
  const setStatus = useSetStatus({ inline: true })
  // Rewatching starts the show over (progress and ticks reset), so it asks once.
  const [confirmRewatch, setConfirmRewatch] = useState(false)
  // Removing also deletes the entry on AniList and MAL, so it asks once too.
  const remove = useRemoveStatus()
  const [confirmRemove, setConfirmRemove] = useState(false)

  const active = LIST_STATUSES.find((s) => s.value === current)

  // A portal, as the cards and banner it sits in clip their contents.
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
          'flex items-center gap-1.5 font-medium transition-colors focus:outline-hidden focus-visible:ring-2 focus-visible:ring-accent-300',
          compact ? 'tap relative size-8 justify-center rounded-md' : 'rounded-lg px-4 py-2 text-sm pointer-coarse:min-h-11',
          // Over art, solid violet or a dark frosted chip: a faint tint vanished into a busy cover.
          compact
            ? active
              ? 'bg-accent-500 text-white shadow-md shadow-black/40 ring-1 ring-white/20 hover:bg-accent-400'
              : 'bg-base-950/75 text-white ring-1 ring-white/15 backdrop-blur-sm hover:bg-base-800'
            : // Beside Play, which is the solid one.
              active
              ? 'bg-accent-500/25 text-accent-300 ring-1 ring-accent-400/60 hover:bg-accent-500/35'
              : 'bg-base-800/90 text-base-100 ring-1 ring-white/10 hover:bg-base-700',
        )}
      >
        <BookmarkIcon filled={!!active} />
        {!compact && <span>{active?.label ?? 'Add to list'}</span>}
      </button>

      {open && menu && createPortal(
        <div
          ref={menuRef}
          role="menu"
          data-portal-menu
          style={{ top: menu.top, left: menu.left }}
          className="fixed z-50 w-44 origin-top-right animate-rise overflow-hidden rounded-lg border border-base-700 bg-base-850 py-1 shadow-xl shadow-black/50"
        >
          {LIST_STATUSES.map((status) => (
            <button
              key={status.value}
              role="menuitem"
              disabled={setStatus.isPending}
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                // Rewatching resets progress, so it only ever goes through the confirm below.
                if (status.value === 'REPEATING' && current !== 'REPEATING') {
                  setConfirmRewatch(true)
                  return
                }
                setConfirmRewatch(false)
                // A failure keeps the menu open with the reason, not a silent no-op.
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
                'flex w-full items-center justify-between px-3 py-1.5 text-left text-sm transition-colors',
                status.value === current
                  ? 'text-accent-400'
                  : 'text-base-200 hover:bg-base-750',
              )}
            >
              {status.label}
              {status.value === current && <CheckIcon />}
            </button>
          ))}
          {/* A Cancel that takes focus: relabelled in place, a double tap confirmed what the first armed. */}
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
                className="flex w-full items-center border-t border-base-750 px-3 py-1.5 text-left text-sm text-red-300 transition-colors hover:bg-recap/15"
              >
                Remove from list
              </button>
            ))}
          {(setStatus.isError || remove.isError) && (
            <p className="border-t border-base-750 px-3 py-1.5 text-xs text-recap">
              {((setStatus.error ?? remove.error) as Error).message}
            </p>
          )}

        </div>,
        document.body,
      )}
    </div>
  )
}

// A confirm step inside the menu; its clicks stay here, not with a card the menu belongs to.
function Confirming({ note, children }: { note: string; children: ReactNode }) {
  return (
    <div
      onClick={(e) => e.stopPropagation()}
      className="flex flex-wrap items-center gap-2 border-t border-base-750 px-3 py-2"
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
