import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api, query, type DiscoverItem, type LibraryItem } from '../lib/api'
import { toCard, type CardAnime } from '../lib/cards'
import { cx } from '../lib/format'
import { libraryFirst, type SearchRow } from '../lib/searchRows'
import { PER_PAGE, useBrowse } from './browseQuery'
import { PosterCard, PosterGrid } from './PosterCard'
import { Button, Empty, ErrorState, Pager, Skeleton } from './ui'

export function SearchResults({
  params,
  filtered,
  onPage,
  onOpen,
  onClearFilters,
}: {
  params: Record<string, string>
  filtered: boolean
  onPage: (page: number) => void
  onOpen: () => void
  onClearFilters: () => void
}) {
  const q = (params.q ?? '').trim()
  const ready = q.length >= 2
  const { results, page, count, capped, lastPage } = useBrowse(params, ready, true)
  const withLocal = ready && !filtered && page === 1
  const local = useQuery({
    queryKey: ['search', 'library', q],
    queryFn: ({ signal }) => api.get<{ results: LibraryItem[] }>(`/api/search/local${query({ q, limit: 8 })}`, signal),
    enabled: withLocal,
    staleTime: 30_000,
    placeholderData: keepPreviousData,
  })
  const localHits = withLocal ? local.data?.results : undefined
  const shaped = libraryFirst(results.data?.items ?? [], localHits, page)
  const sources = new Map<number, DiscoverItem | LibraryItem>([
    ...(localHits ?? []).map((h) => [h.id, h] as const),
    ...(results.data?.items ?? []).map((a) => [a.id, a] as const),
  ])
  const asCards = (rows: SearchRow[]) =>
    rows.flatMap((row) => {
      const source = sources.get(row.id)
      return source ? [{ ...toCard(source), status: row.listStatus }] : []
    })
  const mine = asCards(shaped.mine)
  const rest = asCards(shaped.rest)
  const waiting = !ready || results.isPending
  const stale = results.isPlaceholderData || (withLocal && local.isPlaceholderData)
  const nothing =
    !waiting && !results.isError && !(withLocal && local.isPending) && mine.length === 0 && rest.length === 0
  const total =
    results.data && results.data.items.length > 0
      ? count !== undefined
        ? `${count.toLocaleString()}${capped ? '+' : ''} ${count === 1 ? 'result' : 'results'}`
        : results.data.hasMore
          ? `${(page * PER_PAGE).toLocaleString()}+ results`
          : undefined
      : undefined

  return (
    <div aria-busy={waiting || stale} className={cx('flex flex-col gap-5 transition-opacity', stale && 'opacity-60')}>
      {mine.length > 0 && <Group title="In your library" cards={mine} onOpen={onOpen} />}
      {results.isError && !results.data ? (
        <ErrorState error={results.error} retry={() => results.refetch()} />
      ) : waiting ? (
        <Group title={mine.length > 0 ? 'All anime' : undefined} onOpen={onOpen} />
      ) : (
        rest.length > 0 && <Group title="All anime" aside={total} cards={rest} onOpen={onOpen} />
      )}
      {nothing && (
        <Empty
          title={`Nothing found for “${q}”`}
          hint={filtered ? 'Check the spelling, or try removing a filter.' : 'Check the spelling, or try the Japanese title.'}
          action={filtered ? <Button onClick={onClearFilters}>Clear filters</Button> : undefined}
        />
      )}
      {results.data && (
        <Pager page={page} hasMore={results.data.hasMore && !(results.isPlaceholderData && page > 1)} last={lastPage} onPage={onPage} />
      )}
    </div>
  )
}

function Group({
  title,
  aside,
  cards,
  onOpen,
}: {
  title?: string
  aside?: string
  cards?: CardAnime[]
  onOpen: () => void
}) {
  return (
    <section>
      {title && (
        <div className="mb-3 flex items-baseline justify-between gap-3">
          <h2 className="text-[13px] font-semibold text-base-400">{title}</h2>
          {aside && <span className="text-[13px] text-base-500 tabular-nums">{aside}</span>}
        </div>
      )}
      <PosterGrid>
        {cards
          ? cards.map((card) => <PosterCard key={card.id} anime={card} onOpen={onOpen} />)
          : Array.from({ length: 12 }, (_, i) => (
              <div key={i}>
                <Skeleton className="aspect-[2/3] w-full" />
                <Skeleton className="mt-2 h-4 w-3/4" />
              </div>
            ))}
      </PosterGrid>
    </section>
  )
}
