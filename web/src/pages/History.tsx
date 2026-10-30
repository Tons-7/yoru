import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { api, beacon } from '../lib/api'
import { clockTime, cx, groupByDay } from '../lib/format'
import {
  Button,
  ConfirmInline,
  Empty,
  ErrorState,
  LinkButton,
  PageHeader,
  Pager,
  ProgressBar,
  RefreshFailed,
  Skeleton,
} from '../components/ui'
import { useClampPage } from '../lib/hooks'
import { toast, TOAST_MS } from '../lib/toast'

interface HistoryEntry {
  animeId: number
  title: string
  cover?: string
  thumb?: string
  epKey: string
  episode: number
  position: number
  duration?: number
  percent: number
  watched: boolean
  dismissed: boolean
  playCount: number
  lastPlayed: number
}

interface HistoryPage {
  items: HistoryEntry[]
  total: number
  page: number
  hasMore: boolean
}

const FORGET_REFRESHES = ['history', 'continue', 'watch-stats', 'episodes', 'anime', 'upnext']

const pendingForgets = new Map<string, { timer: number; body: { animeId: number; epKey: string } }>()
window.addEventListener('pagehide', () => {
  for (const [key, { timer, body }] of [...pendingForgets]) {
    window.clearTimeout(timer)
    pendingForgets.delete(key)
    beacon('/api/history/forget', body)
  }
})

export function History() {
  const [params, setParams] = useSearchParams()
  const page = Number(params.get('page') ?? 1)
  const qc = useQueryClient()
  const [confirmClear, setConfirmClear] = useState(false)

  const { data, isPending, isError, error, refetch } = useQuery({
    queryKey: ['history', page],
    queryFn: () => api.get<HistoryPage>(`/api/history?page=${page}&perPage=40`),
  })

  const refresh = () => {
    for (const key of FORGET_REFRESHES) void qc.invalidateQueries({ queryKey: [key] })
  }
  const forget = useMutation({
    mutationFn: (body: Record<string, unknown>) => api.post('/api/history/forget', body),
    onSuccess: () => {
      setConfirmClear(false)
      refresh()
    },
  })

  const [hidden, setHidden] = useState<Set<string>>(() => new Set())
  const forgetSoon = (entry: HistoryEntry) => {
    const key = `${entry.animeId}-${entry.epKey}`
    const unhide = () =>
      setHidden((h) => {
        const next = new Set(h)
        next.delete(key)
        return next
      })
    const body = { animeId: entry.animeId, epKey: entry.epKey }
    const send = () => {
      pendingForgets.delete(key)
      api.post('/api/history/forget', body).then(refresh, () => {
        unhide()
        toast(`Couldn't remove episode ${entry.episode} of ${entry.title} from history`)
      })
    }
    setHidden((h) => new Set(h).add(key))
    pendingForgets.set(key, { timer: window.setTimeout(send, TOAST_MS), body })
    toast(`Removed episode ${entry.episode} of ${entry.title}`, 'info', {
      label: 'Undo',
      run: () => {
        window.clearTimeout(pendingForgets.get(key)?.timer)
        pendingForgets.delete(key)
        unhide()
      },
    })
  }
  const items = (data?.items ?? []).filter((e) => !hidden.has(`${e.animeId}-${e.epKey}`))
  const goTo = (n: number) => setParams({ page: String(n) })
  useClampPage(data?.items.length === 0, page, data?.total, 40, (n) => setParams({ page: String(n) }, { replace: true }))

  return (
    <div className="space-y-5">
      <PageHeader
        title="Watch history"
        meta={data?.total ? `${data.total} episodes` : undefined}
        actions={
          items.length > 0 &&
          (confirmClear ? (
            <ConfirmInline
              note="Also clears resume points and episode ticks; AniList and MAL keep your progress."
              label="Clear everything"
              onConfirm={() => forget.mutate({ all: true })}
              onCancel={() => setConfirmClear(false)}
              pending={forget.isPending}
            />
          ) : (
            <Button onClick={() => setConfirmClear(true)}>Clear all</Button>
          ))
        }
      />
      <Stats />

      {isError && data && <RefreshFailed retry={() => refetch()} />}
      {isError && !data ? (
        <ErrorState error={error} retry={() => refetch()} />
      ) : isPending ? (
        <div className="space-y-2">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="h-28 w-full" />
          ))}
        </div>
      ) : items.length === 0 ? (
        <Empty
          title="Nothing watched yet"
          hint="Episodes you play show up here, with where you left off."
          action={<LinkButton to="/recent">See what just aired</LinkButton>}
        />
      ) : (
        <>
          <div className="space-y-6">
            {groupByDay(items, (e) => e.lastPlayed).map(([label, group]) => (
              <section key={label}>
                <h2 className="mb-2 text-xs font-semibold tracking-wider text-base-500 uppercase">{label}</h2>
                <ul className="space-y-2">
                  {byShow(group).map((entries) => (
                    <ShowCard key={entries[0].animeId} entries={entries} onForget={forgetSoon} />
                  ))}
                </ul>
              </section>
            ))}
          </div>

          <Pager page={page} hasMore={!!data?.hasMore} onPage={goTo} />
        </>
      )}
    </div>
  )
}

interface WatchStats {
  weekSeconds: number
  monthSeconds: number
  totalSeconds: number
  episodes: number
  completed: number
  days: Array<{ day: string; seconds: number }>
}

function hours(seconds: number) {
  const h = seconds / 3600
  return h >= 10 ? `${Math.round(h)} h` : h >= 1 ? `${h.toFixed(1)} h` : `${Math.round(seconds / 60)} min`
}

