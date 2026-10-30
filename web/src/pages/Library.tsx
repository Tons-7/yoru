import { useEffect, useState, type ReactNode } from 'react'
import { useSearchParams } from 'react-router-dom'
import { statusLabel, type LibraryItem } from '../lib/api'
import { useLibraryCounts } from '../lib/queries'
import { PosterGrid } from '../components/PosterCard'
import { LibraryCard } from '../components/LibraryCard'
import { LibraryRows, LibraryTable } from '../components/LibraryList'
import { DesktopControls, PhoneControls, SearchToggle } from '../components/LibraryControls'
import { Button, Empty, ErrorState, LinkButton, Pager, RefreshFailed, SearchInput, Skeleton } from '../components/ui'
import { SwitchHeader, ViewSwitch } from '../components/ViewSwitch'
import { useClampPage, useDebounced, useMediaQuery } from '../lib/hooks'
import {
  countFor,
  DEFAULT_SORT,
  hiddenKey,
  STATUS_ORDER,
  useGridColumns,
  useLayout,
  useLibraryItems,
  type Counts,
  type Layout,
} from '../lib/library'
import { useHiddenKeys } from '../lib/undo'
import { useSide, type Sides } from '../lib/view'
import { HistoryView } from './History'

const LEGACY_FAVOURITES = 'favourites'
const ABOUT_PER_PAGE = 60

type Side = 'list' | 'history'

const SIDES: Sides<Side> = {
  store: 'yoru.library.view',
  base: 'list',
  alt: 'history',
  names: { list: 'list', history: 'history' },
  implied: (params) =>
    params.has('status') || params.has('q') || params.has('sort') || params.has('fav') ? 'list' : null,
}

const OPTIONS = [
  { value: 'list', label: 'My list' },
  { value: 'history', label: 'History' },
] as const

export function Library() {
  const [side, setSide] = useSide(SIDES)
  const switcher = (
    <ViewSwitch<Side> options={OPTIONS} value={side} onChange={setSide} className="sm:w-60" />
  )
  return side === 'history' ? <HistoryView switcher={switcher} /> : <MyList switcher={switcher} />
}

