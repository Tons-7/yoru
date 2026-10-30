import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, type AlertShow } from '../lib/api'
import { cx, dayAndTime, whenLabel } from '../lib/format'
import { useAlertsOff, useNotifications, useNow, useSetPref } from '../lib/queries'
import { feedTitle, useAlertShows, useAlertSwitch, useDeviceAlerts, type DeviceAlerts, type FeedItem } from '../lib/alerts'
import { notificationTarget } from '../lib/notifications'
import { appleTouchDevice, runningInstalled } from '../lib/install'
import { testPush } from '../lib/push'
import { useDeviceWord } from '../lib/shelf'
import { AlertPill, BellGlyph } from './AlertPill'
import { MoreMenu, type MoreItem } from './MoreMenu'
import { Button, ConfirmInline, ErrorState, LinkButton, PageHeader, Skeleton } from './ui'

interface FeedRow {
  key: string
  lead: FeedItem
  ids: number[]
  unread: number[]
  episodes: number[]
}

function rowsOf(items: FeedItem[]): FeedRow[] {
  const rows: FeedRow[] = []
  const shows = new Map<number, FeedRow>()
  for (const n of items) {
    const grouped = n.kind === 'release' && !!n.animeId
    const row = grouped ? shows.get(n.animeId) : undefined
    if (row) {
      row.ids.push(n.id)
      if (!n.read) row.unread.push(n.id)
      if (n.episode) row.episodes.push(n.episode)
      continue
    }
    const fresh = { key: `n-${n.id}`, lead: n, ids: [n.id], unread: n.read ? [] : [n.id], episodes: n.episode ? [n.episode] : [] }
    if (grouped) shows.set(n.animeId, fresh)
    rows.push(fresh)
  }
  return rows
}

export function NotificationsView() {
  const feed = useNotifications()
  const alerts = useAlertShows()
  const qc = useQueryClient()
  const now = useNow() / 1000
  const [confirmClear, setConfirmClear] = useState(false)
  const refresh = () => qc.invalidateQueries({ queryKey: ['notifications'] })
  const read = useMutation({
    mutationFn: (ids?: number[]) =>
      ids ? Promise.all(ids.map((id) => api.post(`/api/notifications/read?id=${id}`))) : api.post('/api/notifications/read'),
    onSuccess: refresh,
  })
  const remove = useMutation({
    mutationFn: (ids?: number[]) =>
      ids ? Promise.all(ids.map((id) => api.del(`/api/notifications?id=${id}`))) : api.del('/api/notifications'),
    onSuccess: () => {
      setConfirmClear(false)
      void refresh()
    },
  })

  const items = (feed.data?.items ?? []) as FeedItem[]
  const unread = feed.data?.unread ?? 0
  const midnight = new Date(now * 1000).setHours(0, 0, 0, 0) / 1000
  const today = rowsOf(items.filter((n) => n.createdAt >= midnight))
  const earlier = rowsOf(items.filter((n) => n.createdAt < midnight))
  const shows = alerts.data?.shows ?? []
  const next = shows.find((s) => s.nextAiringAt && s.nextAiringAt > now)

  const rowProps = {
    now,
    onRead: (ids: number[]) => ids.length > 0 && read.mutate(ids),
    onRemove: (ids: number[]) => remove.mutate(ids),
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="Notifications"
        meta={unread > 0 && <span className="text-sky-300">{unread} new</span>}
        actions={
          items.length > 0 && (
            <>
              {unread > 0 && (
                <Button onClick={() => read.mutate(undefined)} disabled={read.isPending}>
                  Mark all read
                </Button>
              )}
              {confirmClear ? (
                <ConfirmInline
                  label="Delete all"
                  onConfirm={() => remove.mutate(undefined)}
                  onCancel={() => setConfirmClear(false)}
                  pending={remove.isPending}
                />
              ) : (
                <Button variant="ghost" onClick={() => setConfirmClear(true)}>
                  Clear all
                </Button>
              )}
            </>
          )
        }
      />

      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,680px)_20rem] lg:gap-8">
        <div className="space-y-4 lg:col-start-2 lg:row-start-1">
          <DeviceStatus />
          {next && <NextAlert show={next} now={now} />}
          <AlertShows result={alerts} now={now} />
        </div>

        <div className="space-y-5 lg:col-start-1 lg:row-start-1">
          {feed.isError && !feed.data ? (
            <ErrorState error={feed.error} retry={() => feed.refetch()} />
          ) : feed.isPending ? (
            <Skeleton className="h-64 w-full" />
          ) : items.length === 0 ? (
            <section className="surface flex flex-col items-start gap-3 p-5">
              <span className="grid size-11 place-items-center rounded-xl bg-base-850 text-base-400">
                <BellGlyph filled={false} className="size-5" />
              </span>
              <p className="font-medium text-base-100">Nothing new yet.</p>
              <p className="text-sm text-base-400">
                {next
                  ? `Next alert ${dayAndTime(next.nextAiringAt!, now * 1000)}, for ${next.title}.`
                  : 'Tap Alert on a show to hear when new episodes are out.'}
              </p>
              <LinkButton to="/schedule?view=coming" variant="primary">
                Open schedule
              </LinkButton>
            </section>
          ) : (
            <>
              <FeedSection title="Today" rows={today} {...rowProps} />
              <FeedSection title="Earlier" rows={earlier} {...rowProps} />
            </>
          )}
        </div>
      </div>
    </div>
  )
}

