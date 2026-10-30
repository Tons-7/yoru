import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
import { usePrefFlag } from '../lib/queries'
import { groupByDay } from '../lib/format'
import { byShow, RANGES, useOutNow, type Range } from '../lib/aired'
import { ReleasedCard } from '../components/ReleasedCard'
import { SwitchHeader } from '../components/ViewSwitch'
import { ChipDivider, ChipRow, ChoiceChips, ToggleChip } from '../components/Chips'
import { Empty, ErrorState, RefreshFailed, Skeleton } from '../components/ui'

export function RecentPage() {
  return <Navigate to="/schedule?view=out" replace />
}

const GRID =
  'grid grid-cols-2 gap-x-3 gap-y-4 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-5 min-[1800px]:grid-cols-6 min-[2200px]:grid-cols-7'

export function RecentView({
  range,
  onRange,
  switcher,
}: {
  range: Range
  onRange: (range: Range) => void
  switcher: ReactNode
}) {
  const [mine, setMine] = usePrefFlag('recent.mine')
  const { query, aired } = useOutNow(range)
  const { data, isPending, isError, error, refetch } = query
  const items = aired.filter((item) => !mine || item.onList)
  const days = groupByDay(items, (i) => i.airingAt).map(([label, group]) => [label, byShow(group)] as const)
  const shows = (list: typeof aired) => new Set(list.map((item) => item.animeId)).size
  const hidden = mine ? shows(aired) - shows(items) : 0

  return (
    <div className="space-y-4">
      <SwitchHeader beside title="Schedule" switcher={switcher} />
      <div className="space-y-1.5">
        <ChipRow>
          <ToggleChip on={mine} onChange={setMine}>
            Only my list
          </ToggleChip>
          <ChipDivider />
          <ChoiceChips label="How far back" options={RANGES} value={range} onChange={onRange} />
        </ChipRow>
        {hidden > 0 && (
          <p className="text-[13px] text-base-400">
            Your list · {shows(items)} of {shows(aired)} shows.{' '}
            <button
              type="button"
              onClick={() => setMine(false)}
              className="relative tap font-medium text-accent-300 underline-offset-2 hover:underline"
            >
              Show all
            </button>
          </p>
        )}
      </div>

      {isError && data && <RefreshFailed retry={() => refetch()} />}
      {isError && !data ? (
        <ErrorState error={error} retry={() => refetch()} />
      ) : isPending ? (
        <div className="max-h-[calc(100dvh-14rem)] overflow-hidden">
          <div className={GRID}>
            {Array.from({ length: 28 }, (_, i) => (
              <div key={i}>
                <Skeleton className="aspect-video w-full" />
                <Skeleton className="mt-2 h-4 w-3/4" />
                <Skeleton className="mt-1.5 h-3 w-1/2" />
              </div>
            ))}
          </div>
        </div>
      ) : items.length === 0 ? (
        <Empty
          title="Nothing has aired in this window"
          hint={mine ? 'Nothing from your list, at least. Try a wider window, or show every show.' : 'Try a wider window.'}
        />
      ) : (
        <div className="space-y-8">
          {days.map(([label, group]) => (
            <section key={label}>
              <h2 className="mb-3 section-title">
                {label}
                <span className="text-sm font-normal text-base-500">
                  {group.length} {group.length === 1 ? 'show' : 'shows'}
                </span>
              </h2>
              <div className={GRID}>
                {group.map((show) => (
                  <ReleasedCard key={show.key} item={show.first} episodes={show.episodes} />
                ))}
              </div>
            </section>
          ))}
        </div>
      )}
    </div>
  )
}
