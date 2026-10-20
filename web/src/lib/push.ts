import { api } from './api'

/** What alerts with Yoru closed can do on this device, or why not. */
export type PushState = 'on' | 'off' | 'denied' | 'insecure' | 'install' | 'unsupported'

const iPhone = () =>
  /iPhone|iPad|iPod/.test(navigator.userAgent) || (navigator.userAgent.includes('Mac') && navigator.maxTouchPoints > 1)

const installed = () =>
  window.matchMedia('(display-mode: standalone)').matches || (navigator as { standalone?: boolean }).standalone === true

const supported = () => 'serviceWorker' in navigator && 'PushManager' in window

// iPhone Safari offers push only to Yoru added to the Home Screen; plain http has no service workers at all.
export async function pushState(): Promise<PushState> {
  if (iPhone() && !installed()) return 'install'
  if (!window.isSecureContext) return 'insecure'
  if (!supported() || typeof Notification === 'undefined') return 'unsupported'
  if (Notification.permission === 'denied') return 'denied'
  const sub = await subscription()
  return sub ? 'on' : 'off'
}

async function subscription(): Promise<PushSubscription | null> {
  const reg = await navigator.serviceWorker.getRegistration()
  return (await reg?.pushManager.getSubscription()) ?? null
}

/** Registers the alerts worker; on every load while alerts are wanted, so an updated one takes over. */
export function registerWorker(): Promise<ServiceWorkerRegistration> | undefined {
  if (!window.isSecureContext || !supported()) return undefined
  return navigator.serviceWorker.register('/sw.js')
}

function keyBytes(key: string): Uint8Array<ArrayBuffer> {
  const padded = key.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (key.length % 4)) % 4)
  return Uint8Array.from(atob(padded), (c) => c.charCodeAt(0))
}

const sameKey = (a: ArrayBuffer | null | undefined, b: Uint8Array) =>
  !!a && a.byteLength === b.byteLength && new Uint8Array(a).every((v, i) => v === b[i])

/** Subscribes this device and tells Yoru where to send. Call from a tap: it may ask for permission. */
export async function enablePush(): Promise<void> {
  const reg = await registerWorker()
  if (!reg) throw new Error('This browser cannot get alerts with Yoru closed')
  await navigator.serviceWorker.ready
  const { publicKey } = await api.get<{ publicKey: string }>('/api/push')
  const key = keyBytes(publicKey)
  let sub = await reg.pushManager.getSubscription()
  // Made for another key (Yoru's data was reset): the push service would refuse every alert sent with this one.
  if (sub && !sameKey(sub.options.applicationServerKey, key)) {
    await sub.unsubscribe()
    sub = null
  }
  sub ??= await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: key })
  await api.post('/api/push/subscribe', sub.toJSON())
}

export async function disablePush(): Promise<void> {
  const sub = await subscription()
  if (!sub) return
  await api.post('/api/push/unsubscribe', { endpoint: sub.endpoint }).catch(() => {})
  await sub.unsubscribe()
}

/** One alert to this device only. */
export async function testPush(): Promise<void> {
  const sub = await subscription()
  if (!sub) throw new Error('Alerts are off on this device')
  await api.post('/api/push/test', { endpoint: sub.endpoint })
}
