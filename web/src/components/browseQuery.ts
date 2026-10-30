import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api, ApiError, query, type DiscoverItem } from '../lib/api'
import { useDebounced } from '../lib/hooks'

export const PER_PAGE = 42

export const FILTER_KEYS = ['genres', 'formats', 'statuses', 'studio', 'studioName', 'year', 'season']

export function csv(value?: string): string[] {
  return value ? value.split(',').filter(Boolean) : []
}

export function useBrowse(params: Record<string, string>, enabled = true, keep = false) {
  const page = Number(params.page ?? 1)
  const unpaged = Object.fromEntries(Object.entries(params).filter(([k]) => k !== 'page'))

  const results = useQuery({
    queryKey: ['browse', params],
    queryFn: ({ signal }) =>
      api.get<{ items: DiscoverItem[]; total?: number; hasMore: boolean }>(
        `/api/browse${query({ ...params, perPage: PER_PAGE })}`,
        signal,
      ),
    enabled,
    placeholderData: keep ? keepPreviousData : undefined,
  })

  const unpagedKey = JSON.stringify(unpaged)
  const settled = useDebounced(unpagedKey, 800) === unpagedKey
  const counted = useQuery({
    queryKey: ['browse-count', unpaged],
    queryFn: ({ signal }) =>
      api.get<{ count: number; capped: boolean }>(`/api/browse/count${query({ ...unpaged, perPage: PER_PAGE })}`, signal),
    enabled: enabled && settled && !!results.data?.hasMore && !params.studio,
    staleTime: 30 * 60_000,
    retry: (n, err) => err instanceof ApiError && err.status === 503 && n < 48,
    retryDelay: 5000,
  })

  const count = params.studio
    ? results.data?.total || undefined
    : results.data && !results.data.hasMore
      ? (page - 1) * PER_PAGE + results.data.items.length
      : counted.data?.count
  const lastPage =
    results.data && !results.data.hasMore
      ? page
      : count === undefined
        ? undefined
        : Math.max(page, Math.ceil(count / PER_PAGE))

  return { results, page, count, capped: !!counted.data?.capped, lastPage }
}
