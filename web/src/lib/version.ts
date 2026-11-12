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
        else if (!first && health.version !== seen && !playingNow()) window.location.reload()
      })
      .catch(() => {})
  void ask(true)
  window.setInterval(() => {
    if (document.visibilityState === 'visible') void ask(false)
  }, EVERY_MS)
}
