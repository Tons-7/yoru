import { Fragment, useState, type ReactNode } from 'react'
import { useMutation, useQueryClient, type UseQueryResult } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, type DiscoverItem, type LibraryItem, type ScheduleItem } from '../lib/api'
import { toCard } from '../lib/cards'
import { browseSeason, currentSeason, nextSeason } from '../lib/season'
import {
  refreshListState,
  useAired,
  useAlertsOff,
  useDiscover,
  useHome,
  useHomePicks,
  useNow,
  useUpNext,
  useUpNextEdit,
} from '../lib/queries'
import { toast } from '../lib/toast'
import { AiringToday } from '../components/AiringToday'
import { EditHomeSheet } from '../components/EditHome'
import { Hero } from '../components/Hero'
import { useHomeLayout, type HomeRowId } from '../components/homeLayout'
import { MoreMenu } from '../components/MoreMenu'
import { PosterCard } from '../components/PosterCard'
import { Rail, RailFailed, RailItem, RailSkeleton } from '../components/Rail'
import { ReleasedCard } from '../components/ReleasedCard'
import { ResumeCard } from '../components/ResumeCard'
import { TopTen } from '../components/TopTen'
import { soonestFirst } from '../components/upcoming'
import { Button, ConfirmInline, LinkButton } from '../components/ui'
import { UpNextRail, UpNextSheet } from '../components/UpNext'

export function Home() {
  const layout = useHomeLayout()
  const [params, setParams] = useSearchParams()
  const editingHome = params.get('edit') === 'home'
  const setEditingHome = (open: boolean) => {
    const next = new URLSearchParams(params)
    if (open) next.set('edit', 'home')
    else next.delete('edit')
    setParams(next, { replace: true })
  }

  const trending = useDiscover('trending', 20)
  const seasonal = useDiscover('season', 20)
  const upcoming = useDiscover('upcoming', 20)
  const continuing = useHome()
  const sequels = useHomePicks('sequels', layout.wanted('sequels'))
  const forYou = useHomePicks('for-you', layout.wanted('foryou'))
  const aired = useAired(48)
  const upNext = useUpNext()
  const alertsOff = useAlertsOff()
  const upNextEmpty = upNext.isSuccess && upNext.data.items.length === 0
  const [editingList, setEditingList] = useState(false)

  const now = useNow() / 1000
  const released = (aired.data?.items ?? [])
    .filter((item) => item.airingAt <= now)
    .sort((a, b) => b.airingAt - a.airingAt)
    .slice(0, 20)

  const queued = new Set((upNext.data?.items ?? []).map((it) => it.animeId))
  const resuming = (continuing.data?.items ?? []).filter((item) => !queued.has(item.id))

  const seen = new Map<number, number>()
  const mark = (id: number) => seen.set(id, (seen.get(id) ?? 0) + 1)
  for (const show of (trending.data?.items ?? []).filter((a) => a.cover).slice(0, 10)) mark(show.id)
  if (layout.shown('upnext')) {
    for (const id of queued) mark(id)
    for (const item of resuming) mark(item.id)
  }
  const fresh = (items: DiscoverItem[], most: number) => {
    const out = items.filter((a) => (seen.get(a.id) ?? 0) < most)
    for (const a of out) mark(a.id)
    return out
  }
  const picked: Partial<Record<HomeRowId, DiscoverItem[]>> = {}
  for (const id of layout.order) {
    if (!layout.shown(id)) continue
    if (id === 'foryou') picked.foryou = fresh(forYou.data?.items ?? [], 1)
    else if (id === 'sequels') picked.sequels = fresh(sequels.data?.items ?? [], 2)
    else if (id === 'season') picked.season = fresh(seasonal.data?.items ?? [], 2)
    else if (id === 'upcoming') picked.upcoming = fresh(soonestFirst(upcoming.data?.items ?? []), 2)
  }

  const row = (id: HomeRowId): ReactNode => {
    const fold = { folded: layout.folded(id), onUnfold: layout.unfold(id) }
    switch (id) {
      case 'upnext':
        if (continuing.isError && !continuing.data && upNextEmpty)
          return <RailFailed title="Up next" retry={() => void continuing.refetch()} />
        return (
          <UpNextRail
            onEdit={() => setEditingList(true)}
            after={resuming.map((item) => ({ key: `resume-${item.id}`, node: <ContinueCard item={item} /> }))}
            {...fold}
          />
        )
      case 'airing':
        return <AiringToday {...fold} />
      case 'trending':
        return <TopTen {...fold} />
      case 'foryou':
        return <ShowsRail title="For you" result={forYou} items={picked.foryou ?? []} quiet {...fold} />
      case 'sequels':
        return <ShowsRail title="Sequels you missed" result={sequels} items={picked.sequels ?? []} quiet {...fold} />
      case 'season':
        return (
          <ShowsRail
            title="This season"
            to={browseSeason(currentSeason())}
            result={seasonal}
            items={picked.season ?? []}
            {...fold}
          />
        )
      case 'released':
        return <ReleasedRail result={aired} items={released} {...fold} />
      case 'upcoming':
        return (
          <ShowsRail
            title="Upcoming"
            to={browseSeason(nextSeason())}
            result={upcoming}
            items={picked.upcoming ?? []}
            alert={!alertsOff}
            meta={alertsOff ? 'Alerts are off for every show' : undefined}
            action={
              alertsOff && (
                <LinkButton to="/settings?tab=Notifications" size="sm">
                  Turn alerts on
                </LinkButton>
              )
            }
            {...fold}
          />
        )
    }
  }

  return (
    <div className="space-y-8">
      <Hero items={trending.data?.items} loading={trending.isPending} />
      {layout.order.map((id) => layout.shown(id) && <Fragment key={id}>{row(id)}</Fragment>)}
      <div className="flex justify-center pt-2">
        <Button size="lg" className="w-full sm:w-auto" onClick={() => setEditingHome(true)}>
          <SlidersGlyph />
          Edit Home
        </Button>
      </div>
      <UpNextSheet open={editingList} onClose={() => setEditingList(false)} />
      <EditHomeSheet open={editingHome} onClose={() => setEditingHome(false)} layout={layout} />
    </div>
  )
}

