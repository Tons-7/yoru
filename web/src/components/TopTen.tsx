import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { toCard } from '../lib/cards'
import { cx } from '../lib/format'
import { discoverQuery, useDiscover } from '../lib/queries'
import { PosterCard } from './PosterCard'
import { Rail, RailFailed } from './Rail'
import { Segmented, Skeleton } from './ui'

const PERIODS = [
  { value: 'trending', label: 'Today' },
  { value: 'week', label: 'Week' },
  { value: 'month', label: 'Month' },
  { value: 'popular', label: 'All time' },
] as const

type Period = (typeof PERIODS)[number]['value']

const TITLE = 'Top 10'
const TO = '/browse?sort=trending'

const count = (period: Period) => (period === 'trending' ? 20 : 10)

export function TopTen({ folded, onUnfold }: { folded: boolean; onUnfold: () => void }) {
  const [period, setPeriod] = useState<Period>('trending')
  const ranked = useDiscover(period, count(period), true)
  const qc = useQueryClient()
  const loaded = ranked.isSuccess && !folded
  useEffect(() => {
    if (!loaded) return
    const timer = window.setTimeout(() => {
      for (const p of PERIODS) void qc.prefetchQuery(discoverQuery(p.value, count(p.value)))
    }, 3000)
    return () => window.clearTimeout(timer)
  }, [loaded, qc])

  if (folded) return <Rail title={TITLE} to={TO} folded onUnfold={onUnfold}>{null}</Rail>
  if (ranked.isError && !ranked.data) return <RailFailed title={TITLE} to={TO} retry={() => void ranked.refetch()} />

  return (
    <Rail title={TITLE} to={TO} action={<Segmented size="sm" options={PERIODS} value={period} onChange={setPeriod} />}>
      {ranked.isPending
        ? Array.from({ length: 10 }, (_, i) => (
            <div key={i} className="w-[7.5rem] shrink-0 pl-12 box-content sm:w-[8.75rem]">
              <Skeleton className="aspect-[2/3] w-full" />
              <Skeleton className="mt-2 h-4 w-3/4" />
              <Skeleton className="mt-1.5 h-3 w-1/2" />
            </div>
          ))
        : (ranked.data?.items ?? []).slice(0, 10).map((anime, i) => (
            <div
              key={anime.id}
              className={cx('flex shrink-0 snap-start items-start transition-opacity duration-200', ranked.isPlaceholderData && 'opacity-50')}
            >
              <span
                aria-hidden
                className="flex h-[11.25rem] items-end font-display text-[5.5rem] leading-[0.78] font-extrabold tracking-tighter text-base-950 tabular-nums [paint-order:stroke_fill] [-webkit-text-stroke:4px_var(--color-base-500)] sm:h-[13.125rem] sm:text-[6.5rem]"
              >
                {i + 1}
              </span>
              <div className="relative -ml-3 w-[7.5rem] sm:w-[8.75rem]">
                <span className="sr-only">Number {i + 1}</span>
                <PosterCard anime={toCard(anime)} />
              </div>
            </div>
          ))}
    </Rail>
  )
}
