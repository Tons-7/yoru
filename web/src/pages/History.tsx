import { useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, Navigate, useLocation, useSearchParams } from 'react-router-dom'
import { api, beacon } from '../lib/api'
import { cx, groupByDay } from '../lib/format'
import { Empty, ErrorState, LinkButton, Pager, RefreshFailed, Skeleton } from '../components/ui'
import { SwitchHeader } from '../components/ViewSwitch'
import { MoreMenu } from '../components/MoreMenu'
import { PlayIcon } from '../components/PosterCard'
import { useClampPage, useDismiss, useModalFocus } from '../lib/hooks'
import { historyUpNext, outDay, timeLeft, type UpNext } from '../lib/libraryText'
import { refreshListState, useNow } from '../lib/queries'
import { episodeCount } from '../lib/shelf'
import { toast, TOAST_MS } from '../lib/toast'
import { buttonClass } from '../lib/ui'

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
  finished: boolean
  dismissed: boolean
  playCount: number
  lastPlayed: number
  progress: number
  listStatus?: string
  episodes?: number
  airing?: string
  nextEpisode?: number
  nextAiringAt?: number
}

interface HistoryPage {
  items: HistoryEntry[]
  total: number
  page: number
  hasMore: boolean
}

const PER_PAGE = 40
const FORGET_REFRESHES = ['history', 'continue', 'watch-stats', 'episodes', 'anime', 'upnext']

const pendingForgets = new Map<string, { timer: number; body: { animeId: number; epKey?: string } }>()
window.addEventListener('pagehide', () => {
  for (const [key, { timer, body }] of [...pendingForgets]) {
    window.clearTimeout(timer)
    pendingForgets.delete(key)
    beacon('/api/history/forget', body)
  }
})

const entryKey = (e: HistoryEntry) => `${e.animeId}-${e.epKey}`
const showKey = (animeId: number) => `${animeId}-*`

export function History() {
  const { search } = useLocation()
  const params = new URLSearchParams(search)
  params.set('view', 'history')
  return <Navigate to={`/library?${params}`} replace />
}

