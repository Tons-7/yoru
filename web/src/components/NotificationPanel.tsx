import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import { cx, whenLabel } from '../lib/format'
import { useNotifications, type Notification } from '../lib/queries'
import { feedTitle, type FeedItem } from '../lib/alerts'
import { ConfirmInline } from './ui'
import { useAnchorBox, useDismiss, useModalFocus } from '../lib/hooks'
import { desktopNotifyWanted, notificationTarget } from '../lib/notifications'

/** Notifications at a glance, handled in place, linking out only when there is more. */
export function NotificationPanel() {
  const [open, setOpen] = useState(false)
  const [confirmClear, setConfirmClear] = useState(false)
  // In a portal at the end of the page; keyboard focus goes in and comes back.
  const panelRef = useRef<HTMLDivElement>(null)
  useModalFocus(panelRef, open)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const trigger = useRef<HTMLButtonElement>(null)

  const qc = useQueryClient()
  const { data } = useNotifications()
  const unread = data?.unread ?? 0
  const items = (data?.items ?? []).slice(0, 8)
  useDesktopNotifications(data?.items)
  // The installed app's icon shows the unread count an alert set, and clears when they're read here.
  useEffect(() => {
    if (!data || !('setAppBadge' in navigator)) return
    void (unread > 0 ? navigator.setAppBadge(unread) : navigator.clearAppBadge()).catch(() => {})
  }, [data, unread])

  const refresh = () => qc.invalidateQueries({ queryKey: ['notifications'] })
  const markRead = useMutation({
    mutationFn: (id?: number) => api.post(`/api/notifications/read${id ? `?id=${id}` : ''}`),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (id?: number) => api.del(`/api/notifications${id ? `?id=${id}` : ''}`),
    onSuccess: refresh,
  })

  const box = useAnchorBox(trigger, open)

  return (
    <div className="relative shrink-0" ref={ref}>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={unread ? `${unread} unread notifications` : 'Notifications'}
        onClick={() => setOpen((v) => !v)}
        className="tap relative grid size-9 place-items-center rounded-md text-base-300 transition-colors hover:bg-base-800 hover:text-white"
      >
        <BellIcon />
        {unread > 0 && (
          <span
            aria-hidden
            className="absolute -top-0.5 -right-0.5 h-4 min-w-4 rounded-full bg-sky-400 px-1 text-center text-[10px] leading-4 font-bold text-base-950 tabular-nums ring-2 ring-base-950"
          >
            {unread > 9 ? '9+' : unread}
          </span>
        )}
      </button>

      {open && box &&
        createPortal(
          <div
            ref={panelRef}
            role="dialog"
            aria-label="Notifications"
            data-portal-menu
            style={{
              top: box.bottom + 8,
              width: Math.min(320, window.innerWidth - 16),
              right: Math.min(
                Math.max(8, window.innerWidth - box.right),
                window.innerWidth - 8 - Math.min(320, window.innerWidth - 16),
              ),
            }}
            className="fixed z-50 animate-rise overflow-hidden rounded-sheet bg-base-750 shadow-panel"
          >
            <div className="flex items-center justify-between gap-2 border-b border-base-750 px-3 py-2">
              <p className="text-sm font-medium text-base-100">
                Notifications
                {unread > 0 && (
                  <span className="ml-1.5 rounded bg-sky-400/20 px-1.5 text-xs text-sky-200">
                    {unread}
                  </span>
                )}
              </p>
              <div className="flex items-center gap-1 pointer-coarse:gap-2">
                {unread > 0 && !confirmClear && (
                  <button
                    onClick={() => markRead.mutate(undefined)}
                    className="rounded px-1.5 py-0.5 text-xs text-base-400 transition-colors hover:bg-base-800 hover:text-white pointer-coarse:min-h-11 pointer-coarse:px-3"
                  >
                    Mark all read
                  </button>
                )}
                {/* Every notification at once: asked, not done on one click. */}
                {items.length > 0 &&
                  (confirmClear ? (
                    <ConfirmInline
                      label="Clear all"
                      onConfirm={() => {
                        remove.mutate(undefined)
                        setConfirmClear(false)
                      }}
                      onCancel={() => setConfirmClear(false)}
                      size="sm"
                    />
                  ) : (
                    <button
                      onClick={() => setConfirmClear(true)}
                      className="rounded px-1.5 py-0.5 text-xs text-base-400 transition-colors hover:bg-base-800 hover:text-recap pointer-coarse:min-h-11 pointer-coarse:px-3"
                    >
                      Clear
                    </button>
                  ))}
              </div>
            </div>

            {items.length === 0 ? (
              <p className="px-3 py-6 text-center text-xs text-base-500">
                Nothing new yet. Tap Alert on a show to hear when new episodes are out.
              </p>
            ) : (
              <ul className="max-h-80 divide-y divide-base-800 overflow-y-auto scrollbar-thin">
                {items.map((item) => (
                  <Row
                    key={item.id}
                    item={item}
                    onOpen={() => {
                      markRead.mutate(item.id)
                      setOpen(false)
                    }}
                    onRemove={() => remove.mutate(item.id)}
                  />
                ))}
              </ul>
            )}

            <Link
              to="/notifications"
              onClick={close}
              className="block border-t border-base-750 px-3 py-2 text-center text-xs text-base-400 transition-colors hover:bg-base-800 hover:text-white pointer-coarse:py-3"
            >
              See all
            </Link>
          </div>,
          document.body,
        )}
    </div>
  )
}

