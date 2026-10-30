import { useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery } from '@tanstack/react-query'
import { api, query, type DiscoverItem } from '../lib/api'
import { toCard } from '../lib/cards'
import { cx } from '../lib/format'
import { useDebounced, useMediaQuery } from '../lib/hooks'
import { useAlertsOff, useFilters } from '../lib/queries'
import { forgetSearch, rememberSearch, useRecentSearches } from '../lib/recentSearches'
import { openableLink, openLinkPath, openLinkWords } from '../lib/showLink'
import { csv, FILTER_KEYS, PER_PAGE, useBrowse } from '../components/browseQuery'
import { FilterMenu } from '../components/FilterMenu'
import { FilterSheet, SortSheet, type FilterVocabulary } from '../components/FilterSheet'
import { PosterCard, PosterGrid } from '../components/PosterCard'
import { SearchResults } from '../components/SearchResults'
import { GenreTiles, RecentSearches } from '../components/SearchStart'
import { StudioFilter } from '../components/StudioFilter'
import { START_GROUPS, startGroup } from '../components/upcoming'
import { Button, Empty, ErrorState, LinkButton, Pager, Skeleton } from '../components/ui'

const SORT_LABELS: Record<string, string> = {
  popular: 'Most popular',
  trending: 'Trending',
  score: 'Highest rated',
  favourites: 'Most favourited',
  newest: 'Newest',
  oldest: 'Oldest',
  title: 'Title A–Z',
  episodes: 'Most episodes',
}

const SHORT_SORT: Record<string, string> = {
  popular: 'Popular',
  score: 'Top rated',
  favourites: 'Favourites',
  title: 'A–Z',
  episodes: 'Episodes',
}

const pretty = (v: string) => v.replace(/_/g, ' ').toLowerCase().replace(/^\w/, (c) => c.toUpperCase())

const asked = (text: string) => (text.trim().length >= 2 ? text : '')

