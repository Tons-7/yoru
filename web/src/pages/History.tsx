import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { api, beacon } from '../lib/api'
import { clockTime, cx, groupByDay, relativeTime } from '../lib/format'
import {
  Badge,
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

// The show page's resume button reads history too.
const FORGET_REFRESHES = ['history', 'continue', 'watch-stats', 'episodes', 'anime', 'upnext']

// Outside the page: its toast and Undo stay up after leaving it, so the wait does too.
const pendingForgets = new Map<string, { timer: number; body: { animeId: number; epKey: string } }>()
// Closing the tab inside the Undo window still forgets: a beacon outlives the page, a fetch does not.
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

  // A removed row goes at once but is only forgotten when its Undo runs out.
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
  // Removing the last entry of a later page would strand the viewer on an empty "nothing watched".
  useClampPage(data?.items.length === 0, page, data?.total, 40, (n) => setParams({ page: String(n) }, { replace: true }))

  return (
    <div className="space-y-5">
      <PageHeader
        title="Watch history"
        meta={data?.total ? `${data.total} episodes` : undefined}
        actions={
          items.length > 0 &&
          (confirmClear ? (
            // What goes, said before it goes: there is no undo for all of it.
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

      {/* A failed refresh keeps the list it has, as the other pages do. */}
      {isError && data && <RefreshFailed retry={() => refetch()} />}
      {isError && !data ? (
        <ErrorState error={error} retry={() => refetch()} />
      ) : isPending ? (
        <div className="space-y-2">
          {Array.from({ length: 8 }, (_, i) => (
            <Skeleton key={i} className="h-20 w-full" />
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
          {/* A timeline: when you watched is half of what history means. */}
          <div className="space-y-6">
            {groupByDay(items, (e) => e.lastPlayed).map(([label, group]) => (
              <section key={label}>
                <h2 className="mb-2 text-xs font-semibold tracking-wider text-base-500 uppercase">{label}</h2>
                <ul className="space-y-1.5">
                  {group.map((entry) => (
                    <Row
                      key={`${entry.animeId}-${entry.epKey}`}
                      entry={entry}
                      onForget={() => forgetSoon(entry)}
                    />
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

// Time actually played, by window, with the last thirty days as a strip.
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
        <Stat label="Episodes" value={String(data.episodes)} />
        <Stat label="Completed" value={String(data.completed)} />
      </div>
      <div className="mt-3 flex h-10 items-end gap-0.5" title="Last 30 days">
        {data.days.map((d) => (
          <div
            key={d.day}
            title={`${d.day}: ${hours(d.seconds)}`}
            className={cx('flex-1 rounded-sm', d.seconds > 0 ? 'bg-accent-500/70' : 'bg-base-800')}
            style={{ height: `${Math.max(8, (d.seconds / peak) * 100)}%` }}
          />
        ))}
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

function Row({ entry, onForget }: { entry: HistoryEntry; onForget: () => void }) {
  const art = entry.thumb ?? entry.cover

  return (
    <li className="group surface flex items-center gap-3 p-2 transition-colors hover:bg-base-850/80">
      <Link to={`/anime/${entry.animeId}`} className="shrink-0">
        {art ? (
          <img src={art} alt="" loading="lazy" className="h-16 w-11 rounded object-cover" />
        ) : (
          <div className="h-16 w-11 rounded bg-base-850" />
        )}
      </Link>

      <div className="min-w-0 flex-1">
        <Link
          to={`/anime/${entry.animeId}`}
          className="line-clamp-1 text-sm text-base-100 hover:text-white"
        >
          {entry.title}
        </Link>
        <div className="mt-0.5 flex flex-wrap items-center gap-1.5 text-xs text-base-500">
          <span>Episode {entry.episode}</span>
          <span>·</span>
          <span>{relativeTime(entry.lastPlayed)}</span>
          {entry.playCount > 1 && <Badge>Watched {entry.playCount}×</Badge>}
          {entry.watched ? (
            <Badge tone="accent">Finished</Badge>
          ) : (
            <span>{clockTime(entry.position)}</span>
          )}
          {entry.dismissed && <Badge>Dismissed</Badge>}
        </div>
        {!entry.watched && entry.percent > 0 && (
          <div className="mt-1.5 max-w-xs">
            <ProgressBar value={entry.percent} />
          </div>
        )}
      </div>

      <LinkButton
        to={`/watch/${entry.animeId}/${entry.episode}`}
        size="sm"
        variant={entry.watched ? 'secondary' : 'primary'}
      >
        {entry.watched ? 'Watch again' : 'Resume'}
      </LinkButton>
      <button
        onClick={onForget}
        aria-label="Remove from history"
        className="tap relative shrink-0 rounded-md px-2 py-1.5 text-base-600 transition-colors hover:bg-base-800 hover:text-base-200"
      >
        ✕
      </button>
    </li>
  )
}
