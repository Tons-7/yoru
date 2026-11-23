import { api } from './api'
import { playingNow } from './miniplayer'

const EVERY_MS = 60_000

export function watchVersion() {
  let seen = ''
  const ask = (first: boolean) =>
    api
      .get<{ version?: string }>('/api/health')
      .then((health) => {
        if (!health.version) return
        if (!seen) seen = health.version
        else if (!first && health.version !== seen && !playingNow() && !midTask()) window.location.reload()
      })
      .catch(() => {})
  void ask(true)
  window.setInterval(() => {
    if (document.visibilityState === 'visible') void ask(false)
  }, EVERY_MS)
}

function midTask(): boolean {
  if (document.querySelector('[role="dialog"], [role="alertdialog"]')) return true
  const typing = document.activeElement
  return (typing instanceof HTMLInputElement || typing instanceof HTMLTextAreaElement) && typing.value !== ''
}