function Row({
  item,
  onOpen,
  onRemove,
}: {
  item: FeedItem
  onOpen: () => void
  onRemove: () => void
}) {
  return (
    <li className="group/row relative">
      <Link
        to={notificationTarget(item)}
        onClick={onOpen}
        className={cx(
          'block px-3 py-2 pr-8 transition-colors hover:bg-base-800',
          !item.read && 'bg-sky-400/[0.05]',
        )}
      >
        <div className="flex items-start gap-2">
          {!item.read && <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-sky-400" />}
          <div className="min-w-0">
            <p className="line-clamp-2 text-xs font-medium text-base-100">{feedTitle(item)}</p>
            <p className="mt-0.5 line-clamp-1 text-xs text-base-500">
              {item.kind === 'release' || !item.body ? whenLabel(item.createdAt) : `${item.body} · ${whenLabel(item.createdAt)}`}
            </p>
          </div>
        </div>
      </Link>

      <button
        onClick={onRemove}
        aria-label="Remove notification"
        className="tap absolute top-2 right-2 rounded p-1 text-base-600 transition-colors hover:text-recap"
      >
        <svg viewBox="0 0 24 24" className="size-3" aria-hidden>
          <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
        </svg>
      </button>
    </li>
  )
}

const SEEN_KEY = 'yoru.notify.seen'

// Raises a system notification per new unread item; the first poll only marks the spot, replaying nothing.
function useDesktopNotifications(items?: Notification[]) {
  useEffect(() => {
    if (!items || typeof Notification === 'undefined') return
    const newest = items.reduce((m, n) => Math.max(m, n.id), 0)
    // No key yet is the first sight; a stored 0 is a list that was empty, so anything newer is news.
    let seen = 0
    let first = true
    try {
      const stored = localStorage.getItem(SEEN_KEY)
      if (stored !== null && Number.isFinite(Number(stored))) {
        seen = Number(stored)
        first = false
      }
    } catch {
      // Unreadable storage: treat as first sight.
    }
    if (!first && desktopNotifyWanted() && Notification.permission === 'granted') {
      for (const n of items.filter((n) => n.id > seen && !n.read).reverse()) {
        // The tag and link a pushed alert for it carries, so one shown both ways collapses into one.
        const target = new URL(notificationTarget(n), window.location.href).href
        const options = { body: n.body, tag: `yoru-${n.id}`, data: { url: target } }
        // Through the alerts worker where there is one: Android has no page notifications ("Illegal
        // constructor"), and that throw would blank the app.
        void (navigator.serviceWorker?.getRegistration() ?? Promise.resolve(undefined))
          .catch(() => undefined)
          .then((reg) => {
            if (reg) return reg.showNotification(n.title, options)
            try {
              const note = new Notification(n.title, options)
              note.onclick = () => {
                window.focus()
                window.location.assign(target)
              }
            } catch {
              // Nowhere to show it.
            }
          })
          .catch(() => {})
      }
    }
    if (newest > seen || first) {
      try {
        localStorage.setItem(SEEN_KEY, String(newest))
      } catch {
        // Nothing to do; the next poll announces again at worst.
      }
    }
  }, [items])
}

function BellIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <path
        d="M6 9a6 6 0 1 1 12 0c0 3.5 1 5 1.5 5.5H4.5C5 14 6 12.5 6 9Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
      <path d="M10 18a2 2 0 0 0 4 0" fill="none" stroke="currentColor" strokeWidth="1.6" />
    </svg>
  )
}
