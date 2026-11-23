import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import { cx, whenLabel } from '../lib/format'
import { notificationBody, useNotifications, type Notification } from '../lib/queries'
import { feedTitle, type FeedItem } from '../lib/alerts'
import { Badge, Button, ConfirmInline } from './ui'
import { useAnchorBox, useDismiss, useModalFocus } from '../lib/hooks'
import { desktopNotifyWanted, notificationTarget } from '../lib/notifications'

export function NotificationPanel() {
  const [open, setOpen] = useState(false)
  const [confirmClear, setConfirmClear] = useState(false)
  const panelRef = useRef<HTMLDivElement>(null)
  useModalFocus(panelRef, open)
  const close = useCallback(() => {
    setOpen(false)
    setConfirmClear(false)
  }, [])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const trigger = useRef<HTMLButtonElement>(null)

  const qc = useQueryClient()
  const { data, isError, refetch } = useNotifications()
  const unread = data?.unread ?? 0
  const items = (data?.items ?? []).slice(0, 8)
  useDesktopNotifications(data?.items)
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
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={unread ? `${unread} unread notifications` : 'Notifications'}
        onClick={() => (open ? close() : setOpen(true))}
        className="tap relative grid size-9 place-items-center rounded-full text-base-300 transition-colors hover:bg-base-800 hover:text-white"
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
            <div className="flex items-center justify-between gap-2 border-b border-line px-3 py-2">
              <p className="text-sm font-medium text-base-100">
                Notifications
                {unread > 0 && (
                  <span className="ml-1.5">
                    <Badge tone="sky">{unread}</Badge>
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
                      className="rounded px-1.5 py-0.5 text-xs text-base-400 transition-colors hover:bg-base-800 hover:text-red-300 pointer-coarse:min-h-11 pointer-coarse:px-3"
                    >
                      Clear
                    </button>
                  ))}
              </div>
            </div>

            {isError && items.length === 0 ? (
              <div className="flex flex-col items-center gap-2 px-3 py-6 text-center text-xs text-red-300">
                Couldn't load notifications.
                <Button size="sm" onClick={() => void refetch()}>
                  Retry
                </Button>
              </div>
            ) : items.length === 0 ? (
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
                      if (!item.read) markRead.mutate(item.id)
                      close()
                    }}
                    onRemove={() => remove.mutate(item.id)}
                  />
                ))}
              </ul>
            )}

            <Link
              to="/notifications"
              onClick={close}
              className="block border-t border-line px-3 py-2 text-center text-xs text-base-400 transition-colors hover:bg-base-800 hover:text-white pointer-coarse:py-3"
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
          !item.read && 'bg-sky-400/5',
        )}
      >
        <div className="flex items-start gap-2">
          {!item.read && (
            <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-sky-400">
              <span className="sr-only">Unread</span>
            </span>
          )}
          <div className="min-w-0">
            <p className="line-clamp-2 text-xs font-medium text-base-100">{feedTitle(item)}</p>
            <p className="mt-0.5 line-clamp-1 text-xs text-base-500">
              {item.kind === 'release' || !item.body ? whenLabel(item.createdAt) : `${notificationBody(item)} · ${whenLabel(item.createdAt)}`}
            </p>
          </div>
        </div>
      </Link>

      <button
        onClick={onRemove}
        aria-label="Remove notification"
        className="tap absolute top-2 right-2 rounded p-1 text-base-400 transition-colors hover:text-red-300"
      >
        <svg viewBox="0 0 24 24" className="size-3" aria-hidden>
          <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
        </svg>
      </button>
    </li>
  )
}

const SEEN_KEY = 'yoru.notify.seen'

function useDesktopNotifications(items?: Notification[]) {
  useEffect(() => {
    if (!items || typeof Notification === 'undefined') return
    const newest = items.reduce((m, n) => Math.max(m, n.id), 0)
    let seen = 0
    let first = true
    try {
      const stored = localStorage.getItem(SEEN_KEY)
      if (stored !== null && Number.isFinite(Number(stored))) {
        seen = Number(stored)
        first = false
      }
    } catch {
      first = true
    }
    const fresh = items.filter((n) => n.id > seen && !n.read).reverse()
    if (!first && fresh.length > 0 && desktopNotifyWanted() && Notification.permission === 'granted') {
      void (navigator.serviceWorker?.getRegistration() ?? Promise.resolve(undefined))
        .catch(() => undefined)
        .then(async (reg) => {
          const pushed = await (reg?.pushManager?.getSubscription() ?? Promise.resolve(null)).catch(() => null)
          if (pushed) return
          for (const n of fresh) {
            const target = new URL(notificationTarget(n), window.location.href).href
            const options = { body: notificationBody(n), tag: `yoru-${n.id}`, data: { url: target } }
            if (reg) {
              await reg.showNotification(n.title, options).catch(() => {})
              continue
            }
            try {
              const note = new Notification(n.title, options)
              note.onclick = () => {
                window.focus()
                window.location.assign(target)
              }
            } catch {
              return
            }
          }
        })
        .catch(() => {})
    }
    if (newest > seen || first) {
      try {
        localStorage.setItem(SEEN_KEY, String(newest))
      } catch {
        return
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
