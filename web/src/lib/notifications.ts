import type { Notification } from './queries'
import { readKey } from './storage'

export const DESKTOP_NOTIFY_KEY = 'yoru.notify.desktop'

export function desktopNotifyWanted(): boolean {
  return readKey(DESKTOP_NOTIFY_KEY) === '1'
}

export function notificationTarget(n: Notification): string {
  if (n.kind === 'update') return '/settings?tab=About'
  if ((n.kind === 'delay' || n.kind === 'season') && n.animeId) return `/anime/${n.animeId}`
  return n.animeId ? `/watch/${n.animeId}/${n.episode || 1}` : '#'
}