function FeedSection({
  title,
  rows,
  now,
  onRead,
  onRemove,
}: {
  title: string
  rows: FeedRow[]
  now: number
  onRead: (ids: number[]) => void
  onRemove: (ids: number[]) => void
}) {
  if (rows.length === 0) return null
  return (
    <section>
      <h2 className="mb-2 text-sm font-semibold text-base-300">{title}</h2>
      <ul className="surface divide-y divide-white/[0.05] overflow-hidden">
        {rows.map((row) => (
          <FeedRowItem key={row.key} row={row} now={now} onRead={onRead} onRemove={onRemove} />
        ))}
      </ul>
    </section>
  )
}

function FeedRowItem({
  row,
  now,
  onRead,
  onRemove,
}: {
  row: FeedRow
  now: number
  onRead: (ids: number[]) => void
  onRemove: (ids: number[]) => void
}) {
  const n = row.lead
  const title = feedTitle(n, row.episodes)
  const when = whenLabel(n.createdAt, now * 1000)
  const release = n.kind === 'release' && !!n.animeId
  const first = row.episodes.length > 0 ? Math.min(...row.episodes) : 1
  const play = release ? `/watch/${n.animeId}/${first}` : null
  const menu: MoreItem[] = [
    ...(row.unread.length > 0 ? [{ label: 'Mark read', run: () => onRead(row.unread) }] : []),
    ...(n.animeId ? [{ label: 'Open show', to: `/anime/${n.animeId}` }] : []),
    { label: 'Remove', run: () => onRemove(row.ids), danger: true },
  ]
  return (
    <li className={cx('flex items-center gap-2 py-2 pr-2 pl-3', row.unread.length > 0 && 'bg-sky-400/[0.05]')}>
      <span
        aria-hidden
        className={cx('size-2 shrink-0 rounded-full', row.unread.length > 0 ? 'bg-sky-400' : 'bg-transparent')}
      />
      <Link
        to={play ?? notificationTarget(n)}
        onClick={() => onRead(row.unread)}
        className="flex min-w-0 flex-1 items-center gap-3 rounded-lg py-1"
      >
        {n.cover ? (
          <img src={n.cover} alt="" loading="lazy" className="h-14 w-10 shrink-0 rounded object-cover" />
        ) : (
          <span className="grid h-14 w-10 shrink-0 place-items-center rounded bg-base-850 text-base-500">
            <BellGlyph filled={false} />
          </span>
        )}
        <span className="min-w-0">
          <span className={cx('line-clamp-2 text-sm', row.unread.length > 0 ? 'font-semibold text-base-100' : 'text-base-200')}>
            {title}
          </span>
          <span className="block truncate text-xs text-base-500">
            {release || !n.body ? when : `${n.body} · ${when}`}
            {row.unread.length > 0 && <span className="sr-only">, unread</span>}
          </span>
        </span>
      </Link>
      {play && (
        <Link
          to={play}
          onClick={() => onRead(row.unread)}
          aria-label={`Play episode ${first} of ${n.show ?? 'this show'}`}
          className="press grid size-11 shrink-0 place-items-center rounded-full bg-accent-500 text-white transition-colors hover:bg-accent-600"
        >
          <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
            <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />
          </svg>
        </Link>
      )}
      <MoreMenu label={`More for ${title}`} title={title} items={menu} className="size-11 rounded-full" />
    </li>
  )
}

