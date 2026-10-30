import { useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import type { ScheduleItem } from '../lib/api'
import { airTime, cx } from '../lib/format'
import { useNow, useSchedule } from '../lib/queries'
import { releaseState, TONE_TEXT } from '../lib/aired'
import { Rail, RailFailed, RailItem, RailSkeleton } from './Rail'
import { Badge } from './ui'

const TITLE = 'Airing today'
const TO = '/schedule?view=coming'

export function AiringToday({ folded, onUnfold }: { folded: boolean; onUnfold: () => void }) {
  const schedule = useSchedule(7)
  const now = useNow() / 1000
  const today = schedule.data?.days.find((d) => d.today)
  const items = today?.items ?? []
  const next = items.find((item) => item.airingAt > now)
  const nextCard = useRef<HTMLDivElement>(null)
  const placed = useRef<string | null>(null)

  useEffect(() => {
    const el = nextCard.current
    const track = el?.closest<HTMLElement>('[data-rail-track]')
    if (!el || !track || !today || placed.current === today.date) return
    placed.current = today.date
    const left = track.scrollLeft + el.getBoundingClientRect().left - track.getBoundingClientRect().left - 4
    track.scrollTo({ left, behavior: 'instant' })
  }, [today, next])

  if (folded) return <Rail title={TITLE} to={TO} folded onUnfold={onUnfold}>{null}</Rail>
  if (schedule.isPending) return <RailSkeleton title={TITLE} to={TO} size="still" />
  if (schedule.isError && !schedule.data) return <RailFailed title={TITLE} to={TO} retry={() => void schedule.refetch()} />

  return (
    <Rail title={TITLE} to={TO} meta={items.length > 0 ? `${items.length} ${items.length === 1 ? 'episode' : 'episodes'}` : undefined}>
      {items.length === 0 ? (
        <p className="py-2 text-sm text-base-400">Nothing airs today. The full week is in Schedule.</p>
      ) : (
        items.map((item) => (
          <RailItem key={`${item.animeId}-${item.episode}`} size="still">
            <div ref={item === next ? nextCard : undefined}>
              <AiringCard item={item} next={item === next} aired={item.airingAt <= now} now={now} />
            </div>
          </RailItem>
        ))
      )}
    </Rail>
  )
}

function AiringCard({ item, next, aired, now }: { item: ScheduleItem; next: boolean; aired: boolean; now: number }) {
  const art = item.show?.banner ?? item.coverLarge ?? item.cover
  const state = releaseState(item, now)
  return (
    <Link to={`/anime/${item.animeId}`} className="press group/airing block rounded-card focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-white">
      <div className="relative aspect-video overflow-hidden rounded-card bg-base-850">
        {art && (
          <img
            src={art}
            alt=""
            loading="lazy"
            className={cx(
              'size-full object-cover transition-transform duration-300 group-hover/airing:scale-105',
              aired && 'brightness-75',
            )}
          />
        )}
      </div>
      <p className="mt-2 line-clamp-2 text-sm leading-snug font-semibold text-base-100 transition-colors group-hover/airing:text-white">
        {item.title}
      </p>
      <p className="mt-0.5 flex items-center gap-1.5 text-[13px] whitespace-nowrap text-base-400 tabular-nums">
        <span className={cx('min-w-0 truncate', aired && 'text-base-500')}>
          {airTime(item.airingAt)} · Ep {item.episode}
        </span>
        {next && <Badge tone="sky">Next</Badge>}
      </p>
      {state && <p className={cx('text-[13px]', TONE_TEXT[state.tone])}>{state.word}</p>}
    </Link>
  )
}