export function Browse() {
  const [params, setParams] = useSearchParams()
  const [text, setText] = useState(params.get('q') ?? '')
  const debounced = useDebounced(text, 250)
  const wide = useMediaQuery('(min-width: 40rem)')
  const [sheet, setSheet] = useState<'filters' | 'sort' | null>(null)
  const [focused, setFocused] = useState(false)
  const form = useRef<HTMLFormElement>(null)
  const alertsOff = useAlertsOff()
  const recent = useRecentSearches()

  const filters = useFilters()
  const vocab = filters.data as unknown as FilterVocabulary | undefined
  const catalogue = useQuery({
    queryKey: ['corpus'],
    queryFn: ({ signal }) => api.get<{ anime: number }>('/api/corpus', signal),
    staleTime: 6 * 60 * 60_000,
  })
  const held = catalogue.data?.anime ?? 0
  const placeholder = held >= 1000 ? `Search ${(Math.floor(held / 1000) * 1000).toLocaleString()} anime` : 'Search anime'

  const writeQuery = (value: string) => {
    const q = asked(value)
    const next = new URLSearchParams(params)
    if (q) next.set('q', q)
    else next.delete('q')
    if (next.get('q') !== params.get('q')) next.delete('page')
    if (next.toString() !== params.toString()) setParams(next, { replace: true })
  }

  const fromURL = params.get('q') ?? ''
  const [seen, setSeen] = useState(fromURL)
  if (fromURL !== seen) {
    setSeen(fromURL)
    if (fromURL !== asked(debounced) && fromURL !== asked(text)) setText(fromURL)
  }
  const first = useRef(true)
  const arrived = useEffectEvent((q: string) => {
    if (first.current || q !== asked(debounced)) rememberSearch(q)
    first.current = false
  })
  useEffect(() => arrived(fromURL), [fromURL])

  const writeLater = useEffectEvent((value: string) => {
    if (!openableLink(value)) writeQuery(value)
  })
  useEffect(() => writeLater(debounced), [debounced])

  const link = openableLink(text)
  const searching = asked(text) !== '' && !link
  const active = useMemo(() => Object.fromEntries(params.entries()), [params])
  const gridParams = useMemo(() => Object.fromEntries(Object.entries(active).filter(([k]) => k !== 'q')), [active])
  const { results, page, count: resultCount, capped, lastPage } = useBrowse(gridParams, !searching)

  const navigate = useNavigate()
  const random = useMutation({
    meta: { inline: true },
    mutationFn: () => {
      const formats = csv(active.formats)
      return api.get<{ id: number }>(
        `/api/random${query({
          genres: active.genres,
          format: formats.length ? formats[Math.floor(Math.random() * formats.length)] : undefined,
          year: active.year,
        })}`,
      )
    },
    onSuccess: (r) => navigate(`/anime/${r.id}`),
  })
  const openLink = (value: string) => navigate(openLinkPath(value))

  const set = (key: string, value?: string) => {
    const next = new URLSearchParams(params)
    if (value) next.set(key, value)
    else next.delete(key)
    next.delete('page')
    setParams(next)
  }

  const goToPage = (n: number) => {
    const next = new URLSearchParams(params)
    next.set('page', String(n))
    setParams(next)
  }

  const clearFilters = () => {
    const next = new URLSearchParams()
    if (active.q) next.set('q', active.q)
    if (active.sort) next.set('sort', active.sort)
    setParams(next)
  }

  const setStudio = (s?: { id: number; name: string }) => {
    const next = new URLSearchParams(params)
    if (s) {
      next.set('studio', String(s.id))
      next.set('studioName', s.name)
    } else {
      next.delete('studio')
      next.delete('studioName')
    }
    next.delete('page')
    setParams(next)
  }

  const applySheet = (draft: Record<string, string>) => {
    const next = new URLSearchParams()
    if (active.q) next.set('q', active.q)
    for (const [key, value] of Object.entries(draft)) if (value) next.set(key, value)
    setParams(next)
    setSheet(null)
  }

  const toggleGenre = (genre: string) => {
    const now = csv(active.genres)
    set('genres', (now.includes(genre) ? now.filter((g) => g !== genre) : [...now, genre]).join(','))
  }

  const field = () => form.current?.querySelector('input')
  const pickRecent = (search: string) => {
    setText(search)
    writeQuery(search)
    rememberSearch(search)
  }
  const cancel = () => {
    setText('')
    writeQuery('')
    field()?.blur()
  }
  const clear = () => {
    setText('')
    writeQuery('')
    field()?.focus()
  }

  const without = (key: string, value: string) => () =>
    set(key, csv(active[key]).filter((x) => x !== value).join(','))
  const chips: Array<{ key: string; value: string; label: string; remove: () => void }> = [
    ...csv(active.genres).map((g) => ({ key: 'genres', value: g, label: g, remove: without('genres', g) })),
    ...csv(active.formats).map((f) => ({ key: 'formats', value: f, label: f.replace('_', ' '), remove: without('formats', f) })),
    ...csv(active.statuses).map((s) => ({ key: 'statuses', value: s, label: pretty(s), remove: without('statuses', s) })),
    ...(active.studio
      ? [{ key: 'studio', value: active.studio, label: `Studio: ${active.studioName ?? active.studio}`, remove: () => setStudio() }]
      : []),
    ...(active.year ? [{ key: 'year', value: active.year, label: active.year, remove: () => set('year') }] : []),
    ...(active.season ? [{ key: 'season', value: active.season, label: pretty(active.season), remove: () => set('season') }] : []),
  ]
  const filtered = FILTER_KEYS.some((k) => active[k])

  const sortLabel = active.sort
    ? (SHORT_SORT[active.sort] ?? SORT_LABELS[active.sort] ?? pretty(active.sort))
    : searching
      ? 'Best match'
      : 'Popular'
  const countText =
    results.data && results.data.items.length > 0
      ? resultCount !== undefined
        ? `${resultCount.toLocaleString()}${capped ? '+' : ''} ${resultCount === 1 ? 'result' : 'results'}`
        : results.data.hasMore
          ? `${(page * PER_PAGE).toLocaleString()}+ results`
          : null
      : null

  const chipRow = (count: string | null) => (
    <div className={cx('flex items-center gap-2', wide ? 'flex-wrap' : 'no-scrollbar -mx-4 overflow-x-auto px-4 py-1')}>
      {chips.map((c) => (
        <button
          key={`${c.key}-${c.value}`}
          type="button"
          onClick={c.remove}
          aria-label={`Remove ${c.label}`}
          className="tap relative flex h-9 shrink-0 items-center gap-1.5 rounded-poster bg-accent-500/15 pr-2 pl-3 text-[13px] font-medium whitespace-nowrap text-accent-300 transition-colors hover:bg-accent-500/25"
        >
          {c.label}
          <CrossGlyph />
        </button>
      ))}
      {(wide ? chips.length > 1 : chips.length > 0) && (
        <button
          type="button"
          onClick={clearFilters}
          className="tap relative h-9 shrink-0 px-2 text-[13px] font-medium whitespace-nowrap text-base-400 transition-colors hover:text-white"
        >
          Clear all
        </button>
      )}
      {count && (
        <span className={cx('shrink-0 text-[13px] whitespace-nowrap text-base-500 tabular-nums', wide && 'ml-auto')}>{count}</span>
      )}
    </div>
  )

  const showCancel = !wide && focused

  return (
    <div className="flex flex-col gap-4 sm:gap-5">
      <div className="flex items-center justify-between gap-3">
        <h1 className="page-title">Search</h1>
        <Button
          onClick={() => random.mutate()}
          disabled={random.isPending}
          title="A random anime matching the genre, format and year chosen"
        >
          <ShuffleIcon />
          {random.isPending ? 'Picking…' : 'Surprise me'}
        </Button>
      </div>

      <form
        ref={form}
        role="search"
        onSubmit={(e) => {
          e.preventDefault()
          if (link) return void openLink(text)
          writeQuery(text)
          rememberSearch(text)
          field()?.blur()
        }}
        className="sticky top-[var(--bar,4rem)] z-30 -mx-4 -my-2 bg-base-950 px-4 py-2 transition-[top] duration-200 sm:static sm:m-0 sm:max-w-2xl sm:bg-transparent sm:p-0"
      >
        <div className="relative">
          <svg
            viewBox="0 0 24 24"
            aria-hidden
            className="pointer-events-none absolute top-1/2 left-3.5 size-[18px] -translate-y-1/2 text-base-500"
          >
            <circle cx="11" cy="11" r="6.5" fill="none" stroke="currentColor" strokeWidth="2" />
            <path d="m16 16 4.5 4.5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
          </svg>
          <input
            type="search"
            value={text}
            onChange={(e) => {
              const value = e.target.value
              const pasted = (e.nativeEvent as InputEvent).inputType === 'insertFromPaste' || value.length - text.length > 1
              if (pasted && openableLink(value)) void openLink(value)
              else setText(value)
            }}
            onFocus={() => setFocused(true)}
            onBlur={() => setFocused(false)}
            placeholder={placeholder}
            aria-label="Search"
            enterKeyHint="search"
            autoComplete="off"
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
            className={cx(
              'h-12 w-full rounded-xl bg-base-800 pl-10 text-base text-base-100 transition-[box-shadow] placeholder:text-base-500 focus:ring-2 focus:ring-accent-300 focus:outline-hidden [&::-webkit-search-cancel-button]:hidden',
              showCancel ? 'pr-20' : text ? 'pr-12' : 'pr-3',
            )}
          />
          {showCancel ? (
            <button
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={cancel}
              className="absolute inset-y-0 right-0 rounded-r-xl px-4 text-[15px] font-medium text-accent-300"
            >
              Cancel
            </button>
          ) : (
            text && (
              <button
                type="button"
                onClick={clear}
                aria-label="Clear search"
                title="Clear search"
                className="absolute inset-y-0 right-0 grid w-12 place-items-center rounded-r-xl text-base-500 transition-colors hover:text-white"
              >
                <CrossGlyph />
              </button>
            )
          )}
        </div>
        {link && (
          <button
            type="submit"
            onMouseDown={(e) => e.preventDefault()}
            className="mt-2 flex min-h-11 w-full items-center gap-2 rounded-xl px-3 text-left text-sm font-medium text-accent-300 transition-colors hover:bg-base-800"
          >
            <span className="min-w-0 flex-1 truncate">{openLinkWords(link)}</span>
            <span aria-hidden className="text-base-500">
              ›
            </span>
          </button>
        )}
      </form>

      {random.isError && (
        <p className="flex flex-wrap items-center gap-3 text-[13px] text-red-300">
          {(random.error as Error).message}
          <Button size="sm" onClick={() => random.mutate()}>
            Try again
          </Button>
        </p>
      )}

      {!searching && recent.length > 0 && <RecentSearches searches={recent} onPick={pickRecent} onForget={forgetSearch} />}
      {!searching && <GenreTiles chosen={csv(active.genres)} onToggle={toggleGenre} />}

      {wide ? (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <FilterMenu
              label="Genre"
              values={csv(active.genres)}
              options={vocab?.genres ?? []}
              onChange={(v) => set('genres', v.join(','))}
            />
            <FilterMenu
              label="Format"
              values={csv(active.formats)}
              options={vocab?.formats ?? []}
              onChange={(v) => set('formats', v.join(','))}
            />
            <FilterMenu
              label="Status"
              values={csv(active.statuses)}
              options={vocab?.statuses ?? []}
              onChange={(v) => set('statuses', v.join(','))}
            />
            <StudioFilter
              value={active.studio ? { id: Number(active.studio), name: active.studioName ?? 'Studio' } : undefined}
              onChange={setStudio}
            />
            <FilterMenu
              label="Year"
              multiple={false}
              values={csv(active.year)}
              options={(vocab?.years ?? []).map(String)}
              onChange={(v) => set('year', v[0])}
            />
            <FilterMenu
              label="Season"
              multiple={false}
              values={csv(active.season)}
              options={vocab?.seasons ?? []}
              onChange={(v) => set('season', v[0])}
            />
            <div className="ml-auto flex shrink-0 items-center gap-2">
              <span className="text-[13px] font-medium text-base-500">Sort</span>
              <FilterMenu
                label={searching ? 'Best match' : 'Most popular'}
                multiple={false}
                values={csv(active.sort)}
                options={vocab?.sorts ?? []}
                labels={SORT_LABELS}
                onChange={(v) => set('sort', v[0])}
              />
            </div>
          </div>
          {chips.length > 0
            ? chipRow(searching ? null : countText)
            : !searching && countText && <p className="text-[13px] text-base-500 tabular-nums">{countText}</p>}
        </div>
      ) : (
        <>
          <div className="sticky top-[calc(var(--bar,4rem)+4rem)] z-20 -mx-4 flex gap-2 bg-base-950 px-4 py-2 transition-[top] duration-200">
            <Button className="min-w-0 flex-1" onClick={() => setSheet('filters')} aria-haspopup="dialog">
              <FilterGlyph />
              <span className="tabular-nums">{chips.length > 0 ? `Filters · ${chips.length}` : 'Filters'}</span>
            </Button>
            <Button className="min-w-0 flex-1" onClick={() => setSheet('sort')} aria-haspopup="dialog">
              <SortGlyph />
              <span className="truncate">Sort: {sortLabel}</span>
            </Button>
          </div>
          {chips.length > 0 && chipRow(searching ? null : countText)}
        </>
      )}

      {searching ? (
        <SearchResults
          params={active}
          filtered={filtered}
          onPage={goToPage}
          onOpen={() => rememberSearch(text)}
          onClearFilters={clearFilters}
        />
      ) : (
        <>
          {results.isError && !results.data ? (
            <ErrorState error={results.error} retry={() => results.refetch()} />
          ) : results.isPending ? (
            <div className="max-h-[calc(100dvh-14rem)] overflow-hidden">
              <PosterGrid>
                {Array.from({ length: 40 }, (_, i) => (
                  <div key={i}>
                    <Skeleton className="aspect-[2/3] w-full" />
                    <Skeleton className="mt-2 h-4 w-3/4" />
                  </div>
                ))}
              </PosterGrid>
            </div>
          ) : results.data.items.length === 0 ? (
            <Empty
              title="Nothing matched"
              hint={filtered ? 'Try removing a filter.' : undefined}
              action={filtered ? <Button onClick={clearFilters}>Clear filters</Button> : undefined}
            />
          ) : (
            <>
              {results.data.items.every((a) => a.status === 'NOT_YET_RELEASED') ? (
                <Upcoming items={results.data.items} alertsOff={alertsOff} />
              ) : (
                <PosterGrid>
                  {results.data.items.map((anime) => (
                    <PosterCard key={anime.id} anime={toCard(anime)} />
                  ))}
                </PosterGrid>
              )}

              <Pager page={page} hasMore={results.data.hasMore} last={lastPage} onPage={goToPage} />
            </>
          )}
        </>
      )}

      {sheet === 'filters' && (
        <FilterSheet params={gridParams} vocab={vocab} onApply={applySheet} onClose={() => setSheet(null)} />
      )}
      {sheet === 'sort' && (
        <SortSheet
          value={active.sort}
          options={vocab?.sorts ?? []}
          labels={SORT_LABELS}
          searching={searching}
          onChange={(v) => set('sort', v)}
          onClose={() => setSheet(null)}
        />
      )}
    </div>
  )
}