const STEPS: Record<'home-screen' | 'blocked' | 'blocked-app', string[]> = {
  'home-screen': [
    'In Safari, tap Share.',
    'Tap Add to Home Screen, then Add.',
    'Open Yoru from your Home Screen, come back here and tap Turn on.',
  ],
  blocked: [
    "Open this site's settings: the icon at the left of the address bar.",
    'Set Notifications to Allow.',
    'Reload Yoru, then tap Turn on here.',
  ],
  'blocked-app': [
    'Open the Settings app, then Notifications.',
    'Find Yoru and turn Allow Notifications on.',
    'Come back here and tap Turn on.',
  ],
}

const STATUS: Record<DeviceAlerts, { word: string; hint: string }> = {
  on: { word: 'On', hint: 'New episodes reach this {device}, even with Yoru closed.' },
  'open-only': {
    word: 'Only while Yoru is open',
    hint: 'With Yoru closed, alerts need its secure (https) address, or a browser that supports them.',
  },
  off: { word: 'Off', hint: "This {device} doesn't get alerts yet." },
  blocked: { word: 'Blocked by your browser', hint: 'The browser said no to notifications for Yoru.' },
  'home-screen': {
    word: 'Needs Yoru on your Home Screen',
    hint: 'On an iPhone, alerts only reach Yoru added to the Home Screen.',
  },
}

function DeviceStatus() {
  const device = useDeviceWord()
  const alertsOff = useAlertsOff()
  const setPref = useSetPref()
  const { state, push, busy, error, set } = useDeviceAlerts()
  const [help, setHelp] = useState(false)
  const test = useMutation({ meta: { inline: true }, mutationFn: testPush })

  if (alertsOff) {
    return (
      <StatusCard
        label="Alerts"
        word="Off for every show"
        hint="Nothing alerts you until they're back on."
        action={
          <Button
            variant="primary"
            onClick={() => setPref.mutate({ key: 'notify.enabled', value: 'true' })}
            disabled={setPref.isPending}
          >
            Turn on
          </Button>
        }
      />
    )
  }
  if (!state) return <Skeleton className="h-24 w-full" />

  const blockedSteps = appleTouchDevice() && runningInstalled() ? STEPS['blocked-app'] : STEPS.blocked
  const steps = state === 'home-screen' ? STEPS['home-screen'] : state === 'blocked' ? blockedSteps : null
  const { word, hint } = STATUS[state]
  const insecure = state === 'open-only' && push === 'insecure'

  return (
    <StatusCard
      label={`Alerts on this ${device}`}
      word={word}
      hint={
        insecure
          ? 'With Yoru closed, alerts need its secure (https) address: Settings, Devices.'
          : hint.replace('{device}', device)
      }
      action={
        steps ? (
          <Button variant="primary" onClick={() => setHelp((v) => !v)} aria-expanded={help}>
            {state === 'home-screen' ? 'Show me how' : 'How to fix'}
          </Button>
        ) : state === 'off' ? (
          <Button variant="primary" onClick={() => void set(true)} disabled={busy}>
            Turn on
          </Button>
        ) : (
          <>
            {state === 'on' && (
              <Button onClick={() => test.mutate()} disabled={test.isPending}>
                {test.isPending ? 'Sending…' : test.isSuccess ? 'Sent' : 'Send a test'}
              </Button>
            )}
            <Button variant="ghost" onClick={() => void set(false)} disabled={busy}>
              Turn off
            </Button>
          </>
        )
      }
    >
      {steps && help && (
        <ol className="mt-3 list-decimal space-y-1 pl-5 text-sm text-base-300">
          {steps.map((step) => (
            <li key={step}>{step}</li>
          ))}
        </ol>
      )}
      {(error || test.isError) && <p className="mt-2 text-xs text-recap">{error || (test.error as Error).message}</p>}
    </StatusCard>
  )
}

