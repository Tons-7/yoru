import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import type { ScheduleItem } from '../lib/api'
import { airTime, cx, dayDate, dayName, relativeTime } from '../lib/format'
import { useMediaQuery } from '../lib/hooks'
import { useAlertsOff, useNow, usePrefFlag, useSchedule, useSetNotify } from '../lib/queries'
import { Badge, Empty, ErrorState, LabelledSwitch, LinkButton, PageHeader, RefreshFailed, Segmented, Skeleton } from '../components/ui'
import { toast } from '../lib/toast'
import { RecentPage } from './Recent'

export function SchedulePage() {
  const wide = useMediaQuery('(min-width: 40rem)')
  const [params, setParams] = useSearchParams()
  if (wide) return <Airing />
  const recent = params.get('view') === 'recent'
  return (
    <div className="space-y-4">
      <Segmented
        options={[
          { value: 'airing', label: 'Airing' },
          { value: 'recent', label: 'Recently released' },
        ]}
        value={recent ? 'recent' : 'airing'}
        onChange={(v) => setParams(v === 'recent' ? { view: 'recent' } : {}, { replace: true })}
      />
      {recent ? <RecentPage /> : <Airing />}
    </div>
  )
}

function Airing() {
  const [mine, setMine, ready] = usePrefFlag('schedule.mine')
  const alertsOff = useAlertsOff()
  const { data, isPending, isError, error, refetch } = useSchedule(7, mine, 1, ready)

  const days = data?.days ?? []
  const todayIndex = Math.max(0, days.findIndex((d) => d.today))
  const [selectedDate, setSelectedDate] = useState<string | null>(null)
  const picked = days.findIndex((d) => d.date === selectedDate)
  const shownIndex = picked >= 0 ? picked : todayIndex
  const active = days[shownIndex]

  const now = useNow() / 1000
  const ahead = days.flatMap((d) => d.items).filter((item) => item.airingAt > now)
  const nextUp = ahead[0]
  const nowLine = active?.today && !!nextUp && active.items.includes(nextUp) && active.items[0] !== nextUp
  const seen = new Set<number>()
  const alerted = ahead.filter((item) => item.alerts && !seen.has(item.animeId) && seen.add(item.animeId))

  const upcoming = useRef<HTMLLIElement>(null)
  useEffect(() => {
    const el = upcoming.current
    if (!el) return
    const box = el.getBoundingClientRect()
    if (box.top >= 0 && box.bottom <= window.innerHeight) return
    el.scrollIntoView({ block: 'center', behavior: 'auto' })
  }, [active?.date])

  return (
    <div className="space-y-5">
      <PageHeader
        title="Schedule"
        subtitle="When new episodes air, in your local time."
        actions={<LabelledSwitch label="Only my list" on={mine} onChange={setMine} />}
      />

      {isError && data && <RefreshFailed retry={() => refetch()} />}
      {isError && !data ? (
        <ErrorState error={error} retry={() => refetch()} />
      ) : isPending ? (
        <Skeleton className="h-96 w-full" />
      ) : (
        <>
          {/* Edge to edge when narrow: a scrolling box clips the outlines drawn outside it. */}
          <div className="no-scrollbar -mx-4 flex snap-x scroll-px-4 gap-2 overflow-x-auto px-4 py-1 sm:-mx-6 sm:scroll-px-6 sm:px-6 lg:mx-0 lg:grid lg:grid-cols-8 lg:overflow-visible lg:px-0">
            {days.map((day, i) => {
              const isActive = i === shownIndex
              const name = dayName(day.date, day.weekday, days[todayIndex]?.date)
              return (
                <button
                  key={day.date}
                  aria-pressed={isActive}
                  aria-label={`${name}, ${dayDate(day.date)}, ${day.items.length} episodes`}
                  onClick={() => setSelectedDate(day.date)}
                  className={cx(
                    'min-w-[6.75rem] shrink-0 snap-start rounded-xl px-3.5 py-2.5 text-left ring-1 transition-colors lg:min-w-0',
                    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-300',
                    isActive
                      ? 'bg-accent-500/15 ring-accent-400/60'
                      : 'bg-base-900/60 ring-white/[0.06] hover:bg-base-850 hover:ring-white/10',
                  )}
                >
                  <span
                    className={cx(
                      'flex items-center gap-1.5 text-sm font-semibold whitespace-nowrap',
                      isActive ? 'text-accent-300' : 'text-base-100',
                    )}
                  >
                    {day.today && <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-accent-400" />}
                    {name}
                  </span>
                  <span className="mt-0.5 block text-xs whitespace-nowrap tabular-nums text-base-500">
                    {dayDate(day.date)} · {day.items.length} ep
                  </span>
                </button>
              )
            })}
          </div>

          {alertsOff && (
            <p className="surface flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 text-sm text-base-300">
              Alerts are off for every show, so the bells can't be set.
              <LinkButton to="/settings?tab=Notifications" size="sm">
                Turn alerts on
              </LinkButton>
            </p>
          )}

          <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_20rem]">
          {!active || active.items.length === 0 ? (
            <Empty
              title="Nothing airing this day"
              hint={mine ? 'Nothing from your list, at least.' : undefined}
            />
          ) : (
            <ul className="surface divide-y divide-white/[0.05] overflow-hidden">
              {active.items.map((item) => {
                const aired = item.airingAt <= now
                const isNext = item === nextUp
                const badges = (
                  <>
                    {isNext && <Badge tone="accent">Next</Badge>}
                    {item.out && <Badge tone="accent">Out</Badge>}
                    {item.delayedFrom ? <Badge tone="warning">Delayed</Badge> : null}
                    {item.watched && <Badge tone="success">Watched</Badge>}
                    {item.behind > 0 && <Badge tone="accent">{item.behind} behind</Badge>}
                    {item.onList && !item.watched && item.behind === 0 && <Badge>On list</Badge>}
                  </>
                )

                return (
                  <li
                    key={`${item.animeId}-${item.episode}`}
                    ref={isNext ? upcoming : undefined}
                    className="group/row relative"
                  >
                    {isNext && nowLine && (
                      <div
                        aria-hidden
                        className="flex items-center gap-2 px-3 py-1 text-[11px] font-medium text-accent-400"
                      >
                        <span className="h-px flex-1 bg-accent-500/50" />
                        Now · {airTime(now)}
                        <span className="h-px flex-1 bg-accent-500/50" />
                      </div>
                    )}
                    <Link
                      to={`/anime/${item.animeId}`}
                      className={cx(
                        'flex items-center gap-3 border-l-2 border-transparent py-2.5 pr-14 pl-3 transition-colors hover:bg-base-850/70',
                        isNext && 'border-accent-500 bg-accent-500/10',
                      )}
                    >
                      <div className="w-20 shrink-0 text-right max-sm:w-14">
                        <p className="text-sm font-medium whitespace-nowrap tabular-nums text-base-100">
                          {airTime(item.airingAt)}
                        </p>
                        <p
                          className={cx(
                            'text-[11px] whitespace-nowrap max-sm:hidden',
                            isNext ? 'text-accent-400' : 'text-base-500',
                          )}
                        >
                          {relativeTime(item.airingAt)}
                        </p>
                      </div>

                      {item.thumb ?? item.cover ? (
                        <img
                          src={item.thumb ?? item.cover}
                          alt=""
                          loading="lazy"
                          className={cx('h-14 w-10 shrink-0 rounded object-cover shadow-card', aired && 'brightness-75')}
                        />
                      ) : (
                        <div className="h-14 w-10 shrink-0 rounded bg-base-850" />
                      )}

                      <div className="min-w-0 flex-1">
                        <p className={cx('line-clamp-1 text-sm font-medium', aired ? 'text-base-300' : 'text-base-100')}>
                          {item.title}
                        </p>
                        <p className="mt-0.5 text-xs text-base-500">
                          Episode {item.episode}
                          {item.jstWeekday !== active.weekday && ` · ${item.jstWeekday} JST`}
                          {item.delayedFrom
                            ? ` · was due ${new Date(item.delayedFrom * 1000).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}`
                            : ''}
                        </p>
                        <div className="mt-1 flex flex-wrap items-center gap-1.5 sm:hidden">{badges}</div>
                      </div>

                      <div className="flex shrink-0 items-center gap-1.5 max-sm:hidden">{badges}</div>
                    </Link>
                    <div className="absolute top-1/2 right-3 -translate-y-1/2">
                      {aired && !item.watched ? (
                        <Link
                          to={`/watch/${item.animeId}/${item.episode}`}
                          aria-label={`Play episode ${item.episode} of ${item.title}`}
                          className="tap relative grid size-8 place-items-center rounded-full bg-accent-500 text-white shadow-lg transition-colors hover:bg-accent-400"
                        >
                          <svg viewBox="0 0 24 24" className="size-3.5" aria-hidden>
                            <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />
                          </svg>
                        </Link>
                      ) : (
                        <AlertBell item={item} />
                      )}
                    </div>
                  </li>
                )
              })}
            </ul>
          )}
            <aside className="space-y-4 max-xl:hidden">
              {ahead[0] && <AirsNext key={ahead[0].animeId} item={ahead[0]} />}
              <section className="surface p-4">
                <h2 className="text-sm font-semibold text-white">Your alerts this week</h2>
                {alertsOff ? (
                  <p className="mt-1 text-sm text-base-500">Alerts are off for every show in Settings.</p>
                ) : alerted.length === 0 ? (
                  <p className="mt-1 text-sm text-base-500">None yet. Tap a bell to hear when an episode is out.</p>
                ) : (
                  <ul className="mt-2 space-y-1">
                    {alerted.slice(0, 8).map((item) => (
                      <li key={item.animeId}>
                        <Link to={`/anime/${item.animeId}`} className="-mx-2 flex items-center gap-3 rounded-lg px-2 py-1.5 hover:bg-base-850">
                          {item.thumb ?? item.cover ? (
                            <img src={item.thumb ?? item.cover} alt="" loading="lazy" className="h-10 w-7 shrink-0 rounded object-cover" />
                          ) : (
                            <div className="h-10 w-7 shrink-0 rounded bg-base-850" />
                          )}
                          <span className="min-w-0">
                            <span className="line-clamp-1 block text-sm text-base-100">{item.title}</span>
                            <span className="block text-xs text-base-500 tabular-nums">
                              {new Date(item.airingAt * 1000).toLocaleDateString(undefined, { weekday: 'short' })} {airTime(item.airingAt)} ·
                              Ep {item.episode}
                            </span>
                          </span>
                        </Link>
                      </li>
                    ))}
                  </ul>
                )}
              </section>
            </aside>
          </div>
        </>
      )}
    </div>
  )
}

