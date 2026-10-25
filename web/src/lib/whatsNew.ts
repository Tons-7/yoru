import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import { usePrefs } from './queries'

// Settings rows by arrival date: versions are dates, so one newer than the version updated from is new.
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
]

// What works differently, with no setting of its own to show.
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
]

/** a is a later version than b: 2026.10.24.1 after 2026.10.24, numbers compared, not text. */
export function newer(a: string, b: string): boolean {
  const x = a.split('.').map(Number)
  const y = b.split('.').map(Number)
  for (let i = 0; i < Math.max(x.length, y.length); i++) {
    if ((x[i] ?? 0) !== (y[i] ?? 0)) return (x[i] ?? 0) > (y[i] ?? 0)
  }
  return false
}

export const SEEN_KEY = 'settings.new_seen'

/** What updates added to Settings since the version replaced; unseen until "Seen them" for this one. */
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
