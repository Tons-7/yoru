import type { Notification } from './queries'

export const DESKTOP_NOTIFY_KEY = 'yoru.notify.desktop'

export function desktopNotifyWanted(): boolean {
  try {
    return localStorage.getItem(DESKTOP_NOTIFY_KEY) === '1'
  } catch {
    return false
  }
}

export function notificationTarget(n: Notification): string {
  if (n.kind === 'update') return '/settings?tab=About'
  return n.animeId ? `/watch/${n.animeId}/${n.episode || 1}` : '#'
}