interface Fold {
  folded: boolean
  onUnfold: () => void
}

function ShowsRail({
  title,
  to,
  result,
  items,
  quiet = false,
  alert,
  meta,
  action,
  folded,
  onUnfold,
}: Fold & {
  title: string
  to?: string
  result: UseQueryResult<unknown>
  items: DiscoverItem[]
  quiet?: boolean
  alert?: boolean
  meta?: ReactNode
  action?: ReactNode
}) {
  if (result.isError && !result.data) return <RailFailed title={title} to={to} retry={() => void result.refetch()} />
  if (result.isPending) {
    if (quiet) return null
    if (!folded) return <RailSkeleton title={title} to={to} />
  } else if (items.length === 0) return null
  if (folded) return <Rail title={title} to={to} folded onUnfold={onUnfold}>{null}</Rail>
  return (
    <Rail title={title} to={to} meta={meta} action={action}>
      {items.map((anime) => (
        <RailItem key={anime.id}>
          <PosterCard anime={toCard(anime)} alert={alert} />
        </RailItem>
      ))}
    </Rail>
  )
}

function ReleasedRail({
  result,
  items,
  folded,
  onUnfold,
}: Fold & { result: UseQueryResult<unknown>; items: ScheduleItem[] }) {
  const title = 'Recently released'
  const to = '/schedule?view=out'
  if (result.isError && !result.data) return <RailFailed title={title} to={to} retry={() => void result.refetch()} />
  if (result.isPending) return folded ? null : <RailSkeleton title={title} to={to} size="still" />
  if (items.length === 0) return null
  if (folded) return <Rail title={title} to={to} folded onUnfold={onUnfold}>{null}</Rail>
  return (
    <Rail title={title} to={to}>
      {items.map((item) => (
        <RailItem key={`${item.animeId}-${item.episode}`} size="still">
          <ReleasedCard item={item} />
        </RailItem>
      ))}
    </Rail>
  )
}

function ContinueCard({ item }: { item: LibraryItem }) {
  const resume = item.resume
  const episode = resume?.episode ?? item.progress + 1
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { add } = useUpNextEdit()
  const [confirming, setConfirming] = useState(false)
  const dismiss = useMutation({
    mutationFn: () => api.post('/api/continue/dismiss', { animeId: item.id, episode }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['continue'] }),
  })
  const watched = useMutation({
    mutationFn: () => api.post('/api/watched', { animeId: item.id, episode, watched: true }),
    onSuccess: () => {
      refreshListState(qc)
      toast(`Marked episode ${episode} of ${item.title} watched`, 'info')
    },
  })

  return (
    <ResumeCard
      animeId={item.id}
      title={item.title}
      cover={item.cover}
      episode={episode}
      position={resume?.position}
      duration={resume?.duration}
      fallback="Next episode"
      menu={
        <MoreMenu
          label={`More for ${item.title}`}
          menuLabel={`${item.title} in Up next`}
          className="size-11 rounded-full"
          items={[
            { label: 'Go to show', run: () => navigate(`/anime/${item.id}`) },
            { label: 'Add to Up next', run: () => add.mutate({ animeId: item.id, episode }), disabled: add.isPending },
            { label: 'Mark as watched', run: () => watched.mutate(), disabled: watched.isPending },
            { label: 'Remove from Up next', run: () => setConfirming(true), danger: true },
          ]}
        />
      }
      footer={
        confirming && (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <ConfirmInline
              label="Remove"
              note="Progress stays."
              onConfirm={() => dismiss.mutate()}
              onCancel={() => setConfirming(false)}
              pending={dismiss.isPending}
              size="sm"
            />
          </div>
        )
      }
    />
  )
}

function SlidersGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path
        d="M4 7h9m4 0h3M4 17h3m4 0h9M15 5v4M9 15v4"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
      />
    </svg>
  )
}
