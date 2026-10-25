import { useSyncExternalStore } from 'react'

// Installing Yoru as an app: the browser's offer comes once, early, so it is kept from start-up for the button.

interface InstallOffer extends Event {
  prompt: () => Promise<void>
  userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>
}

/** ready: one tap; share-sheet: iPhone/iPad's Share menu; menu: the browser's own; https: not over http. */
export type InstallState = 'installed' | 'ready' | 'share-sheet' | 'menu' | 'https'

let offer: InstallOffer | null = null
let installed = false
const listeners = new Set<() => void>()
const changed = () => {
  for (const l of listeners) l()
}

/** Running as the installed app, not in a browser tab. */
export const runningInstalled = () =>
  matchMedia('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone === true

/** An iPhone or iPad: iPads say Macintosh, and touch gives them away. */
export const appleTouchDevice = () =>
  /iPhone|iPad|iPod/.test(navigator.userAgent) || (/Macintosh/.test(navigator.userAgent) && navigator.maxTouchPoints > 1)

/** Called once at start-up, before the offer can come and go unheard. */
export function listenForInstall() {
  installed = runningInstalled()
  window.addEventListener('beforeinstallprompt', (e) => {
    // Kept for the button, not shown by the browser at a moment of its choosing.
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
  // Safari there never offers.
  return appleTouchDevice() ? 'share-sheet' : 'menu'
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

export function useInstall(): InstallState {
  return useSyncExternalStore(subscribe, current)
}

/** Shows the browser's install question; false when it was turned down or there was nothing to show. */
export async function install(): Promise<boolean> {
  const asked = offer
  if (!asked) return false
  await asked.prompt()
  const { outcome } = await asked.userChoice
  // An offer is good for one question.
  offer = null
  changed()
  return outcome === 'accepted'
}

/** What to do, in words, where one tap can't install. */
export const INSTALL_HELP: Record<Exclude<InstallState, 'ready'>, string> = {
  installed: 'Yoru is installed on this device.',
  'share-sheet': 'In Safari, tap Share, then Add to Home Screen.',
  menu: "Use your browser's menu: Install app, or Add to Home screen.",
  https: 'Installing needs the secure (https) address: turn it on in Settings, Devices, then open Yoru from it.',
}
