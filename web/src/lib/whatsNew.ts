import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import { usePrefs } from './queries'

export interface NewSetting {
  label: string
  since: string
}
export const NEW_SETTINGS: NewSetting[] = [
  { label: 'Keyboard shortcuts', since: '2026.10.25' },
  { label: 'Install on this device', since: '2026.10.25' },
  { label: 'Subtitle look', since: '2026.10.25' },
  { label: 'Jimaku (Japanese subtitles)', since: '2026.10.25' },
  { label: 'Hardware decoding', since: '2026.10.25' },
  { label: 'Smooth banding', since: '2026.10.25' },
  { label: 'Soften flashing', since: '2026.10.25' },
  { label: 'On mobile data', since: '2026.10.25' },
  { label: 'Streaming quality', since: '2026.10.25' },
  { label: 'Pair a device', since: '2026.10.28' },
  { label: 'Rows on Home', since: '2026.10.29' },
  { label: 'Adult titles', since: '2026.10.29' },
  { label: 'Raise subtitles', since: '2026.10.30.1' },
  { label: 'Skip openings', since: '2026.10.30.2' },
  { label: 'Skip endings', since: '2026.10.30.2' },
]

export interface Change {
  what: string
  before: string
  now: string
  since: string
}
export const CHANGES: Change[] = [
  { what: 'Download all', before: 'every episode not on disk', now: 'the unwatched ones', since: '2026.10.25' },
  {
    what: 'HEVC and 10-bit releases',
    before: 'converted for every device',
    now: 'played as they are wherever the device decodes them',
    since: '2026.10.25',
  },
  { what: 'Only my list, on Schedule and Recent', before: 'kept by each browser', now: 'the same on every device', since: '2026.10.30.1' },
  { what: 'Adult titles', before: 'left out of For you only', now: 'left out of For you, Schedule and Recent', since: '2026.10.30.1' },
]

export function newer(a: string, b: string): boolean {
  const x = a.split('.').map(Number)
  const y = b.split('.').map(Number)
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    if ((x[i] ?? 0) !== (y[i] ?? 0)) return (x[i] ?? 0) > (y[i] ?? 0)
  }
  return false
}

export const SEEN_KEY = 'settings.new_seen'

export function useNewSettings() {
  const update = useQuery({
    queryKey: ['update-since'],
    queryFn: () => api.get<{ current: string; updatedFrom?: string }>('/api/update/since'),
    staleTime: 5 * 60_000,
  })
  const prefs = usePrefs()
  const from = update.data?.updatedFrom
  const current = update.data?.current ?? ''
  const rows = from ? NEW_SETTINGS.filter((s) => newer(s.since, from)) : []
  const changes = from ? CHANGES.filter((c) => newer(c.since, from)) : []
  const seen = prefs.data?.effective[SEEN_KEY] === current
  return { from, current, rows, changes, unseen: rows.length + changes.length > 0 && !seen && !!prefs.data }
}
