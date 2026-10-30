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

export const TOAST_MS = 6000

let listeners: Array<(t: Toast) => void> = []
let dismissers: Array<(id: number) => void> = []
let next = 1

export function toast(message: string, tone: Toast['tone'] = 'error', action?: ToastAction) {
  const t = { id: next++, message, tone, action }
  for (const l of listeners) l(t)
  return t.id
}

export function dismissToast(id: number) {
  for (const d of dismissers) d(id)
}

export function onToast(listener: (t: Toast) => void) {
  listeners.push(listener)
  return () => {
    listeners = listeners.filter((l) => l !== listener)
  }
}

export function onDismiss(listener: (id: number) => void) {
  dismissers.push(listener)
  return () => {
    dismissers = dismissers.filter((d) => d !== listener)
  }
}
