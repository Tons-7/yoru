import { useSyncExternalStore } from 'react'

let focused = false
const listeners = new Set<() => void>()

export function setFocus(on: boolean) {
  if (focused === on) return
  focused = on
  listeners.forEach((l) => l())
}

export function useFocus(): boolean {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => focused,
  )
}
