import { useCallback, useState } from 'react'
import { usePrefs } from './queries'

/** Home's rows: taken off in Settings (home.hidden, for every device), folded on Home (this device). */
export const HOME_ROWS = [
  { id: 'upnext', label: 'Up next' },
  { id: 'continue', label: 'Continue watching' },
  { id: 'sequels', label: 'Sequels you missed' },
  { id: 'foryou', label: 'For you' },
  { id: 'released', label: 'Recently released' },
  { id: 'season', label: 'This season' },
  { id: 'trending', label: 'Trending now' },
  { id: 'upcoming', label: 'Upcoming' },
] as const

export type HomeRow = (typeof HOME_ROWS)[number]['id']

const FOLDED_KEY = 'yoru.home.folded'

/** The rows taken off Home, from the setting; a broken value hides none. */
export function hiddenRows(value: string | undefined): Set<string> {
  try {
    const list: unknown = JSON.parse(value || '[]')
    return new Set(Array.isArray(list) ? list.filter((v) => typeof v === 'string') : [])
  } catch {
    return new Set()
  }
}

function readFolded(): Set<string> {
  try {
    return hiddenRows(localStorage.getItem(FOLDED_KEY) ?? undefined)
  } catch {
    return new Set()
  }
}

/** Which rows Home shows, and which of them are folded to their heading here. */
export function useHomeRows() {
  const prefs = usePrefs()
  const hidden = hiddenRows(prefs.data?.effective['home.hidden'])
  const [folded, setFolded] = useState(readFolded)
  const toggle = useCallback((id: HomeRow) => {
    setFolded((was) => {
      const next = new Set(was)
      if (!next.delete(id)) next.add(id)
      try {
        localStorage.setItem(FOLDED_KEY, JSON.stringify([...next]))
      } catch {
        // Kept for this visit only.
      }
      return next
    })
  }, [])
  return {
    shown: (id: HomeRow) => !hidden.has(id),
    // Known shown, the settings loaded: a row taken off mustn't be fetched while they load.
    wanted: (id: HomeRow) => prefs.isSuccess && !hidden.has(id),
    fold: (id: HomeRow) => ({ folded: folded.has(id), onToggle: () => toggle(id) }),
  }
}
