export const HOME_ROWS = [
  { id: 'upnext', label: 'Up next' },
  { id: 'released', label: 'Recently released' },
  { id: 'airing', label: 'Airing today' },
  { id: 'trending', label: 'Top 10' },
  { id: 'foryou', label: 'For you' },
  { id: 'sequels', label: 'Sequels you missed' },
  { id: 'season', label: 'This season' },
  { id: 'upcoming', label: 'Upcoming' },
] as const

export type HomeRow = (typeof HOME_ROWS)[number]['id']

export function hiddenRows(value: string | undefined): Set<string> {
  try {
    const list: unknown = JSON.parse(value || '[]')
    return new Set(Array.isArray(list) ? list.filter((v) => typeof v === 'string') : [])
  } catch {
    return new Set()
  }
}
