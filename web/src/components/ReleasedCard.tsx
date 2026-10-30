import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, type ScheduleItem } from '../lib/api'
import { cx, posterSet, whenLabel } from '../lib/format'
import { minutesLeft, releaseState, TONE_TEXT } from '../lib/aired'
import { refreshListState, useNow, useUpNextEdit } from '../lib/queries'
import { toast } from '../lib/toast'
import { useInUpNext } from '../lib/upnext'
import { toCard } from '../lib/cards'
import { HoverInfo } from './HoverInfo'
import { MoreMenu, type MoreItem } from './MoreMenu'

function span(numbers: number[]) {
  const lo = numbers[0]
  const hi = numbers[numbers.length - 1]
  return numbers.length === 2 ? `Ep ${lo} and ${hi}` : `Ep ${lo} to ${hi}`
}

export function ReleasedCard({ item, episodes }: { item: ScheduleItem; episodes?: ScheduleItem[] }) {
  const all = episodes && episodes.length > 0 ? [...episodes].sort((a, b) => a.episode - b.episode) : [item]
  const newest = all[all.length - 1]
  const play = all.find((it) => !it.watched) ?? item
  const now = useNow() / 1000
  const watch = `/watch/${play.animeId}/${play.episode}`
  const left = minutesLeft(play)
  const state = releaseState(all.every((it) => it.watched) ? newest : play, now)
  const several = all.length > 1
  const line = several
    ? `${span(all.map((it) => it.episode))} · ${all.length} new`
    : left
      ? `Ep ${play.episode} · ${left} min left`
      : `Ep ${play.episode} · ${whenLabel(play.airingAt, now * 1000)}`
  const badge = several ? `${all.length} new` : play.behind > 0 && !play.watched ? `${play.behind} behind` : null
  const done = state?.tone === 'done'

  const qc = useQueryClient()
  const queued = useInUpNext(play.animeId)
  const inUpNext = all.filter((it) => queued.has(it.episode))
  const unwatched = all.filter((it) => !it.watched)
  const { add, remove } = useUpNextEdit()
  const mark = useMutation({
    mutationFn: (v: { episode: number; watched: boolean }) =>
      api.post('/api/watched', { animeId: play.animeId, episode: v.episode, watched: v.watched }),
    onSettled: () => refreshListState(qc),
    onError: (e) => toast(`Couldn't change ${play.title}: ${e.message}`, 'error'),
  })

  const markWatched = () =>
    mark.mutate(
      { episode: newest.episode, watched: true },
      {
        onSuccess: () =>
          toast(`Marked ${several ? `up to episode ${newest.episode}` : `episode ${newest.episode}`} of ${play.title} watched`, 'info'),
      },
    )

  const items: MoreItem[] = [
    { label: `Play episode ${play.episode}`, to: watch },
    inUpNext.length > 0
      ? {
          label: 'Remove from Up next',
          run: () => {
            for (const it of inUpNext) remove.mutate({ animeId: it.animeId, episode: it.episode })
          },
        }
      : {
          label: 'Add to Up next',
          note: unwatched.length > 1 ? span(unwatched.map((it) => it.episode)) : undefined,
          run: () => {
            for (const it of unwatched.length > 0 ? unwatched : [play]) add.mutate({ animeId: it.animeId, episode: it.episode })
          },
        },
    { label: 'Open show', to: `/anime/${play.animeId}` },
    done
      ? {
          label: 'Mark not watched',
          note: `Episode ${newest.episode}`,
          run: () => mark.mutate({ episode: newest.episode, watched: false }),
          disabled: mark.isPending,
        }
      : {
          label: 'Mark watched',
          note: several ? `Up to episode ${newest.episode}` : undefined,
          run: markWatched,
          disabled: mark.isPending,
        },
  ]

  return (
    <div className="group/released relative">
      <HoverInfo
        anime={{
          ...(play.show ? toCard(play.show) : {}),
          status: play.show?.status ?? null,
          listStatus: play.show?.listStatus ?? null,
          id: play.animeId,
          title: play.title,
          romaji: play.romaji,
          english: play.english,
          format: play.format,
          color: play.colour,
          cover: play.cover,
          progress: play.progress,
          play: { to: watch, label: `Play ep ${play.episode}` },
        }}
      >
        <Link
          to={watch}
          className="block rounded-card focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent-400"
        >
          <div className="relative aspect-video overflow-hidden rounded-card bg-base-850 shadow-card ring-1 ring-white/[0.06] transition-[transform,box-shadow] duration-300 ease-out-quint group-hover/released:-translate-y-0.5 group-hover/released:shadow-lift">
            {play.cover && (
              <img
                src={play.cover}
                srcSet={posterSet(play.cover, play.coverLarge)}
                alt=""
                loading="lazy"
                className={cx(
                  'size-full object-cover object-center transition-transform duration-300 group-hover/released:scale-105',
                  done && 'opacity-60',
                )}
              />
            )}
            {badge && (
              <span className="absolute top-1.5 right-1.5 rounded-md bg-base-950/90 px-1.5 py-0.5 text-xs leading-tight font-semibold whitespace-nowrap text-base-100">
                {badge}
              </span>
            )}
            {left && play.duration && (
              <span className="absolute inset-x-0 bottom-0 h-[3px] bg-base-950/70">
                <span
                  className="block h-full bg-accent-500"
                  style={{ width: `${Math.min(100, ((play.position ?? 0) / play.duration) * 100)}%` }}
                />
              </span>
            )}
          </div>
          <p className="mt-2 line-clamp-2 text-[15px] leading-5 font-semibold text-base-100 transition-colors group-hover/released:text-white">
            {play.title}
          </p>
          <div className="flex min-h-11 flex-col justify-center pr-11 text-[13px] leading-[18px] tabular-nums">
            <span className="text-base-400">{line}</span>
            {state && (
              <span className={TONE_TEXT[state.tone]}>
                {state.word}
                {state.checked && <span className="text-base-500"> · {state.checked}</span>}
              </span>
            )}
          </div>
        </Link>
      </HoverInfo>
      <div className="absolute right-0 bottom-0">
        <MoreMenu
          label={`More for ${play.title}`}
          title={play.title}
          detail={line}
          items={items}
          className="size-11 rounded-full"
        />
      </div>
    </div>
  )
}
