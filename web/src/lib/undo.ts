import { useSyncExternalStore } from 'react'
import { dismissToast, toast, TOAST_MS } from './toast'

let hidden: ReadonlySet<string> = new Set()
const listeners = new Set<() => void>()

function setHidden(keys: string[], on: boolean) {
  const next = new Set(hidden)
  for (const key of keys) {
    if (on) next.add(key)
    else next.delete(key)
  }
  hidden = next
  listeners.forEach((l) => l())
}

export function useHiddenKeys(): ReadonlySet<string> {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => hidden,
  )
}

export interface Removal {
  keys: string[]
  message: string
  failed: string
  run: () => Promise<unknown>
}

const pending = new Map<number, { removal: Removal; toastId: number }>()
window.addEventListener('pagehide', () => {
  for (const [timer, { removal, toastId }] of [...pending]) {
    window.clearTimeout(timer)
    pending.delete(timer)
    dismissToast(toastId)
    void removal
      .run()
      .finally(() => setHidden(removal.keys, false))
      .catch(() => {})
  }
})

export function removeWithUndo(removal: Removal) {
  const { keys, message, failed, run } = removal
  setHidden(keys, true)
  let undone = false
  const timer = window.setTimeout(() => {
    pending.delete(timer)
    if (undone) return
    run().then(
      () => setHidden(keys, false),
      (err: unknown) => {
        setHidden(keys, false)
        toast(`${failed}: ${err instanceof Error ? err.message : 'something went wrong'}`, 'error', {
          label: 'Try again',
          run: () => removeWithUndo(removal),
        })
      },
    )
  }, TOAST_MS)
  const toastId = toast(message, 'info', {
    label: 'Undo',
    run: () => {
      undone = true
      window.clearTimeout(timer)
      pending.delete(timer)
      setHidden(keys, false)
    },
  })
  pending.set(timer, { removal, toastId })
}
