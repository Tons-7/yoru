import type { QueryClient } from '@tanstack/react-query'

const DELAYS = [1500, 3000, 6000, 12_000, 20_000]
const REST_AFTER_GIVING_UP = 5 * 60_000

const BEHIND = '__behind'

const waiting = new Map<string, { tries: number; timer: ReturnType<typeof setTimeout> }>()
const resting = new Map<string, number>()
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
    if (event.type === 'removed') {
      resting.delete(query.queryHash)
      return settle(query.queryHash)
    }
    if (event.type !== 'updated') return
    if (event.action.type === 'error') return settle(query.queryHash)
    if (event.action.type !== 'success' || event.action.manual) return
    if (!isBehind(query.state.data)) {
      resting.delete(query.queryHash)
      return settle(query.queryHash)
    }
    const until = resting.get(query.queryHash)
    if (until !== undefined && Date.now() < until) return settle(query.queryHash)
    resting.delete(query.queryHash)
    const was = waiting.get(query.queryHash)
    if (was) clearTimeout(was.timer)
    const tries = (was?.tries ?? 0) + 1
    const giveUp = () => {
      resting.set(query.queryHash, Date.now() + REST_AFTER_GIVING_UP)
      void client.invalidateQueries({ queryKey: query.queryKey, exact: true, refetchType: 'none' })
      settle(query.queryHash)
    }
    if (tries > DELAYS.length) return giveUp()
    const timer = setTimeout(() => {
      const observer = query.observers[0]
      if (!observer) return giveUp()
      void observer.refetch().catch(() => settle(query.queryHash))
    }, DELAYS[tries - 1])
    waiting.set(query.queryHash, { tries, timer })
    if (!was) tell()
  })
}
