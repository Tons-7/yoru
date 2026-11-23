import { useSyncExternalStore } from 'react'


export type Unattended = { animeId: number; episode: number }

let unattended: Unattended[] = []

export function noteInput() {
  if (unattended.length) unattended = []
}

export function noteAutoStart(e: Unattended) {
  unattended = [...unattended, e]
}

export function unattendedRun(): Unattended[] {
  return unattended
}

export type Sleep = { kind: 'off' } | { kind: 'at'; at: number } | { kind: 'episode' } | { kind: 'fired' }

let sleep: Sleep = { kind: 'off' }
let timer = 0
const listeners = new Set<() => void>()

export function setSleep(next: Sleep) {
  window.clearTimeout(timer)
  sleep = next
  if (next.kind === 'at') timer = window.setTimeout(() => setSleep({ kind: 'fired' }), Math.max(0, next.at - Date.now()))
  for (const l of listeners) l()
}

export function getSleep(): Sleep {
  return sleep
}

function subscribeSleep(l: () => void) {
  listeners.add(l)
  return () => listeners.delete(l)
}

export function useSleep(): Sleep {
  return useSyncExternalStore(subscribeSleep, () => sleep)
}
