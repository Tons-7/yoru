import { useEffect, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { airTime, cx, relativeTime } from '../lib/format'
import { useMediaQuery, useStoredFlag } from '../lib/hooks'
import { useNow, useSchedule } from '../lib/queries'
import { Badge, Empty, ErrorState, FilterToggle, PageHeader, RefreshFailed, Segmented, Skeleton } from '../components/ui'
import { RecentPage } from './Recent'

function dayLabel(date: string) {
  return new Date(`${date}T00:00`).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

// On a phone Recently released is a tab here: the tab bar has room for five places, not six.
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
  const [mine, setMine] = useStoredFlag('yoru.schedule.mine')
  const { data, isPending, isError, error, refetch } = useSchedule(7, mine)

  const days = data?.days ?? []
  const todayIndex = Math.max(0, days.findIndex((d) => d.today))
  // By date, not position: past midnight the days shift one along and an index lands on the next one.
  const [selectedDate, setSelectedDate] = useState<string | null>(null)
  const picked = days.findIndex((d) => d.date === selectedDate)
  const shownIndex = picked >= 0 ? picked : todayIndex
  const active = days[shownIndex]

  const now = useNow() / 1000
  const nextUp = active?.items.find((item) => item.airingAt > now)
  // The "now" line sits between what has aired and what has not.
  const nowLine = active?.today && nextUp && active.items[0] !== nextUp

  // Scroll to what airs next, keyed on the date so a refetch doesn't scroll under a reader.
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
        actions={
          <FilterToggle on={mine} onChange={setMine}>
            Only my list
          </FilterToggle>
        }
      />

      {isError && data && <RefreshFailed retry={() => refetch()} />}
      {/* Below the header, so the "Only my list" toggle stays reachable. */}
      {isError && !data ? (
        <ErrorState error={error} retry={() => refetch()} />
      ) : isPending ? (
        <Skeleton className="h-96 w-full" />
      ) : (
        <>
          <div className="no-scrollbar flex gap-1.5 overflow-x-auto pb-1">
            {days.map((day, i) => {
              const isActive = i === shownIndex
              return (
                <button
                  key={day.date}
                  aria-pressed={isActive}
                  onClick={() => setSelectedDate(day.date)}
                  className={cx(
                    'shrink-0 rounded-lg px-4 py-2 text-left ring-1 transition-colors',
                    isActive
                      ? 'bg-accent-500/15 ring-accent-500/40'
                      : 'bg-base-900/60 ring-white/[0.06] hover:bg-base-850',
                  )}
                >
                  <span
                    className={cx(
                      'block text-sm font-medium',
                      isActive ? 'text-accent-300' : 'text-base-200',
                    )}
                  >
                    {day.today ? 'Today' : day.weekday}
                  </span>
                  <span className="block text-[11px] text-base-500">
                    {dayLabel(day.date)} · {day.items.length} ep
                  </span>
                </button>
              )
            })}
          </div>

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
                        'flex items-center gap-3 border-l-2 px-3 py-2.5 transition-colors hover:bg-base-850/70',
                        // Aired rows dim only their art (below): faded text fell below readable contrast.
                        'border-transparent',
                        isNext && 'border-accent-500 bg-accent-500/10',
                        aired && !item.watched && 'pr-14',
                      )}
                    >
                      {/* Narrower on a phone, where the title needs the width. */}
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
                          {/* Late-night broadcasts are a different weekday in Japan, as groups label them. */}
                          {item.jstWeekday !== active.weekday && ` · ${item.jstWeekday} JST`}
                          {item.delayedFrom
                            ? ` · was due ${new Date(item.delayedFrom * 1000).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}`
                            : ''}
                        </p>
                        {/* On a phone the badges go under the title, which keeps the row's width. */}
                        <div className="mt-1 flex flex-wrap items-center gap-1.5 sm:hidden">{badges}</div>
                      </div>

                      <div className="flex shrink-0 items-center gap-1.5 max-sm:hidden">{badges}</div>
                    </Link>
                    {/* Out already: one click to watch, not two via the show. */}
                    {aired && !item.watched && (
                      <Link
                        to={`/watch/${item.animeId}/${item.episode}`}
                        aria-label={`Play episode ${item.episode} of ${item.title}`}
                        className="tap absolute top-1/2 right-3 grid size-8 -translate-y-1/2 place-items-center rounded-full bg-accent-500 text-white shadow-lg transition-colors hover:bg-accent-400"
                      >
                        <svg viewBox="0 0 24 24" className="size-3.5 translate-x-px" aria-hidden>
                          <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />
                        </svg>
                      </Link>
                    )}
                  </li>
                )
              })}
            </ul>
          )}
        </>
      )}
    </div>
  )
}
