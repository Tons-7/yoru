import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import type { ScheduleDay, ScheduleItem } from '../lib/api'
import { airTime, cx, dayAndTime, dayDate, dayName, whenLabel } from '../lib/format'
import { useAlertsOff, useNow, usePrefFlag, useSchedule, useSetPref } from '../lib/queries'
import { useMediaQuery } from '../lib/hooks'
import { Button, Empty, ErrorState, RefreshFailed, Segmented, Skeleton } from '../components/ui'
import { SwitchHeader, ViewSwitch } from '../components/ViewSwitch'
import { AlertPill } from '../components/AlertPill'
import { useAlertSwitch } from '../lib/alerts'
import { ToggleChip } from '../components/Chips'
import { releaseState, TONE_TEXT, useOutNow, type Range } from '../lib/aired'
import { useSide, type Sides } from '../lib/view'
import { readKey, writeKey } from '../lib/storage'
import { RecentView } from './Recent'

type Side = 'out' | 'coming'

const SIDES: Sides<Side> = {
  store: 'yoru.schedule.view',
  base: 'coming',
  alt: 'out',
  names: { out: 'out', recent: 'out', coming: 'coming' },
}

type Layout = 'day' | 'week'

const LAYOUT_KEY = 'yoru.schedule.layout'

function useLayoutChoice(): [Layout, (next: Layout) => void] {
  const [layout, setLayout] = useState<Layout>(() => (readKey(LAYOUT_KEY) === 'week' ? 'week' : 'day'))
  const choose = (next: Layout) => {
    setLayout(next)
    writeKey(LAYOUT_KEY, next)
  }
  return [layout, choose]
}

export function SchedulePage() {
  const [side, setSide] = useSide(SIDES)
  const [range, setRange] = useState<Range>('72')
  const { unwatched } = useOutNow(range)
  const switcher = (
    <ViewSwitch<Side>
      options={[
        { value: 'out', label: unwatched > 0 ? `Out now · ${unwatched}` : 'Out now' },
        { value: 'coming', label: 'Coming up' },
      ]}
      value={side}
      onChange={setSide}
      className="sm:w-64"
    />
  )
  return side === 'out' ? (
    <RecentView range={range} onRange={setRange} switcher={switcher} />
  ) : (
    <Airing switcher={switcher} onOut={() => setSide('out')} />
  )
}