function Stats() {
  const { data } = useQuery({
    queryKey: ['watch-stats'],
    queryFn: () => api.get<WatchStats>('/api/history/stats'),
    staleTime: 60_000,
  })
  if (!data || data.totalSeconds === 0) return null
  const peak = Math.max(1, ...data.days.map((d) => d.seconds))

  return (
    <section className="surface p-4" aria-label="Watch stats">
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
        <Stat label="This week" value={hours(data.weekSeconds)} />
        <Stat label="This month" value={hours(data.monthSeconds)} />
        <Stat label="All time" value={hours(data.totalSeconds)} />
        <Stat label="Episodes finished" value={String(data.episodes)} />
        <Stat label="Shows completed" value={String(data.completed)} />
      </div>
      <div className="mt-4 flex h-10 items-end gap-0.5">
        {data.days.map((d, i) => (
          <div
            key={d.day}
            title={`${d.day}: ${hours(d.seconds)}`}
            className={cx(
              'flex-1 rounded-sm',
              d.seconds === 0 ? 'bg-base-800' : i === data.days.length - 1 ? 'bg-accent-400' : 'bg-accent-500/70',
            )}
            style={{ height: `${Math.max(8, (d.seconds / peak) * 100)}%` }}
          />
        ))}
      </div>
      <div className="mt-1 flex justify-between text-[11px] text-base-600">
        <span>30 days ago</span>
        <span>Today</span>
      </div>
    </section>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-[11px] tracking-wide text-base-500 uppercase">{label}</p>
      <p className="text-lg font-semibold text-white tabular-nums">{value}</p>
    </div>
  )
}

function byShow(group: HistoryEntry[]) {
  const shows = new Map<number, HistoryEntry[]>()
  for (const e of group) {
    let list = shows.get(e.animeId)
    if (!list) shows.set(e.animeId, (list = []))
    list.push(e)
  }
  return [...shows.values()]
}

function ShowCard({ entries, onForget }: { entries: HistoryEntry[]; onForget: (entry: HistoryEntry) => void }) {
  const latest = entries[0]
  const art = latest.thumb ?? latest.cover
  const episodes = [...entries].sort((a, b) => b.episode - a.episode)

  return (
    <li className="surface flex gap-3 p-3">
      <Link to={`/anime/${latest.animeId}`} tabIndex={-1} aria-hidden className="shrink-0 self-start">
        {art ? (
          <img src={art} alt="" loading="lazy" className="aspect-[2/3] w-12 rounded-md object-cover sm:w-14" />
        ) : (
          <div className="aspect-[2/3] w-12 rounded-md bg-base-850 sm:w-14" />
        )}
      </Link>

      <div className="min-w-0 flex-1">
        <Link
          to={`/anime/${latest.animeId}`}
          className="line-clamp-2 pt-0.5 text-sm font-medium text-base-100 hover:text-white"
        >
          {latest.title}
        </Link>
        <ul className="mt-1">
          {episodes.map((e) => (
            <EpisodeLine key={e.epKey} entry={e} onForget={() => onForget(e)} />
          ))}
        </ul>
      </div>
    </li>
  )
}

function EpisodeLine({ entry, onForget }: { entry: HistoryEntry; onForget: () => void }) {
  const left = entry.duration ? entry.duration - entry.position : 0

  return (
    <li className="flex items-center gap-1 border-t border-base-800/60 first:border-t-0">
      <Link
        to={`/watch/${entry.animeId}/${entry.episode}`}
        className="group/line flex min-h-10 min-w-0 flex-1 items-center gap-2.5 text-xs pointer-coarse:min-h-11"
      >
        <svg
          viewBox="0 0 24 24"
          className={cx(
            'size-3.5 shrink-0 group-hover/line:text-accent-300',
            entry.watched ? 'text-base-500' : 'text-accent-400',
          )}
          aria-hidden
        >
          <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />
        </svg>
        <span className="shrink-0 font-medium text-base-200 group-hover/line:text-white">Episode {entry.episode}</span>
        {entry.watched ? (
          <span className="flex min-w-0 items-center gap-1 text-emerald-400">
            <svg viewBox="0 0 24 24" className="size-3.5 shrink-0" aria-hidden>
              <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            <span className="truncate">Finished{entry.playCount > 1 && ` · ${entry.playCount}×`}</span>
          </span>
        ) : entry.position > 0 ? (
          <span className="flex min-w-0 items-center gap-2 text-accent-300">
            <span className="hidden w-20 shrink-0 sm:block">
              <ProgressBar value={entry.percent} />
            </span>
            <span className="truncate">
              {left >= 60 ? `Resume · ${Math.round(left / 60)} min left` : `Resume at ${clockTime(entry.position)}`}
              {entry.dismissed && <span className="text-base-500"> · removed from Continue watching</span>}
            </span>
          </span>
        ) : (
          <span className="truncate text-base-500">Not started</span>
        )}
        <span className="ml-auto shrink-0 text-base-500 tabular-nums">
          {new Date(entry.lastPlayed * 1000).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })}
        </span>
      </Link>
      <button
        onClick={onForget}
        aria-label={`Remove episode ${entry.episode} from history`}
        className="tap relative grid size-8 shrink-0 place-items-center rounded-md text-base-600 transition-colors hover:bg-base-800 hover:text-base-200"
      >
        <svg viewBox="0 0 24 24" className="size-3.5" aria-hidden>
          <path d="M6 6l12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
        </svg>
      </button>
    </li>
  )
}