export function HistoryView({ switcher }: { switcher: ReactNode }) {
  const [params, setParams] = useSearchParams()
  const page = Number(params.get('page') ?? 1)
  const qc = useQueryClient()
  const [confirmClear, setConfirmClear] = useState(false)

  const { data, isPending, isError, error, refetch } = useQuery({
    queryKey: ['history', page],
    queryFn: () => api.get<HistoryPage>(`/api/history?page=${page}&perPage=${PER_PAGE}`),
  })

  const refresh = () => {
    for (const key of FORGET_REFRESHES) void qc.invalidateQueries({ queryKey: [key] })
  }

  const [hidden, setHidden] = useState<Set<string>>(() => new Set())
  const forgetSoon = (key: string, body: { animeId: number; epKey?: string }, what: string) => {
    const unhide = () =>
      setHidden((h) => {
        const next = new Set(h)
        next.delete(key)
        return next
      })
    const send = () => {
      pendingForgets.delete(key)
      api.post('/api/history/forget', body).then(refresh, () => {
        unhide()
        toast(`Couldn't remove ${what} from history`)
      })
    }
    setHidden((h) => new Set(h).add(key))
    pendingForgets.set(key, { timer: window.setTimeout(send, TOAST_MS), body })
    toast(`Removed ${what}`, 'info', {
      label: 'Undo',
      run: () => {
        window.clearTimeout(pendingForgets.get(key)?.timer)
        pendingForgets.delete(key)
        unhide()
      },
    })
  }
  const forgetEpisode = (e: HistoryEntry) =>
    forgetSoon(entryKey(e), { animeId: e.animeId, epKey: e.epKey }, `episode ${e.episode} of ${e.title}`)
  const forgetShow = (e: HistoryEntry) => forgetSoon(showKey(e.animeId), { animeId: e.animeId }, `${e.title} from history`)

  const items = (data?.items ?? []).filter((e) => !hidden.has(entryKey(e)) && !hidden.has(showKey(e.animeId)))
  const goTo = (n: number, replace = false) => {
    const next = new URLSearchParams(params)
    next.set('page', String(n))
    setParams(next, { replace })
  }
  useClampPage(data?.items.length === 0, page, data?.total, PER_PAGE, (n) => goTo(n, true))

  const days = groupByDay(items, (e) => e.lastPlayed)
  const byAnime = new Map<number, HistoryEntry[]>()
  for (const e of items) byAnime.set(e.animeId, [...(byAnime.get(e.animeId) ?? []), e])
  const led = new Set<number>()

  return (
    <div className="space-y-5">
      <SwitchHeader
        title="Library"
        meta={data?.total ? episodeCount(data.total) : undefined}
        switcher={switcher}
        actions={
          data && (
            <MoreMenu
              label="History options"
              className="size-11 rounded-full"
              items={[
                {
                  label: 'Clear watch history',
                  danger: true,
                  disabled: !data.total,
                  note: data.total ? undefined : 'Nothing to clear',
                  run: () => setConfirmClear(true),
                },
              ]}
            />
          )
        }
      />

      <div className="space-y-5 xl:grid xl:grid-cols-[minmax(0,1fr)_22rem] xl:items-start xl:gap-6 xl:space-y-0">
        <div className="xl:sticky xl:top-20 xl:order-2">
          <Stats />
        </div>

        <div className="min-w-0 space-y-5">
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
              action={<LinkButton to="/schedule?view=out">See what just aired</LinkButton>}
            />
          ) : (
            <>
              <div className="space-y-6">
                {days.map(([label, group]) => (
                  <section key={label}>
                    <h2 className="mb-2 text-[13px] font-semibold text-base-300">{label}</h2>
                    <ul className="space-y-2">
                      {byShow(group).map((entries) => {
                        const id = entries[0].animeId
                        const lead = !led.has(id)
                        led.add(id)
                        return (
                          <ShowGroup
                            key={id}
                            entries={entries}
                            upNext={lead ? historyUpNext(byAnime.get(id) ?? entries) : null}
                            onForget={forgetEpisode}
                            onForgetShow={forgetShow}
                          />
                        )
                      })}
                    </ul>
                  </section>
                ))}
              </div>

              <Pager page={page} hasMore={!!data?.hasMore} onPage={goTo} />
            </>
          )}
        </div>
      </div>

      {confirmClear && (
        <ClearDialog
          episodes={data?.total ?? 0}
          onClose={() => setConfirmClear(false)}
          onCleared={() => {
            setConfirmClear(false)
            refreshListState(qc)
            refresh()
          }}
        />
      )}
    </div>
  )
}