function Airing({ switcher, onOut }: { switcher: ReactNode; onOut: () => void }) {
  const [mine, setMine, ready] = usePrefFlag('schedule.mine')
  const alertsOff = useAlertsOff()
  const setPref = useSetPref()
  const roomy = useMediaQuery('(min-width: 75rem)')
  const [layout, setLayout] = useLayoutChoice()
  const week = roomy && layout === 'week'
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
  const earlier = active?.today ? active.items.filter((item) => item.airingAt <= now) : []
  const rows = active ? (active.today ? active.items.filter((item) => item.airingAt > now) : active.items) : []
  const nowLine = earlier.length > 0 && !!nextUp && rows[0] === nextUp
  const seen = new Set<number>()
  const alerted = ahead.filter((item) => item.alerts && !seen.has(item.animeId) && seen.add(item.animeId))

  const upcoming = useRef<HTMLLIElement>(null)
  useEffect(() => {
    const el = upcoming.current
    if (!el) return
    const box = el.getBoundingClientRect()
    if (box.top >= 0 && box.bottom <= window.innerHeight) return
    el.scrollIntoView({ block: 'center', behavior: 'auto' })
  }, [active?.date, week])

  return (
    <div className="space-y-4">
      <SwitchHeader
        beside
        title="Schedule"
        switcher={switcher}
        actions={
          <>
            <ToggleChip on={mine} onChange={setMine}>
              Only my list
            </ToggleChip>
            {roomy && (
              <Segmented
                options={[
                  { value: 'day', label: 'Day' },
                  { value: 'week', label: 'Week' },
                ]}
                value={layout}
                onChange={setLayout}
              />
            )}
          </>
        }
      />

      {isError && data && <RefreshFailed retry={() => refetch()} />}
      {isError && !data ? (
        <ErrorState error={error} retry={() => refetch()} />
      ) : isPending ? (
        <Skeleton className="h-96 w-full" />
      ) : (
        <>
          {!week && <DayStrip days={days} shownIndex={shownIndex} todayIndex={todayIndex} onPick={setSelectedDate} />}

          {alertsOff && (
            <p className="surface flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 text-sm text-base-300">
              Alerts are off for every show, so Alert can't be set.
              <Button
                size="sm"
                onClick={() => setPref.mutate({ key: 'notify.enabled', value: 'true' })}
                disabled={setPref.isPending}
              >
                Turn alerts on
              </Button>
            </p>
          )}

          {week ? (
            <WeekView days={days.slice(0, 7)} now={now} nextUp={nextUp} />
          ) : (
            <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_20rem]">
              {!active || active.items.length === 0 ? (
                <Empty title="Nothing airing this day" hint={mine ? 'Nothing from your list, at least.' : undefined} />
              ) : (
                <ul className="surface divide-y divide-white/[0.05] overflow-hidden">
                  {earlier.length > 0 && (
                    <li>
                      <button
                        type="button"
                        onClick={onOut}
                        className="flex min-h-14 w-full items-center gap-3 px-3 text-left transition-colors hover:bg-base-800/60 sm:gap-1.5"
                      >
                        <span className="min-w-0 max-sm:flex-1">
                          <span className="block text-sm font-medium text-base-100 tabular-nums sm:text-base-300">
                            Earlier today · {earlier.length} out
                          </span>
                          <span className="block text-xs text-base-400 sm:hidden">Opens Out now</span>
                        </span>
                        <svg viewBox="0 0 24 24" className="size-4 shrink-0 text-base-400" aria-hidden>
                          <path d="m9 6 6 6-6 6" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                      </button>
                    </li>
                  )}
                  {earlier.length > 0 && rows.length === 0 && (
                    <li className="px-3 py-4 text-sm text-base-400">Nothing more airs today.</li>
                  )}
                  {byTime(rows).map((slot) => {
                    const hasNext = !!nextUp && slot.items.includes(nextUp)
                    return (
                      <li key={slot.at} ref={hasNext ? upcoming : undefined}>
                        {hasNext && nowLine && (
                          <div aria-hidden className="flex items-center gap-2 px-3 py-1 text-xs font-medium text-accent-300">
                            <span className="h-px flex-1 bg-accent-500/50" />
                            Now · {airTime(now)}
                            <span className="h-px flex-1 bg-accent-500/50" />
                          </div>
                        )}
                        <p className="flex h-7 items-end gap-1.5 px-3 text-[13px] font-semibold text-base-200 tabular-nums sm:hidden">
                          {airTime(slot.at)}
                          {Math.abs(slot.at - now) < 86_400 && (
                            <span className={cx('font-medium', hasNext ? 'text-accent-300' : 'text-base-500')}>
                              · {whenLabel(slot.at, now * 1000)}
                            </span>
                          )}
                        </p>
                        <ul>
                          {slot.items.map((item, i) => (
                            <ScheduleRow
                              key={`${item.animeId}-${item.episode}`}
                              item={item}
                              first={i === 0}
                              isNext={item === nextUp}
                              now={now}
                              weekday={active.weekday}
                              alertsOff={alertsOff}
                            />
                          ))}
                        </ul>
                      </li>
                    )
                  })}
                </ul>
              )}
              <aside className="space-y-4 max-xl:hidden">
                {nextUp && <AirsNext key={nextUp.animeId} item={nextUp} now={now} alertsOff={alertsOff} />}
                <section className="surface p-4">
                  <div className="flex items-center justify-between gap-2">
                    <h2 className="text-sm font-semibold text-white">Your alerts</h2>
                    <Link to="/notifications" className="text-xs font-medium text-accent-300 hover:underline">
                      Manage
                    </Link>
                  </div>
                  {alertsOff ? (
                    <p className="mt-1 text-sm text-base-500">Alerts are off for every show.</p>
                  ) : alerted.length === 0 ? (
                    <p className="mt-1 text-sm text-base-500">None this week. Tap Alert on a show to hear when new episodes are out.</p>
                  ) : (
                    <ul className="mt-2 space-y-1">
                      {alerted.slice(0, 8).map((item) => (
                        <li key={item.animeId}>
                          <Link to={`/anime/${item.animeId}`} className="-mx-2 flex items-center gap-3 rounded-lg px-2 py-1.5 hover:bg-base-850">
                            <Poster item={item} className="h-10 w-7" />
                            <span className="min-w-0">
                              <span className="line-clamp-1 block text-sm text-base-100">{item.title}</span>
                              <span className="block text-xs text-base-500 tabular-nums">
                                Ep {item.episode} · {dayAndTime(item.airingAt, now * 1000)}
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
          )}
        </>
      )}
    </div>
  )
}

function byTime(items: ScheduleItem[]) {
  const slots: Array<{ at: number; items: ScheduleItem[] }> = []
  for (const item of items) {
    const last = slots[slots.length - 1]
    if (last?.at === item.airingAt) last.items.push(item)
    else slots.push({ at: item.airingAt, items: [item] })
  }
  return slots
}

function Poster({ item, className, dim = false }: { item: ScheduleItem; className: string; dim?: boolean }) {
  const src = item.thumb ?? item.cover
  return src ? (
    <img src={src} alt="" loading="lazy" className={cx('shrink-0 rounded object-cover shadow-card', dim && 'brightness-75', className)} />
  ) : (
    <div className={cx('shrink-0 rounded bg-base-850', className)} />
  )
}

function DayStrip({
  days,
  shownIndex,
  todayIndex,
  onPick,
}: {
  days: ScheduleDay[]
  shownIndex: number
  todayIndex: number
  onPick: (date: string) => void
}) {
  const today = days[todayIndex]?.date
  return (
    <div className="sticky top-[var(--bar,4rem)] z-30 -mx-4 bg-base-950 px-2 py-2 transition-[top] duration-200 sm:-mx-6 sm:px-6">
      <div className="grid grid-cols-8 gap-0.5 sm:flex sm:snap-x sm:gap-2 sm:overflow-x-auto sm:no-scrollbar lg:grid lg:overflow-visible">
        {days.map((day, i) => {
          const isActive = i === shownIndex
          const name = dayName(day.date, day.weekday, today)
          const mine = day.items.some((item) => item.onList)
          return (
            <button
              key={day.date}
              type="button"
              aria-pressed={isActive}
              aria-label={`${name}, ${dayDate(day.date)}, ${day.items.length} episodes${mine ? ', with shows from your list' : ''}`}
              onClick={() => onPick(day.date)}
              className={cx(
                'flex min-h-13 min-w-0 flex-col items-center justify-center rounded-xl ring-1 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-300',
                'sm:min-w-[6.75rem] sm:shrink-0 sm:snap-start sm:items-start sm:px-3.5 sm:py-2 lg:min-w-0',
                isActive
                  ? 'bg-accent-500 text-white ring-accent-400 sm:bg-accent-500/15 sm:ring-accent-400/60'
                  : day.today
                    ? 'bg-base-900 ring-accent-400/70 hover:bg-base-850'
                    : 'bg-base-900/60 ring-white/[0.06] hover:bg-base-850 hover:ring-white/10',
              )}
            >
              <span
                className={cx(
                  'text-[11px] font-medium uppercase sm:hidden',
                  isActive ? 'text-white/85' : day.today ? 'text-accent-300' : 'text-base-400',
                )}
              >
                {day.weekday.slice(0, 3)}
              </span>
              <span className="text-base leading-tight font-semibold tabular-nums sm:hidden">
                {Number(day.date.slice(8))}
              </span>
              <span
                aria-hidden
                className={cx(
                  'mt-0.5 size-1 rounded-full sm:hidden',
                  mine ? (isActive ? 'bg-white' : 'bg-accent-400') : 'bg-transparent',
                )}
              />
              <span
                className={cx(
                  'flex items-center gap-1.5 text-sm font-semibold whitespace-nowrap max-sm:hidden',
                  isActive ? 'text-accent-300' : 'text-base-100',
                )}
              >
                {day.today && <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-accent-400" />}
                {name}
              </span>
              <span className="mt-0.5 flex items-center gap-1.5 text-xs whitespace-nowrap text-base-500 tabular-nums max-sm:hidden">
                {dayDate(day.date)} · {day.items.length} ep
                {mine && <span aria-hidden className="size-1 rounded-full bg-accent-400" />}
              </span>
            </button>
          )
        })}
      </div>
    </div>
  )
}

function rowMeta(item: ScheduleItem, isNext: boolean, now: number, weekday: string): ReactNode[] {
  const parts: ReactNode[] = [`Ep ${item.episode}`]
  if (isNext) parts.push(<span className="text-accent-300">Next</span>)
  if (!item.watched && item.behind > 0) parts.push(`${item.behind} behind`)
  else if (item.onList && !item.watched && item.behind === 0 && item.airingAt > now) parts.push('On list')
  if (item.delayedFrom) {
    parts.push(
      <span className="text-amber-300">
        Delayed, was due {new Date(item.delayedFrom * 1000).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}
      </span>,
    )
  }
  if (item.jstWeekday !== weekday) parts.push(`${item.jstWeekday} in Japan`)
  const state = releaseState(item, now)
  if (state) {
    parts.push(
      <span className={TONE_TEXT[state.tone]}>
        {state.tone === 'wait' ? 'Aired · not found yet' : state.word}
        {state.checked && ` · ${state.checked}`}
      </span>,
    )
  }
  return parts
}

function ScheduleRow({
  item,
  first,
  isNext,
  now,
  weekday,
  alertsOff,
}: {
  item: ScheduleItem
  first: boolean
  isNext: boolean
  now: number
  weekday: string
  alertsOff: boolean
}) {
  const aired = item.airingAt <= now
  const meta = rowMeta(item, isNext, now, weekday)
  return (
    <li
      className={cx(
        'flex items-center gap-2 border-l-2 pr-3 transition-colors hover:bg-base-800/40',
        isNext ? 'border-accent-500 bg-accent-500/10' : 'border-transparent',
      )}
    >
      <Link to={`/anime/${item.animeId}`} className="flex min-w-0 flex-1 items-center gap-3 py-3 pl-3 sm:py-2.5">
        <span className="w-20 shrink-0 self-start pt-0.5 text-right max-sm:hidden">
          {first && (
            <>
              <span className="block text-sm font-medium whitespace-nowrap tabular-nums text-base-100">{airTime(item.airingAt)}</span>
              {Math.abs(item.airingAt - now) < 86_400 && (
                <span className={cx('block text-xs whitespace-nowrap', isNext ? 'text-accent-300' : 'text-base-500')}>
                  {whenLabel(item.airingAt, now * 1000)}
                </span>
              )}
            </>
          )}
        </span>
        <Poster item={item} dim={aired} className="h-[68px] w-12 sm:h-14 sm:w-10" />
        <span className="min-w-0 flex-1">
          <span
            className={cx(
              'line-clamp-2 text-base leading-[22px] font-semibold sm:text-sm sm:leading-5 sm:font-medium',
              aired && !isNext ? 'text-base-300' : 'text-base-100',
            )}
          >
            {item.title}
          </span>
          <span className="mt-0.5 block text-sm text-base-400 sm:text-xs">
            {meta.map((part, i) => (
              <span key={i}>
                {i > 0 && ' · '}
                {part}
              </span>
            ))}
          </span>
        </span>
      </Link>
      <RowControl item={item} aired={aired} alertsOff={alertsOff} />
    </li>
  )
}

function RowControl({ item, aired, alertsOff }: { item: ScheduleItem; aired: boolean; alertsOff: boolean }) {
  if (aired && item.watched) {
    return (
      <span
        role="img"
        aria-label="Watched"
        className="grid size-9 shrink-0 place-items-center rounded-full bg-emerald-400/15 text-emerald-300 pointer-coarse:size-11"
      >
        <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
          <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </span>
    )
  }
  if (aired || item.out) {
    return (
      <Link
        to={`/watch/${item.animeId}/${item.episode}`}
        aria-label={`Play episode ${item.episode} of ${item.title}`}
        className={cx(
          'press grid size-9 shrink-0 place-items-center rounded-full transition-colors pointer-coarse:size-11',
          item.out ? 'bg-accent-500 text-white hover:bg-accent-600' : 'bg-white/[0.08] text-white hover:bg-white/15',
        )}
      >
        <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
          <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />
        </svg>
      </Link>
    )
  }
  return <RowAlert item={item} alertsOff={alertsOff} />
}

function RowAlert({ item, alertsOff, wide = false }: { item: ScheduleItem; alertsOff: boolean; wide?: boolean }) {
  const { change, pending } = useAlertSwitch(item.animeId, item.title)
  return (
    <AlertPill
      on={item.alerts}
      title={item.title}
      onToggle={() => change(!item.alerts)}
      pending={pending}
      disabled={alertsOff}
      wide={wide}
      className={wide ? undefined : 'max-sm:px-2.5'}
    />
  )
}

function AirsNext({ item, now, alertsOff }: { item: ScheduleItem; now: number; alertsOff: boolean }) {
  const art = item.show?.banner ?? item.coverLarge ?? item.cover
  return (
    <section className="surface overflow-hidden">
      <div className="relative aspect-[16/9] bg-base-850">
        {art && <img src={art} alt="" className="size-full object-cover object-top" />}
        <p className="absolute top-3 left-3 rounded-md bg-base-950/90 px-2 py-0.5 text-xs font-semibold text-base-100">
          Airs next
        </p>
      </div>
      <div className="p-4">
        <Link to={`/anime/${item.animeId}`} className="line-clamp-2 font-medium text-white hover:text-accent-300">
          {item.title}
        </Link>
        <p className="mt-1 text-sm text-base-400 tabular-nums">
          Ep {item.episode} · {airTime(item.airingAt)} · {whenLabel(item.airingAt, now * 1000)}
        </p>
        <div className="mt-3">
          <RowAlert item={item} alertsOff={alertsOff} wide />
        </div>
        {alertsOff && <p className="mt-2 text-xs text-base-500">Alerts are off for every show.</p>}
      </div>
    </section>
  )
}

function WeekView({ days, now, nextUp }: { days: ScheduleDay[]; now: number; nextUp?: ScheduleItem }) {
  const today = days.find((d) => d.today)?.date
  return (
    <div className="grid grid-cols-7 gap-2">
      {days.map((day) => {
        const name = dayName(day.date, day.weekday, today, true)
        const nowAt = day.today ? day.items.findIndex((item) => item.airingAt > now) : -1
        return (
          <section
            key={day.date}
            aria-label={`${dayName(day.date, day.weekday, today)}, ${dayDate(day.date)}`}
            className={cx('min-w-0 rounded-card', day.today ? 'bg-accent-500/[0.07] ring-1 ring-accent-500/30' : 'bg-base-900/40')}
          >
            <h2
              className={cx(
                'sticky top-[var(--bar,4rem)] z-20 rounded-t-card px-2.5 py-2 transition-[top] duration-200',
                day.today ? 'bg-base-950 bg-[linear-gradient(rgb(106_85_245/0.16),rgb(106_85_245/0.16))]' : 'bg-base-950',
              )}
            >
              <span className={cx('block text-sm font-semibold', day.today ? 'text-accent-300' : 'text-base-100')}>
                {day.today ? `${day.weekday.slice(0, 3)} ${Number(day.date.slice(8))} · Today` : `${name} ${Number(day.date.slice(8))}`}
              </span>
              <span className="block text-xs text-base-500 tabular-nums">
                {day.items.length} {day.items.length === 1 ? 'episode' : 'episodes'}
              </span>
            </h2>
            {day.items.length === 0 ? (
              <p className="px-2.5 pb-3 text-xs text-base-500">Nothing airs.</p>
            ) : (
              <ul className="space-y-1.5 p-1.5">
                {day.items.map((item, i) => (
                  <WeekCard key={`${item.animeId}-${item.episode}`} item={item} now={now} isNext={item === nextUp} nowLine={i === nowAt} />
                ))}
                {day.today && nowAt === -1 && <NowLine now={now} />}
              </ul>
            )}
          </section>
        )
      })}
    </div>
  )
}

function NowLine({ now }: { now: number }) {
  return (
    <li aria-hidden className="flex items-center gap-1.5 px-1 py-0.5 text-[11px] font-semibold text-accent-300 tabular-nums">
      <span className="size-1.5 rounded-full bg-accent-400" />
      Now {airTime(now)}
      <span className="h-px flex-1 bg-accent-500/60" />
    </li>
  )
}

function WeekCard({ item, now, isNext, nowLine }: { item: ScheduleItem; now: number; isNext: boolean; nowLine: boolean }) {
  const aired = item.airingAt <= now
  const state = releaseState(item, now)
  return (
    <>
      {nowLine && <NowLine now={now} />}
      <li>
        <Link
          to={`/anime/${item.animeId}`}
          className={cx(
            'flex gap-2 rounded-lg border-l-[3px] bg-base-900 py-1.5 pr-1.5 pl-1.5 transition-colors hover:bg-base-850',
            item.onList ? 'border-accent-500' : 'border-transparent',
            isNext && 'ring-1 ring-accent-400/50',
          )}
        >
          <Poster item={item} dim={aired} className="h-14 w-10" />
          <span className="min-w-0 flex-1">
            <span className={cx('block text-[13px] tabular-nums', isNext ? 'text-accent-300' : 'text-base-400')}>
              {airTime(item.airingAt)}
            </span>
            <span className={cx('line-clamp-2 text-sm leading-[18px] font-medium', aired ? 'text-base-300' : 'text-base-100')}>
              {item.title}
            </span>
            <span className="block text-xs text-base-400">
              Ep {item.episode}
              {state ? (
                <span className={TONE_TEXT[state.tone]}> · {state.word}</span>
              ) : (
                !aired && item.alerts && <span className="text-accent-300"> · Alert on</span>
              )}
            </span>
          </span>
        </Link>
      </li>
    </>
  )
}
