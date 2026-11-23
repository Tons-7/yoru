import { useSyncExternalStore } from 'react'

export type OnMobile = 'ask' | 'saver' | 'full' | 'wifi'
export type Quality = 'auto' | 'original' | '1080' | '720' | '480'

export const ON_MOBILE: { value: OnMobile; label: string }[] = [
  { value: 'ask', label: 'Ask first' },
  { value: 'saver', label: 'Data saver' },
  { value: 'full', label: 'Full quality' },
  { value: 'wifi', label: 'Wi-Fi only' },
]
export const QUALITIES: { value: Quality; label: string }[] = [
  { value: 'auto', label: 'Auto' },
  { value: 'original', label: 'Original' },
  { value: '1080', label: '1080p' },
  { value: '720', label: '720p' },
  { value: '480', label: '480p' },
]
export const SAVER = '480'
export const LOWER: Record<string, '1080' | '720' | '480' | undefined> = { original: '1080', '1080': '720', '720': '480' }

const STEP_RATE: Record<string, number> = { '1080': 6_128_000, '720': 3_128_000, '480': 1_328_000 }

export const stepBytes = (step: string, seconds: number) => ((STEP_RATE[step] ?? 0) / 8) * seconds

const MOBILE_KEY = 'yoru.mobileData'
const QUALITY_KEY = 'yoru.quality'
const SITTING_KEY = 'yoru.mobileData.sitting'

const listeners = new Set<() => void>()
const changed = () => listeners.forEach((l) => l())

function read(store: Storage | undefined, key: string): string | null {
  try {
    return store?.getItem(key) ?? null
  } catch {
    return null
  }
}
function write(store: Storage | undefined, key: string, value: string | null) {
  try {
    if (value === null) store?.removeItem(key)
    else store?.setItem(key, value)
  } catch {
  }
  changed()
}
const local = () => (typeof localStorage === 'undefined' ? undefined : localStorage)
const session = () => (typeof sessionStorage === 'undefined' ? undefined : sessionStorage)

export const onMobile = (): OnMobile => {
  const v = read(local(), MOBILE_KEY)
  return ON_MOBILE.some((o) => o.value === v) ? (v as OnMobile) : 'ask'
}
export const setOnMobile = (v: OnMobile) => write(local(), MOBILE_KEY, v === 'ask' ? null : v)

export const deviceQuality = (): Quality => {
  const v = read(local(), QUALITY_KEY)
  return QUALITIES.some((o) => o.value === v) ? (v as Quality) : 'auto'
}
export const setDeviceQuality = (v: Quality) => write(local(), QUALITY_KEY, v === 'auto' ? null : v)

export type Sitting = 'full' | 'saver' | null
export const sitting = (): Sitting => {
  const v = read(session(), SITTING_KEY)
  return v === 'full' || v === 'saver' ? v : null
}
export const setSitting = (v: Sitting) => write(session(), SITTING_KEY, v)

type Connection = EventTarget & { type?: string; saveData?: boolean }
const connection = () => (navigator as Navigator & { connection?: Connection }).connection

export function cellular(): boolean | null {
  const type = connection()?.type
  if (!type || type === 'unknown') return null
  return type === 'cellular'
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  const c = connection()
  c?.addEventListener('change', listener)
  window.addEventListener('storage', listener)
  return () => {
    listeners.delete(listener)
    c?.removeEventListener('change', listener)
    window.removeEventListener('storage', listener)
  }
}
const snapshot = () => `${cellular()}|${onMobile()}|${deviceQuality()}|${sitting()}`

export function streamChoice(
  net: ReturnType<typeof useNetwork>,
  away: boolean,
  stepped?: string,
): { step?: string; card?: 'ask' | 'wifi' } {
  if (net.quality !== 'auto') return { step: net.quality === 'original' ? undefined : net.quality }
  if (net.cellular ?? away) {
    if (net.sitting) return { step: net.sitting === 'saver' ? SAVER : stepped }
    if (net.onMobile === 'ask' || net.onMobile === 'wifi') return { card: net.onMobile }
    if (net.onMobile === 'saver') return { step: SAVER }
  }
  return { step: away ? stepped : undefined }
}

export function useNetwork() {
  useSyncExternalStore(subscribe, snapshot, () => '')
  return { cellular: cellular(), onMobile: onMobile(), quality: deviceQuality(), sitting: sitting() }
}