function ClearDialog({ episodes, onClose, onCleared }: { episodes: number; onClose: () => void; onCleared: () => void }) {
  const panel = useDismiss<HTMLDivElement>(onClose)
  useModalFocus(panel)
  const clear = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post('/api/history/forget', { all: true }),
    onSuccess: onCleared,
  })
  return createPortal(
    <div className="fixed inset-0 z-[60] flex items-end justify-center p-4 sm:items-center">
      <div aria-hidden className="absolute inset-0 animate-fade-in bg-black/70" />
      <div
        ref={panel}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="clear-history-title"
        aria-describedby="clear-history-what"
        className="relative w-full max-w-sm animate-rise rounded-card bg-base-750 p-5 shadow-panel"
      >
        <h2 id="clear-history-title" className="text-lg font-semibold text-white">
          Clear all watch history?
        </h2>
        <div id="clear-history-what" className="mt-2 space-y-2 text-sm text-base-300">
          <p>
            {episodeCount(episodes)} will be removed from History, with the place you stopped in each.
          </p>
          <p className="text-base-400">
            Your list progress here, on AniList and on MyAnimeList stays, and so does your watch time.
          </p>
        </div>
        {clear.isError && (
          <p className="mt-3 text-[13px] text-red-300">Couldn't clear it: {(clear.error as Error).message}</p>
        )}
        <div className="mt-5 flex flex-wrap justify-end gap-2">
          <button type="button" autoFocus onClick={onClose} className={buttonClass('secondary')}>
            Keep history
          </button>
          <button
            type="button"
            onClick={() => clear.mutate()}
            disabled={clear.isPending}
            className={buttonClass('danger')}
          >
            {clear.isPending ? 'Clearing…' : 'Clear history'}
          </button>
        </div>
      </div>
    </div>,
    document.body,
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
  const [open, setOpen] = useState(false)
  const { data } = useQuery({
    queryKey: ['watch-stats'],
    queryFn: () => api.get<WatchStats>('/api/history/stats'),
    staleTime: 60_000,
  })
  if (!data || data.totalSeconds === 0) return null
  const peak = Math.max(1, ...data.days.map((d) => d.seconds))

  return (
    <div className="space-y-2">
      <button
        type="button"
        aria-expanded={open}
        aria-controls="watch-stats"
        onClick={() => setOpen(!open)}
        className="surface flex min-h-14 w-full items-center gap-3 px-4 text-left text-sm font-medium text-base-100 transition-colors hover:bg-base-800 sm:hidden"
      >
        <span className="min-w-0 flex-1 tabular-nums">
          {hours(data.weekSeconds)} this week · {episodeCount(data.episodes)} finished
        </span>
        <svg
          viewBox="0 0 24 24"
          className={cx('size-4 shrink-0 text-base-400 transition-transform', open && 'rotate-90')}
          aria-hidden
        >
          <path d="m9 6 6 6-6 6" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      <section id="watch-stats" className={cx('surface p-4', !open && 'max-sm:hidden')} aria-label="Watch stats">
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-5 xl:grid-cols-2">
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
        <div className="mt-1 flex justify-between text-xs text-base-500">
          <span>30 days ago</span>
          <span>Today</span>
        </div>
      </section>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-[13px] text-base-400">{label}</p>
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

function ShowGroup({
  entries,
  upNext,
  onForget,
  onForgetShow,
}: {
  entries: HistoryEntry[]
  upNext: UpNext | null
  onForget: (entry: HistoryEntry) => void
  onForgetShow: (entry: HistoryEntry) => void
}) {
  const latest = entries[0]
  const art = latest.cover ?? latest.thumb
  const episodes = [...entries].sort((a, b) => b.episode - a.episode)
  const show = `/anime/${latest.animeId}`

  return (
    <li className="surface overflow-hidden">
      <div className="flex items-center gap-3 p-3">
        <Link to={show} tabIndex={-1} aria-hidden className="shrink-0">
          {art ? (
            <img src={art} alt="" loading="lazy" className="aspect-[2/3] w-12 rounded-md object-cover" />
          ) : (
            <div className="aspect-[2/3] w-12 rounded-md bg-base-850" />
          )}
        </Link>
        <div className="min-w-0 flex-1">
          <Link to={show} className="line-clamp-2 text-[15px] leading-5 font-semibold text-base-100 hover:text-white">
            {latest.title}
          </Link>
          {upNext && <UpNextLine upNext={upNext} total={latest.episodes} />}
        </div>
        {upNext && upNext.kind !== 'done' && <PlayNext upNext={upNext} entry={latest} />}
      </div>
      <ul>
        {episodes.map((e) => (
          <EpisodeRow
            key={e.epKey}
            entry={e}
            last={episodes.length === 1}
            onForget={() => onForget(e)}
            onForgetShow={() => onForgetShow(e)}
          />
        ))}
      </ul>
    </li>
  )
}

function UpNextLine({ upNext, total }: { upNext: UpNext; total?: number }) {
  const now = useNow(600_000) / 1000
  if (upNext.kind === 'done') {
    return <p className="mt-0.5 text-[13px] text-emerald-400">Done{total ? ` · ${total} eps` : ''}</p>
  }
  if (upNext.kind === 'waiting') {
    return (
      <p className="mt-0.5 text-[13px] text-base-400">
        Up next: Episode {upNext.episode} · {upNext.at ? `out ${outDay(upNext.at, now)}` : 'not out yet'}
      </p>
    )
  }
  return (
    <p className="mt-0.5 text-[13px] text-accent-300">
      Up next: Episode {upNext.episode}
      {upNext.resume && <span className="text-base-400"> · {timeLeft(upNext.resume.position, upNext.resume.duration)}</span>}
    </p>
  )
}

function PlayNext({ upNext, entry }: { upNext: Exclude<UpNext, { kind: 'done' }>; entry: HistoryEntry }) {
  const label = `Play episode ${upNext.episode} of ${entry.title}`
  const look = 'grid size-11 shrink-0 place-items-center rounded-full'
  if (upNext.kind === 'waiting') {
    return (
      <button
        type="button"
        disabled
        aria-label={`Episode ${upNext.episode} of ${entry.title} isn't out yet`}
        className={cx(look, 'bg-base-800 text-base-600')}
      >
        <PlayIcon />
      </button>
    )
  }
  return (
    <Link
      to={`/watch/${entry.animeId}/${upNext.episode}`}
      aria-label={label}
      title={label}
      className={cx(look, 'press bg-accent-500 text-white transition-colors hover:bg-accent-600')}
    >
      <PlayIcon />
    </Link>
  )
}

function EpisodeRow({
  entry,
  last,
  onForget,
  onForgetShow,
}: {
  entry: HistoryEntry
  last: boolean
  onForget: () => void
  onForgetShow: () => void
}) {
  const qc = useQueryClient()
  const done = entry.finished || (entry.position === 0 && entry.playCount > 0)
  const ticked = entry.progress >= entry.episode
  const mark = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post('/api/watched', { animeId: entry.animeId, episode: entry.episode, watched: true }),
    onSuccess: () => {
      refreshListState(qc)
      toast(`Marked episode ${entry.episode} of ${entry.title} watched`, 'info')
    },
    onError: (e: Error) => toast(`Couldn't mark episode ${entry.episode} of ${entry.title} watched: ${e.message}`),
  })
  const time = new Date(entry.lastPlayed * 1000).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })

  return (
    <li className="flex items-center border-t border-base-800/60">
      <Link
        to={`/watch/${entry.animeId}/${entry.episode}`}
        aria-label={`${done ? 'Play' : 'Resume'} episode ${entry.episode} of ${entry.title}`}
        className="group/row flex min-h-14 min-w-0 flex-1 flex-col justify-center gap-1.5 py-2 pr-1 pl-3 transition-colors hover:bg-base-850"
      >
        <span className="flex items-baseline gap-3">
          <span className="min-w-0 flex-1 truncate text-[15px] font-semibold text-base-100 group-hover/row:text-white">
            Episode {entry.episode}
          </span>
          <span className="shrink-0 text-[13px] text-base-500 tabular-nums">{time}</span>
        </span>
        <span className="flex items-center gap-2.5 text-[13px]">
          <span aria-hidden className="h-[3px] min-w-12 flex-1 overflow-hidden rounded-full bg-base-800">
            <span
              className={cx('block h-full rounded-full', done ? 'bg-emerald-400' : 'bg-accent-400')}
              style={{ width: `${done ? 100 : Math.min(100, Math.max(0, entry.percent))}%` }}
            />
          </span>
          {done ? (
            <span className="flex shrink-0 items-center gap-1 text-emerald-400">
              <svg viewBox="0 0 24 24" className="size-3.5" aria-hidden>
                <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              Watched{entry.playCount > 1 && ` · ${entry.playCount}×`}
            </span>
          ) : (
            <span className="shrink-0 text-base-300">{timeLeft(entry.position, entry.duration)}</span>
          )}
        </span>
      </Link>
      <MoreMenu
        label={`More for episode ${entry.episode} of ${entry.title}`}
        title={`Episode ${entry.episode}`}
        detail={entry.title}
        className="mr-1 size-11 rounded-full"
        items={[
          { label: 'Remove from history', danger: true, run: onForget },
          ...(last
            ? [{ label: 'Remove the whole show', note: 'Every episode of it in History', danger: true, run: onForgetShow }]
            : []),
          {
            label: 'Mark watched',
            disabled: ticked || mark.isPending,
            note: ticked ? 'Already counted on your list' : undefined,
            run: () => mark.mutate(),
            divider: true,
          },
          { label: 'Go to show', to: `/anime/${entry.animeId}` },
        ]}
      />
    </li>
  )
}
