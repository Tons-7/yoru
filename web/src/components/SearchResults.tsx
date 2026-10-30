import { Fragment } from 'react'
import { Link } from 'react-router-dom'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api, query, statusLabel, type LibraryItem } from '../lib/api'
import { cx, tint } from '../lib/format'
import { libraryFirst, rowFacts, type SearchRow } from '../lib/searchRows'
import { PER_PAGE, useBrowse } from './browseQuery'
import { Badge, Button, Empty, ErrorState, Pager, Skeleton } from './ui'

const TONES: Record<string, 'accent' | 'success' | 'neutral'> = {
  CURRENT: 'accent',
  REPEATING: 'accent',
  COMPLETED: 'success',
}

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
  const { mine, rest } = libraryFirst(results.data?.items ?? [], withLocal ? local.data?.results : undefined, page)
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
      {mine.length > 0 && <Group title="In your library" rows={mine} onOpen={onOpen} />}
      {results.isError && !results.data ? (
        <ErrorState error={results.error} retry={() => results.refetch()} />
      ) : waiting ? (
        <Group title={mine.length > 0 ? 'All anime' : undefined} onOpen={onOpen} />
      ) : (
        rest.length > 0 && <Group title="All anime" aside={total} rows={rest} onOpen={onOpen} />
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
  rows,
  onOpen,
}: {
  title?: string
  aside?: string
  rows?: SearchRow[]
  onOpen: () => void
}) {
  return (
    <section>
      {title && (
        <div className="mb-1.5 flex items-baseline justify-between gap-3">
          <h2 className="text-[13px] font-semibold text-base-400">{title}</h2>
          {aside && <span className="text-[13px] text-base-500 tabular-nums">{aside}</span>}
        </div>
      )}
      <ul className="grid gap-x-6 gap-y-1 sm:grid-cols-2 xl:grid-cols-3 min-[2200px]:grid-cols-4">
        {rows
          ? rows.map((row) => (
              <li key={row.id}>
                <ResultRow row={row} onOpen={onOpen} />
              </li>
            ))
          : Array.from({ length: 12 }, (_, i) => (
              <li key={i} className="flex items-center gap-3 py-1.5">
                <Skeleton className="aspect-[2/3] w-11 shrink-0 rounded-md" />
                <span className="flex-1 space-y-2">
                  <Skeleton className="h-4 w-3/4" />
                  <Skeleton className="h-3 w-1/3" />
                </span>
              </li>
            ))}
      </ul>
    </section>
  )
}

export function ResultRow({ row, onOpen }: { row: SearchRow; onOpen?: () => void }) {
  const facts = rowFacts(row)
  return (
    <Link
      to={`/anime/${row.id}`}
      onClick={onOpen}
      className="press -mx-2 flex items-center gap-3 rounded-card px-2 py-1.5 transition-colors hover:bg-base-900 focus-visible:outline-2 focus-visible:outline-white"
    >
      <span
        className="aspect-[2/3] w-11 shrink-0 overflow-hidden rounded-md bg-base-850"
        style={row.color ? { backgroundColor: tint(row.color, 0.3) } : undefined}
      >
        {row.thumb && <img src={row.thumb} alt="" loading="lazy" decoding="async" className="size-full object-cover" />}
      </span>
      <span className="min-w-0 flex-1">
        <span className="line-clamp-2 text-[15px] leading-snug font-semibold text-base-100">{row.title}</span>
        {facts.length > 0 && (
          <span className="mt-0.5 block text-[13px] text-base-400 tabular-nums">
            {facts.map((fact, i) => (
              <Fragment key={i}>
                {i > 0 && ' · '}
                <span className={fact.airing ? 'text-sky-300' : undefined}>{fact.text}</span>
              </Fragment>
            ))}
          </span>
        )}
      </span>
      {row.listStatus && <Badge tone={TONES[row.listStatus] ?? 'neutral'}>{statusLabel(row.listStatus)}</Badge>}
    </Link>
  )
}