function Upcoming({ items, alertsOff }: { items: DiscoverItem[]; alertsOff: boolean }) {
  return (
    <div className="space-y-8">
      {alertsOff && (
        <p className="surface flex flex-wrap items-center justify-between gap-2 px-4 py-2.5 text-sm text-base-300">
          Alerts are off for every show, so you can't set one here.
          <LinkButton to="/settings?tab=Notifications" size="sm">
            Turn alerts on
          </LinkButton>
        </p>
      )}
      {START_GROUPS.map((group) => {
        const shows = items.filter((a) => startGroup(a.startDate) === group)
        if (shows.length === 0) return null
        return (
          <section key={group}>
            <h2 className="section-title mb-3">{group}</h2>
            <PosterGrid>
              {shows.map((anime) => (
                <PosterCard key={anime.id} anime={toCard(anime)} alert={!alertsOff} />
              ))}
            </PosterGrid>
          </section>
        )
      })}
    </div>
  )
}

function ShuffleIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path
        d="M4 7h3.5c1.4 0 2.6.7 3.4 1.8l2.2 3.4c.8 1.1 2 1.8 3.4 1.8H20M4 17h3.5c1.4 0 2.6-.7 3.4-1.8l2.2-3.4c.8-1.1 2-1.8 3.4-1.8H20M17 4l3 3-3 3M17 14l3 3-3 3"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function FilterGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0" aria-hidden>
      <path d="M4 6h16M7 12h10M10 18h4" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  )
}

function SortGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0" aria-hidden>
      <path
        d="M7 4v16m0 0-3-3m3 3 3-3M17 20V4m0 0-3 3m3-3 3 3"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function CrossGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-3.5 shrink-0 opacity-80" aria-hidden>
      <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />
    </svg>
  )
}
