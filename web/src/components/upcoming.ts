function parts(date?: string | null) {
  const m = date?.match(/^(\d{4})(?:-(\d{2}))?(?:-(\d{2}))?$/)
  return { year: Number(m?.[1] ?? 0), month: Number(m?.[2] ?? 0), day: Number(m?.[3] ?? 0) }
}

function startKey(date?: string | null): number {
  const { year, month, day } = parts(date)
  if (!year) return Number.MAX_SAFE_INTEGER
  return year * 10_000 + (month || 13) * 100 + (day || 32)
}

export function soonestFirst<T extends { startDate?: string | null }>(items: T[]): T[] {
  return items
    .map((item, i) => ({ item, i, key: startKey(item.startDate) }))
    .sort((a, b) => a.key - b.key || a.i - b.i)
    .map((x) => x.item)
}

export const START_GROUPS = ['This month', 'Next month', 'Later'] as const

export type StartGroup = (typeof START_GROUPS)[number]

export function startGroup(date: string | null | undefined, now = new Date()): StartGroup {
  const { year, month } = parts(date)
  if (!year || !month) return 'Later'
  const ahead = (year - now.getFullYear()) * 12 + (month - 1 - now.getMonth())
  if (ahead <= 0) return 'This month'
  if (ahead === 1) return 'Next month'
  return 'Later'
}