function MyList({ switcher }: { switcher: ReactNode }) {
  const [params, setParams] = useSearchParams()
  const wide = useMediaQuery('(min-width: 40rem)')
  const [layout, setLayout] = useLayout()
  const raw = params.get('status') ?? ''
  const fav = raw === LEGACY_FAVOURITES || params.get('fav') === '1'
  const status = raw === LEGACY_FAVOURITES ? '' : raw
  const sort = params.get('sort') ?? DEFAULT_SORT
  const page = Number(params.get('page') ?? 1)

  const [text, setText] = useState(params.get('q') ?? '')
  const q = useDebounced(text.trim(), 250)
  useEffect(() => {
    const next = new URLSearchParams(params)
    if (q) next.set('q', q)
    else next.delete('q')
    if ((params.get('q') ?? '') !== q) next.delete('page')
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q])
  const fromURL = params.get('q') ?? ''
  useEffect(() => {
    if (fromURL !== q) setText(fromURL)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fromURL])
  const [searching, setSearching] = useState(false)
  const searchOpen = searching || !!fromURL

  const counts = useLibraryCounts().data as Counts | undefined
  const hidden = useHiddenKeys()
  const sectioned = layout === 'grid' && !status && !q && !fav
  const columns = useGridColumns()
  const perPage = layout === 'grid' ? columns * Math.round(ABOUT_PER_PAGE / columns) : ABOUT_PER_PAGE
  const list = useLibraryItems({ status, q, sort, fav }, page, perPage, !sectioned)
  const { data, isPending, isError, error, refetch } = list
  const items = (data?.items ?? []).filter((it) => !hidden.has(hiddenKey(it.id)))
  const mixed = !status && (!!q || fav || layout === 'list')

  const change = (edit: (next: URLSearchParams) => void) => {
    const next = new URLSearchParams(params)
    if (next.get('status') === LEGACY_FAVOURITES) {
      next.delete('status')
      next.set('fav', '1')
    }
    edit(next)
    next.delete('page')
    setParams(next)
  }
  const setStatus = (value: string) =>
    change((next) => {
      if (value) next.set('status', value)
      else next.delete('status')
    })
  const setFav = (on: boolean) =>
    change((next) => {
      if (on) next.set('fav', '1')
      else next.delete('fav')
    })
  const setSort = (value: string) =>
    change((next) => {
      if (value && value !== DEFAULT_SORT) next.set('sort', value)
      else next.delete('sort')
    })
  const setPage = (n: number, replace = false) => {
    const next = new URLSearchParams(params)
    next.set('page', String(n))
    setParams(next, { replace })
  }
  useClampPage(
    sectioned ? page > 1 : data?.items.length === 0,
    page,
    sectioned ? 0 : data?.total,
    data?.perPage ?? perPage,
    (n) => setPage(n, true),
  )

  const controls = {
    status,
    fav,
    sort,
    layout,
    counts,
    text,
    onText: setText,
    onStatus: setStatus,
    onFav: setFav,
    onSort: setSort,
    onLayout: setLayout,
  }
  const total = counts?.total

  return (
    <div className="space-y-5">
      <SwitchHeader
        title="Library"
        meta={total !== undefined ? `${total.toLocaleString()} ${total === 1 ? 'show' : 'shows'}` : undefined}
        switcher={switcher}
        actions={
          !wide && (
            <SearchToggle
              open={searchOpen}
              onToggle={() => {
                if (searchOpen) {
                  setSearching(false)
                  setText('')
                } else setSearching(true)
              }}
            />
          )
        }
      />

      {wide ? (
        <DesktopControls {...controls} />
      ) : (
        <div className="space-y-3">
          {searchOpen && (
            <div className="flex items-center gap-2">
              <SearchInput
                autoFocus={searching}
                value={text}
                onChange={(e) => setText(e.target.value)}
                placeholder="Search your library"
                aria-label="Filter library"
                className="min-w-0 flex-1"
              />
              <Button
                variant="ghost"
                onClick={() => {
                  setSearching(false)
                  setText('')
                }}
              >
                Cancel
              </Button>
            </div>
          )}
          <PhoneControls {...controls} />
        </div>
      )}

      {sectioned ? (
        <Sections counts={counts} sort={sort} onShowAll={setStatus} />
      ) : (
        <>
          {isError && data && <RefreshFailed retry={() => refetch()} />}
          {isError && !data ? (
            <ErrorState error={error} retry={() => refetch()} />
          ) : isPending ? (
            <Placeholder layout={layout} wide={wide} />
          ) : items.length === 0 ? (
            <Nothing status={status} fav={fav} q={q} counts={counts} onStatus={setStatus} onFav={setFav} />
          ) : (
            <>
              {layout === 'list' ? (
                wide ? (
                  <LibraryTable items={items} sort={sort} onSort={setSort} />
                ) : (
                  <LibraryRows items={items} showStatus={mixed} />
                )
              ) : (
                <PosterGrid>
                  {items.map((item) => (
                    <LibraryCard key={item.id} item={item} showStatus={mixed} />
                  ))}
                </PosterGrid>
              )}
              <Pager page={page} hasMore={!!data?.hasMore} onPage={setPage} />
            </>
          )}
        </>
      )}
    </div>
  )
}

function Sections({ counts, sort, onShowAll }: { counts?: Counts; sort: string; onShowAll: (status: string) => void }) {
  const cols = useGridColumns()
  if (!counts) return <Placeholder layout="grid" wide={false} />
  const filled = STATUS_ORDER.filter((s) => (counts.statuses[s] ?? 0) > 0)
  if (filled.length === 0) return <Nothing status="" fav={false} q="" counts={counts} onStatus={onShowAll} onFav={() => {}} />
  return (
    <div className="space-y-8">
      {filled.map((s) => (
        <Section key={s} status={s} count={counts.statuses[s]} sort={sort} cols={cols} onShowAll={() => onShowAll(s)} />
      ))}
    </div>
  )
}