function useAlertToggle(item: ScheduleItem) {
  const set = useSetNotify(item.animeId)
  const off = useAlertsOff()
  const change = (on: boolean) =>
    set.mutate(on, {
      onError: (e) =>
        toast(`Couldn't ${on ? 'turn on' : 'turn off'} alerts for ${item.title}: ${e.message}`, 'error', {
          label: 'Try again',
          run: () => change(on),
        }),
    })
  return { on: item.alerts, off, toggle: () => change(!item.alerts), pending: set.isPending }
}

function AlertBell({ item }: { item: ScheduleItem }) {
  const { on, off, toggle, pending } = useAlertToggle(item)
  return (
    <button
      type="button"
      onClick={toggle}
      disabled={pending || off}
      aria-pressed={on}
      aria-label={off ? `Alerts are off in Settings` : on ? `Alerts on for ${item.title}` : `Alert me about ${item.title}`}
      title={off ? 'Alerts are off for every show in Settings' : on ? 'Alerts on; tap to stop them' : 'Alert me when new episodes are out'}
      className={cx(
        'tap relative grid size-8 place-items-center rounded-full transition-colors disabled:opacity-40',
        on ? 'bg-accent-500/20 text-accent-300 hover:bg-accent-500/30' : 'text-base-400 ring-1 ring-white/10 hover:bg-base-800 hover:text-white',
      )}
    >
      <BellGlyph filled={on} />
    </button>
  )
}

