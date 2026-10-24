import { useSyncExternalStore } from 'react'

// Whether anyone is still watching: the episodes auto next started untouched, and the sleep timer. Module
// state, so both carry from one episode's page to the next.

export type Unattended = { animeId: number; episode: number }

let unattended: Unattended[] = []

/** A key, tap, wheel turn or mouse movement: someone is there. */
export function noteInput() {
  if (unattended.length) unattended = []
}

/** An episode auto next started by itself. */
export function noteAutoStart(e: Unattended) {
  unattended = [...unattended, e]
}

export function unattendedRun(): Unattended[] {
  return unattended
}

/** Off; a time it pauses at; the end of the episode playing; or it went off and stopped playback. */
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

export function useSleep(): Sleep {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => sleep,
  )
}
