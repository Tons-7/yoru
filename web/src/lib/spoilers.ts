import { useSyncExternalStore } from 'react'

// Episodes tapped open despite the spoiler rule, until the page reloads: one tap, not a setting.
let revealed = new Set<string>()
const listeners = new Set<() => void>()

export function reveal(animeId: number, episode: number) {
  revealed = new Set(revealed).add(`${animeId}-${episode}`)
  for (const l of listeners) l()
}

/** Whether an episode the server flagged as a spoiler was opened by hand. */
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