function Section({
  status,
  count,
  sort,
  cols,
  onShowAll,
}: {
  status: string
  count: number
  sort: string
  cols: number
  onShowAll: () => void
}) {
  const hidden = useHiddenKeys()
  const { data, isPending, isError, refetch } = useLibraryItems({ status, q: '', sort, fav: false }, 1, cols * 2)
  const items = (data?.items ?? []).filter((it) => !hidden.has(hiddenKey(it.id))).slice(0, cols * 2)
  const heading = `section-${status}`
  return (
    <section aria-labelledby={heading}>
      <h2 id={heading} className="mb-3 flex items-baseline gap-2 text-lg font-semibold text-white">
        {statusLabel(status)}
        <span className="text-sm font-normal text-base-500 tabular-nums">{count.toLocaleString()}</span>
      </h2>
      {isError && !data ? (
        <p className="flex items-center gap-3 text-sm text-base-400">
          Couldn't load these.
          <Button size="sm" onClick={() => refetch()}>
            Try again
          </Button>
        </p>
      ) : isPending ? (
        <GridSkeleton n={Math.min(count, cols * 2)} />
      ) : (
        <PosterGrid>
          {items.map((item: LibraryItem) => (
            <LibraryCard key={item.id} item={item} />
          ))}
        </PosterGrid>
      )}
      {count > items.length && !isPending && (
        <Button className="mt-4 w-full sm:w-auto" onClick={onShowAll}>
          Show all {count.toLocaleString()}
        </Button>
      )}
    </section>
  )
}

function GridSkeleton({ n }: { n: number }) {
  return (
    <PosterGrid>
      {Array.from({ length: n }, (_, i) => (
        <div key={i}>
          <Skeleton className="aspect-[2/3] w-full" />
          <Skeleton className="mt-2 h-4 w-3/4" />
          <Skeleton className="mt-1.5 h-3 w-1/2" />
        </div>
      ))}
    </PosterGrid>
  )
}

function Placeholder({ layout, wide }: { layout: Layout; wide: boolean }) {
  return (
    <div className="max-h-[calc(100dvh-14rem)] overflow-hidden">
      {layout === 'grid' ? (
        <GridSkeleton n={40} />
      ) : (
        <div className="space-y-2">
          {Array.from({ length: 20 }, (_, i) => (
            <Skeleton key={i} className={wide ? 'h-14 w-full' : 'h-20 w-full'} />
          ))}
        </div>
      )}
    </div>
  )
}

const EMPTY: Record<string, { title: string; hint: string }> = {
  CURRENT: { title: "Nothing you're watching", hint: 'Shows you start go here.' },
  PLANNING: { title: 'Nothing planned', hint: 'Bookmark a show to plan it.' },
  REPEATING: { title: "Nothing you're rewatching", hint: 'Pick Rewatching on a finished show to watch it again.' },
  PAUSED: { title: 'Nothing on hold', hint: 'Shows you pause go here.' },
  COMPLETED: { title: 'Nothing completed yet', hint: 'Shows you finish go here.' },
  DROPPED: { title: 'Nothing dropped', hint: 'Shows you stop watching go here.' },
}

function Nothing({
  status,
  fav,
  q,
  counts,
  onStatus,
  onFav,
}: {
  status: string
  fav: boolean
  q: string
  counts?: Counts
  onStatus: (status: string) => void
  onFav: (on: boolean) => void
}) {
  if (q) {
    return (
      <Empty
        title="No match in your list"
        hint="Try another spelling, or search all of AniList."
        action={<LinkButton to={`/browse?q=${encodeURIComponent(q)}`}>Search AniList</LinkButton>}
      />
    )
  }
  if (fav && status) {
    return (
      <Empty
        title={`No favourites in ${statusLabel(status)}`}
        hint="Favourites only is on, so the rest of this status is hidden."
        action={<Button onClick={() => onFav(false)}>Show all of {statusLabel(status)}</Button>}
      />
    )
  }
  if (fav) {
    return (
      <Empty
        title="No favourites yet"
        hint="Tap the heart on a show to keep it here."
        action={<LinkButton to="/browse">Browse anime</LinkButton>}
      />
    )
  }
  if (status) {
    const words = EMPTY[status] ?? { title: 'Nothing here', hint: 'Tag a show with this status and it will appear here.' }
    const watching = countFor(counts, 'CURRENT', false) ?? 0
    const all = counts?.total ?? 0
    return (
      <Empty
        title={words.title}
        hint={words.hint}
        action={
          status !== 'CURRENT' && watching > 0 ? (
            <Button onClick={() => onStatus('CURRENT')}>Go to Watching</Button>
          ) : all > 0 ? (
            <Button onClick={() => onStatus('')}>Show all</Button>
          ) : (
            <LinkButton to="/browse">Browse anime</LinkButton>
          )
        }
      />
    )
  }
  return (
    <Empty
      title="Nothing here yet"
      hint="Connect AniList in settings to import your list, or browse and tag shows yourself."
      action={
        <LinkButton to="/settings?tab=Accounts" variant="primary">
          Connect a tracker
        </LinkButton>
      }
    />
  )
}
