import { useSyncExternalStore } from 'react'

let revealed = new Set<string>()
const listeners = new Set<() => void>()

function publish(next: Set<string>) {
  revealed = next
  for (const l of listeners) l()
}

export function reveal(animeId: number, episode: number) {
  publish(new Set(revealed).add(`${animeId}-${episode}`))
}

export function conceal(animeId: number, episode: number) {
  const next = new Set(revealed)
  next.delete(`${animeId}-${episode}`)
  publish(next)
}

export function useRevealed(): (animeId: number, episode: number) => boolean {
  const set = useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    () => revealed,
  )
  return (animeId, episode) => set.has(`${animeId}-${episode}`)
}
