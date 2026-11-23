import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import { useDocumentTitle } from '../lib/hooks'
import { ErrorState, Skeleton } from '../components/ui'

interface Taste {
  name: string
  seconds: number
  shows: number
}

interface StatsData {
  totalSeconds: number
  episodes: number
  completed: number
  shows: number
  genres: Taste[]
  studios: Taste[]
  seasons: Taste[]
  months: { month: string; seconds: number }[]
}

function hours(seconds: number) {
  const h = seconds / 3600
  return h >= 10 ? `${Math.round(h).toLocaleString()} h` : h >= 1 ? `${h.toFixed(1)} h` : `${Math.round(seconds / 60)} min`
}

const SEASONS: Record<string, string> = { WINTER: 'Winter', SPRING: 'Spring', SUMMER: 'Summer', FALL: 'Fall' }

function seasonName(name: string) {
  const [season, year] = name.split(' ')
  return `${SEASONS[season] ?? season} ${year ?? ''}`.trim()
}

function monthName(month: string, short: boolean) {
  const [y, m] = month.split('-').map(Number)
  return new Date(y, m - 1, 1).toLocaleDateString(undefined, short ? { month: 'narrow' } : { month: 'long', year: 'numeric' })
}

export function StatsPage() {
  useDocumentTitle('Statistics')
  const stats = useQuery({
    queryKey: ['stats'],
    queryFn: () => api.get<StatsData>('/api/stats'),
    staleTime: 60_000,
  })

  return (
    <div className="mx-auto w-full max-w-3xl space-y-4 sm:space-y-5">
      <h1 className="page-title">Statistics</h1>
      {stats.isPending ? (
        <Skeleton className="h-96 w-full" />
      ) : stats.isError || !stats.data ? (
        <ErrorState error={stats.error} retry={() => void stats.refetch()} />
      ) : stats.data.totalSeconds === 0 && stats.data.shows === 0 ? (
        <section className="surface p-6 text-center">
          <p className="font-medium text-base-100">Nothing watched yet.</p>
          <p className="mt-1 text-sm text-base-400">Your hours, genres and studios show up here as you watch.</p>
        </section>
      ) : (
        <Body data={stats.data} />
      )}
    </div>
  )
}

function Body({ data }: { data: StatsData }) {
  return (
    <>
      <section aria-label="Totals" className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <Tile label="Hours watched" value={hours(data.totalSeconds)} />
        <Tile label="Episodes finished" value={data.episodes.toLocaleString()} />
        <Tile label="Shows completed" value={data.completed.toLocaleString()} />
        <Tile label="Shows watched" value={data.shows.toLocaleString()} />
      </section>
      <Months months={data.months} />
      <div className="grid gap-4 sm:gap-5 md:grid-cols-2">
        <Ranked title="Genres" items={data.genres} />
        <Ranked title="Studios" items={data.studios} />
        <Ranked title="Seasons" items={data.seasons.map((s) => ({ ...s, name: seasonName(s.name) }))} />
      </div>
    </>
  )
}

function Tile({ label, value }: { label: string; value: string }) {
  return (
    <div className="surface p-4">
      <p className="text-[13px] text-base-400">{label}</p>
      <p className="mt-1 font-display text-2xl font-semibold text-white tabular-nums sm:text-3xl">{value}</p>
    </div>
  )
}

function Months({ months }: { months: StatsData['months'] }) {
  const peak = months.reduce((best, m) => (m.seconds > best.seconds ? m : best), months[0] ?? { month: '', seconds: 0 })
  const top = Math.max(1, peak?.seconds ?? 0)
  const now = months[months.length - 1]
  return (
    <section className="surface p-4 sm:p-5" aria-labelledby="stats-months">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h2 id="stats-months" className="text-base font-semibold text-white">
          The last 12 months
        </h2>
        <p className="text-sm text-base-400 tabular-nums">
          This month {hours(now?.seconds ?? 0)}
          {peak && peak.seconds > 0 && peak.month !== now?.month && ` · most in ${monthName(peak.month, false)}, ${hours(peak.seconds)}`}
        </p>
      </div>
      <ol className="mt-4 flex h-36 items-end gap-1.5 sm:gap-2" aria-label="Hours watched each month">
        {months.map((m) => (
          <li key={m.month} className="flex h-full min-w-0 flex-1 flex-col justify-end gap-1.5">
            <span className="sr-only">
              {monthName(m.month, false)}: {hours(m.seconds)}
            </span>
            <span
              aria-hidden
              className={m.seconds === 0 ? 'h-1 rounded-full bg-base-800' : 'rounded-t-[4px] bg-accent-500'}
              style={m.seconds === 0 ? undefined : { height: `${Math.max(4, (m.seconds / top) * 100)}%` }}
            />
            <span aria-hidden className="text-center text-[11px] text-base-500">
              {monthName(m.month, true)}
            </span>
          </li>
        ))}
      </ol>
    </section>
  )
}

function Ranked({ title, items }: { title: string; items: Taste[] }) {
  if (items.length === 0) return null
  const top = Math.max(1, items[0].seconds)
  return (
    <section className="surface p-4 sm:p-5" aria-labelledby={`stats-${title}`}>
      <h2 id={`stats-${title}`} className="text-base font-semibold text-white">
        {title}
      </h2>
      <ol className="mt-3 space-y-3">
        {items.map((t) => (
          <li key={t.name}>
            <div className="flex items-baseline justify-between gap-3 text-sm">
              <span className="min-w-0 truncate text-base-100">{t.name}</span>
              <span className="shrink-0 text-base-400 tabular-nums">
                {hours(t.seconds)} · {t.shows === 1 ? '1 show' : `${t.shows} shows`}
              </span>
            </div>
            <div aria-hidden className="mt-1.5 h-1.5 rounded-full bg-base-800">
              <div className="h-full rounded-full bg-accent-500" style={{ width: `${Math.max(3, (t.seconds / top) * 100)}%` }} />
            </div>
          </li>
        ))}
      </ol>
    </section>
  )
}
