import type { QueryClient } from '@tanstack/react-query'

const DELAYS = [1500, 3000, 6000, 12_000, 20_000]

const BEHIND = '__behind'

const waiting = new Map<string, { tries: number; timer: ReturnType<typeof setTimeout> }>()
const listeners = new Set<() => void>()
const tell = () => listeners.forEach((l) => l())

export function markRefreshing(data: unknown) {
  if (typeof data === 'object' && data !== null && !Array.isArray(data)) (data as Record<string, unknown>)[BEHIND] = true
}

const isBehind = (data: unknown) =>
  typeof data === 'object' && data !== null && (data as Record<string, unknown>)[BEHIND] === true

export const freshness = {
  subscribe(listener: () => void) {
    listeners.add(listener)
    return () => listeners.delete(listener)
  },
  updating: () => waiting.size > 0,
}

export function watchRefreshes(client: QueryClient) {
  const settle = (hash: string) => {
    const was = waiting.get(hash)
    if (!was) return
    clearTimeout(was.timer)
    waiting.delete(hash)
    tell()
  }
  client.getQueryCache().subscribe((event) => {
    const query = event.query
    if (event.type === 'removed') return settle(query.queryHash)
    if (event.type !== 'updated') return
    if (event.action.type === 'error') return settle(query.queryHash)
    if (event.action.type !== 'success') return
    if (!isBehind(query.state.data)) return settle(query.queryHash)
    const was = waiting.get(query.queryHash)
    if (was) clearTimeout(was.timer)
    const tries = (was?.tries ?? 0) + 1
    if (tries > DELAYS.length) return settle(query.queryHash)
    const timer = setTimeout(() => {
      const observer = query.observers[0]
      if (!observer) return settle(query.queryHash)
      void observer.refetch().catch(() => settle(query.queryHash))
    }, DELAYS[tries - 1])
    waiting.set(query.queryHash, { tries, timer })
    if (!was) tell()
  })
}
