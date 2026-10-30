import type { ScheduleItem } from './api'
import { relativeTime } from './format'
import { useAired, useNow } from './queries'

export const RANGES = [
  { value: '24', label: 'Last day' },
  { value: '72', label: '3 days' },
  { value: '168', label: 'Week' },
] as const

export type Range = (typeof RANGES)[number]['value']

export function useOutNow(range: Range) {
  const hours = Number(range)
  const now = useNow() / 1000
  const query = useAired(hours)
  const since = now - hours * 3600
  const aired = (query.data?.items ?? [])
    .filter((item) => item.airingAt <= now && item.airingAt >= since)
    .sort((a, b) => b.airingAt - a.airingAt)
  const unwatched = new Set(aired.filter((item) => item.onList && !item.watched).map((item) => item.animeId)).size
  return { query, aired, unwatched }
}

export type ReleaseTone = 'done' | 'ready' | 'wait'

export interface ReleaseState {
  word: string
  tone: ReleaseTone
  checked?: string
}

export function releaseState(item: ScheduleItem, now: number): ReleaseState | null {
  if (item.watched && item.airingAt <= now) return { word: 'Watched', tone: 'done' }
  if (item.out) return { word: 'Ready to play', tone: 'ready' }
  if (item.watch && item.airingAt <= now) {
    return { word: 'Not found yet', tone: 'wait', checked: item.checkedAt ? `checked ${relativeTime(item.checkedAt)}` : undefined }
  }
  return null
}

export const TONE_TEXT: Record<ReleaseTone, string> = {
  done: 'text-emerald-300',
  ready: 'text-sky-300',
  wait: 'text-base-400',
}

export function minutesLeft(item: ScheduleItem): number | null {
  if (!item.position || !item.duration || item.watched) return null
  return Math.max(1, Math.round((item.duration - item.position) / 60))
}

export interface ShowRelease {
  key: string
  first: ScheduleItem
  newest: ScheduleItem
  episodes: ScheduleItem[]
}

export function byShow(items: ScheduleItem[]): ShowRelease[] {
  const shows = new Map<number, ScheduleItem[]>()
  for (const item of items) shows.set(item.animeId, [...(shows.get(item.animeId) ?? []), item])
  return [...shows.values()].map((group) => {
    const episodes = [...group].sort((a, b) => a.episode - b.episode)
    const newest = episodes[episodes.length - 1]
    const first = episodes.find((it) => !it.watched) ?? newest
    return { key: `${newest.animeId}-${newest.episode}`, first, newest, episodes }
  })
}
