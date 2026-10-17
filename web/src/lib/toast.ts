export interface Toast {
  id: number
  message: string
  tone: 'error' | 'info'
  action?: ToastAction
}

export interface ToastAction {
  label: string
  run: () => void
}

/** How long a toast stays, and so how long its action (an Undo) can be pressed. */
export const TOAST_MS = 6000

// Outside React so a mutation cache, which is not a component, can raise one.
let listeners: Array<(t: Toast) => void> = []
let next = 1

export function toast(message: string, tone: Toast['tone'] = 'error', action?: ToastAction) {
  const t = { id: next++, message, tone, action }
  for (const l of listeners) l(t)
}

/** Hears every toast raised from now on; the returned function stops it. */
export function onToast(listener: (t: Toast) => void) {
  listeners.push(listener)
  return () => {
    listeners = listeners.filter((l) => l !== listener)
  }
}