function AirsNext({ item }: { item: ScheduleItem }) {
  const { on, off, toggle, pending } = useAlertToggle(item)
  const art = item.show?.banner ?? item.coverLarge ?? item.cover
  return (
    <section className="surface overflow-hidden">
      <div className="relative aspect-[16/9] bg-base-850">
        {art && <img src={art} alt="" className="size-full object-cover object-top" />}
        <div className="absolute inset-0 bg-gradient-to-t from-base-950/90 to-transparent" />
        <p className="absolute top-3 left-3 rounded-md bg-base-950/70 px-2 py-0.5 text-[11px] font-semibold tracking-wide text-accent-300 uppercase">
          Airs next
        </p>
      </div>
      <div className="p-4">
        <Link to={`/anime/${item.animeId}`} className="line-clamp-2 font-medium text-white hover:text-accent-200">
          {item.title}
        </Link>
        <p className="mt-1 text-sm text-base-400 tabular-nums">
          Episode {item.episode} · {airTime(item.airingAt)} · {relativeTime(item.airingAt)}
        </p>
        {off ? (
          <p className="mt-3 text-sm text-base-500">Alerts are off for every show in Settings.</p>
        ) : (
          <button
            type="button"
            onClick={toggle}
            disabled={pending}
            aria-pressed={on}
            className={cx(
              'mt-3 flex items-center gap-2 rounded-lg px-3 py-1.5 text-sm font-medium transition-colors pointer-coarse:min-h-11',
              on ? 'bg-accent-500/20 text-accent-300 hover:bg-accent-500/30' : 'bg-base-800 text-base-100 hover:bg-base-700',
            )}
          >
            <BellGlyph filled={on} />
            {on ? 'Alerts on' : 'Alert me'}
          </button>
        )}
      </div>
    </section>
  )
}

function BellGlyph({ filled }: { filled: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path
        d="M6 16V11a6 6 0 1 1 12 0v5l1.5 2h-15L6 16Zm4 3.5a2 2 0 0 0 4 0"
        fill={filled ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
