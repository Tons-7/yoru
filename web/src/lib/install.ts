import { useSyncExternalStore } from 'react'

interface InstallOffer extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

export type InstallState = 'installed' | 'ready' | 'share-sheet' | 'menu' | 'https'

let offer: InstallOffer | null = null
let installed = false
const listeners = new Set<() => void>()
const changed = () => {
  for (const l of listeners) l()
}

export const runningInstalled = () =>
  matchMedia('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone === true

export const appleTouchDevice = () =>
  /iPhone|iPad|iPod/.test(navigator.userAgent) || (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1)

export function listenForInstall() {
  installed = runningInstalled()
  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault()
    offer = e as InstallOffer
    changed()
  })
  window.addEventListener('appinstalled', () => {
    offer = null
    installed = true
    changed()
  })
}

function current(): InstallState {
  if (installed) return 'installed'
  if (offer) return 'ready'
  if (!window.isSecureContext) return 'https'
  return appleTouchDevice() ? 'share-sheet' : 'menu'
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

export function useInstall(): InstallState {
  return useSyncExternalStore(subscribe, current)
}

export async function install(): Promise<boolean> {
  const asked = offer
  if (!asked) return false
  try {
    await asked.prompt()
    const { outcome } = await asked.userChoice
    return outcome === 'accepted'
  } catch {
    return false
  } finally {
    offer = null
    changed()
  }
}

export const INSTALL_HELP: Record<Exclude<InstallState, 'ready'>, string> = {
  installed: 'Yoru is installed on this device.',
  'share-sheet': 'In Safari, tap Share, then Add to Home Screen.',
  menu: "Use your browser's menu: Install app, or Add to Home screen.",
  https: 'Installing needs the secure (https) address: turn it on in Settings, Devices, then open Yoru from it.',
}