function StatusCard({
  label,
  word,
  hint,
  action,
  children,
}: {
  label: string
  word: string
  hint: string
  action: ReactNode
  children?: ReactNode
}) {
  return (
    <section className="surface p-4">
      <p className="text-xs font-medium text-base-400">{label}</p>
      <p className="mt-0.5 font-semibold text-base-100">{word}</p>
      <p className="mt-0.5 text-sm text-base-400">{hint}</p>
      <div className="mt-3 flex flex-wrap items-center gap-2">{action}</div>
      {children}
    </section>
  )
}

function NextAlert({ show, now }: { show: AlertShow; now: number }) {
  return (
    <section className="surface p-4">
      <p className="text-xs font-medium text-base-400">Next alert</p>
      <Link to={`/anime/${show.animeId}`} className="mt-0.5 line-clamp-2 font-semibold text-base-100 hover:text-white">
        {show.title}
        {show.nextEpisode ? ` · Ep ${show.nextEpisode}` : ''}
      </Link>
      <p className="mt-0.5 text-sm text-base-400 tabular-nums">
        Airs {dayAndTime(show.nextAiringAt!, now * 1000)}. The alert comes once it's out.
      </p>
    </section>
  )
}

function AlertShows({ result, now }: { result: ReturnType<typeof useAlertShows>; now: number }) {
  const shows = result.data?.shows ?? []
  return (
    <section>
      <h2 className="mb-2 text-sm font-semibold text-base-300">
        Shows that alert you{shows.length > 0 && <span className="font-normal text-base-500"> · {shows.length}</span>}
      </h2>
      {result.isError && !result.data ? (
        <ErrorState error={result.error} retry={() => result.refetch()} />
      ) : result.isPending ? (
        <Skeleton className="h-32 w-full" />
      ) : shows.length === 0 ? (
        <p className="surface p-4 text-sm text-base-400">None yet. Tap Alert on a show to hear when new episodes are out.</p>
      ) : (
        <ul className="surface divide-y divide-white/[0.05] overflow-hidden">
          {shows.map((show) => (
            <AlertShowRow key={show.animeId} show={show} now={now} />
          ))}
        </ul>
      )}
      <p className="mt-2 text-xs text-base-500">Shows you mark Watching alert you too.</p>
    </section>
  )
}

function AlertShowRow({ show, now }: { show: AlertShow; now: number }) {
  const [on, setOn] = useState(true)
  const { change, pending } = useAlertSwitch(show.animeId, show.title, setOn)
  const meta =
    show.nextAiringAt && show.nextAiringAt > now
      ? `Ep ${show.nextEpisode} · ${dayAndTime(show.nextAiringAt, now * 1000)}`
      : show.status === 'FINISHED'
        ? 'Finished airing'
        : 'No date yet'
  return (
    <li className="flex items-center gap-3 py-2 pr-3 pl-3">
      <Link to={`/anime/${show.animeId}`} className="flex min-w-0 flex-1 items-center gap-3">
        {show.cover ? (
          <img src={show.cover} alt="" loading="lazy" className="h-14 w-10 shrink-0 rounded object-cover" />
        ) : (
          <span className="h-14 w-10 shrink-0 rounded bg-base-850" />
        )}
        <span className="min-w-0">
          <span className="line-clamp-2 text-sm font-medium text-base-100">{show.title}</span>
          <span className="block text-xs text-base-500 tabular-nums">
            {meta}
            {show.because === 'watching' && ' · Watching'}
          </span>
        </span>
      </Link>
      <AlertPill on={on} title={show.title} onToggle={() => change(!on)} pending={pending} />
    </li>
  )
}
