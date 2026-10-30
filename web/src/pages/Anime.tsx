import { Fragment, useEffect, useRef, useState, type ButtonHTMLAttributes, type ReactNode, type Ref } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  api,
  LIST_STATUSES,
  statusLabel,
  type Bookmark,
  type DiscoverItem,
  type Episode,
  type FollowState,
  type ListStatus,
} from '../lib/api'
import { bytes, clockTime, cx, relativeTime, youtubeID } from '../lib/format'
import { ControlLabel } from '../lib/ui'
import { cannotSave, saveEpisodes } from '../lib/offline'
import {
  refreshDownloads,
  useAccess,
  useAlertsOff,
  useAnimeExtra,
  useBetterCopies,
  useDownloadQueue,
  useDownloads,
  useDriveRoom,
  useEpisodes,
  useFranchise,
  useNow,
  usePrefs,
  useRecommendations,
  useRemoveStatus,
  useSetNotify,
  useSetPref,
  useSetStatus,
  type RelatedEntry,
  type Season,
} from '../lib/queries'
import { CharacterRail } from '../components/CharacterRail'
import { ShowCredits } from '../components/ShowCredits'
import { EpisodeList } from '../components/EpisodeList'
import { isUnaired } from '../lib/episodes'
import { PlayIcon, PosterCard } from '../components/PosterCard'
import { toCard } from '../lib/cards'
import { Rail, RailItem } from '../components/Rail'
import { PopSheet } from '../components/PopSheet'
import { SeasonSwitch } from '../components/SeasonSwitch'
import { Sheet } from '../components/Sheet'
import { WaitSelect } from '../components/WaitSelect'
import { BetterCopyOffer } from '../components/BetterCopy'
import { TrailerOverlay } from '../components/TrailerOverlay'
import {
  Badge,
  Button,
  ConfirmInline,
  ErrorState,
  inputClass,
  LinkButton,
  SaveHint,
  Skeleton,
  Switch,
  TextInput,
} from '../components/ui'
import { useDocumentTitle, useMediaQuery } from '../lib/hooks'

interface AnimeDetail {
  id: number
  source: 'anilist' | 'myanimelist' | 'corpus'
  title: string
  partial?: boolean
  listStatus?: string
  progress?: number
  episodeCount?: number
  repeat?: number
  score?: number
  bookmark?: Bookmark
  resume?: { episode: number; position: number; duration: number }
  sync?: { anilist: boolean; mal: boolean }
  delay?: { episode: number; wasAt: number; nowAt: number | null }
  notify?: FollowState
  malId?: number
  anime: Media
}

interface Media {
  trailer?: { id?: string; site?: string } | null
  title?: { romaji?: string; english?: string; native?: string }
  description?: string
  coverImage?: { extraLarge?: string; color?: string }
  bannerImage?: string
  episodes?: number
  format?: string
  status?: string
  genres?: string[]
  averageScore?: number
  season?: string
  seasonYear?: number
  studios?: { nodes?: { id: number; name: string }[] } | string[]
  source?: string
  duration?: number
  startDate?: FuzzyDate
  endDate?: FuzzyDate
  nextAiringEpisode?: { episode: number; airingAt: number } | null
  romaji?: string
  cover?: string
  Description?: string
}

interface FuzzyDate {
  year?: number | null
  month?: number | null
  day?: number | null
}

type Lead =
  | { kind: 'play'; label: string; done?: number }
  | { kind: 'rewatch' }
  | { kind: 'wait'; label: string }

const DAY = 86_400_000

function fuzzy(d?: FuzzyDate): string | null {
  if (!d?.year) return null
  if (!d.month) return String(d.year)
  return new Date(d.year, d.month - 1, d.day ?? 1).toLocaleDateString(undefined, {
    year: 'numeric',
    month: 'short',
    ...(d.day ? { day: 'numeric' } : {}),
  })
}

const STATUS: Record<string, { label: string; tone: string }> = {
  RELEASING: { label: 'Airing', tone: 'text-sky-300' },
  FINISHED: { label: 'Finished', tone: 'text-base-300' },
  NOT_YET_RELEASED: { label: 'Upcoming', tone: 'text-sky-300' },
  HIATUS: { label: 'On hiatus', tone: 'text-amber-300' },
  CANCELLED: { label: 'Cancelled', tone: 'text-base-400' },
}

const FORMATS: Record<string, string> = {
  TV: 'TV',
  TV_SHORT: 'TV short',
  MOVIE: 'Movie',
  SPECIAL: 'Special',
  OVA: 'OVA',
  ONA: 'ONA',
  MUSIC: 'Music video',
}

const isFilm = (media: Media) => media.format?.toUpperCase() === 'MOVIE'

function seasonName(media: Media): string | null {
  if (media.season && media.seasonYear) {
    return `${media.season.charAt(0)}${media.season.slice(1).toLowerCase()} ${media.seasonYear}`
  }
  return media.seasonYear ? String(media.seasonYear) : null
}

function airLabel(unix: number, now: number): string {
  if (unix * 1000 - now < DAY) return relativeTime(unix)
  return new Date(unix * 1000).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })
}

function outLabel(unix: number, now: number): string {
  const ms = unix * 1000 - now
  if (ms < DAY) return relativeTime(unix)
  return new Date(unix * 1000).toLocaleDateString(undefined, ms < 6 * DAY ? { weekday: 'short' } : { day: 'numeric', month: 'short' })
}

function resumeLabel(episode: number, resume: { position: number; duration: number }): string {
  const left = resume.duration - resume.position
  return resume.duration > 0 && left > 0
    ? `Resume Ep ${episode} · ${Math.max(1, Math.round(left / 60))} min left`
    : `Resume Ep ${episode} · ${clockTime(resume.position)}`
}

function otherTitles(media: Media, title?: string): string[] {
  const names = [media.title?.native, media.title?.romaji ?? media.romaji, media.title?.english]
  return [...new Set(names.filter((n): n is string => !!n && n !== title))]
}

function airedWords(media: Media): string | null {
  const start = fuzzy(media.startDate)
  const end = fuzzy(media.endDate)
  if (!start) return null
  if (media.status === 'RELEASING') return `${start} to now`
  if (media.status === 'NOT_YET_RELEASED') return `Starts ${start}`
  return end && end !== start ? `${start} to ${end}` : start
}

function sourceName(source?: string): string | null {
  if (!source) return null
  const said = source.replace(/_/g, ' ').toLowerCase()
  if (said === 'other' || said === 'unknown') return null
  return said.charAt(0).toUpperCase() + said.slice(1)
}

function sourceWords(source?: string): string | null {
  const name = sourceName(source)
  if (!name) return null
  if (name === 'Original') return 'Original story'
  if (name === 'Anime') return 'From another anime'
  return `From the ${name.toLowerCase()}`
}

function studioList(studios: Media['studios']) {
  return Array.isArray(studios) ? studios.map((name) => ({ id: 0, name })) : (studios?.nodes ?? [])
}

function Studios({ studios }: { studios: Media['studios'] }) {
  return (
    <>
      {studioList(studios).map((s, i) => (
        <Fragment key={`${s.id}-${s.name}`}>
          {i > 0 && ', '}
          {s.id ? (
            <Link
              to={`/browse?studio=${s.id}&studioName=${encodeURIComponent(s.name)}`}
              className="tap relative text-base-200 underline decoration-base-600 underline-offset-2 transition-colors hover:text-white hover:decoration-accent-300"
              title={`More from ${s.name}`}
            >
              {s.name}
            </Link>
          ) : (
            s.name
          )}
        </Fragment>
      ))}
    </>
  )
}

function MetaLine({ media, delay, repeat }: { media: Media; delay?: AnimeDetail['delay']; repeat?: number }) {
  const now = useNow()
  const status = media.status ? STATUS[media.status] : undefined
  const aired = media.nextAiringEpisode ? media.nextAiringEpisode.episode - 1 : null
  const film = isFilm(media)
  const episodes = film
    ? null
    : media.status === 'RELEASING' && aired && media.episodes && aired < media.episodes
      ? `${aired} of ${media.episodes} out`
      : media.episodes
        ? `${media.episodes} ${media.episodes === 1 ? 'ep' : 'eps'}`
        : aired
          ? `${aired} out`
          : null
  const next = media.nextAiringEpisode && media.nextAiringEpisode.airingAt * 1000 > now ? media.nextAiringEpisode : null
  const season = seasonName(media)

  const facts: Array<[string, ReactNode]> = []
  if (media.averageScore) facts.push(['score', <span key="score" className="text-amber-300">★ {(media.averageScore / 10).toFixed(1)}</span>])
  if (media.format) facts.push(['format', FORMATS[media.format] ?? media.format.replace(/_/g, ' ')])
  if (season) facts.push(['season', season])
  if (episodes) facts.push(['episodes', episodes])
  if (film && media.duration) facts.push(['duration', `${media.duration} min`])
  if (repeat) facts.push(['repeat', <span key="repeat" title="Completed rewatches, as counted on your list">Rewatched ×{repeat}</span>])
  if (status) {
    facts.push([
      'status',
      <span key="status" className={cx('inline-flex items-center gap-1.5', status.tone)}>
        {media.status === 'RELEASING' && <span aria-hidden className="size-1.5 rounded-full bg-sky-300" />}
        {status.label}
        {next ? ` · Ep ${next.episode} ${airLabel(next.airingAt, now)}` : ''}
      </span>,
    ])
  }
  if (delay) {
    facts.push([
      'delay',
      <span key="delay" className="text-amber-300" title={`Was due ${new Date(delay.wasAt * 1000).toLocaleString()}`}>
        Ep {delay.episode} delayed{delay.nowAt ? '' : ' · no new date yet'}
      </span>,
    ])
  }
  if (facts.length === 0) return null

  return (
    <p className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-[15px] text-base-300 tabular-nums">
      {facts.map(([key, fact], i) => (
        <Fragment key={key}>
          {key === 'status' && i > 0 && <span aria-hidden className="-my-0.5 basis-full sm:hidden" />}
          {i > 0 && <span aria-hidden className={cx('text-base-600', key === 'status' && 'max-sm:hidden')}>·</span>}
          {fact}
        </Fragment>
      ))}
    </p>
  )
}

function FactsLine({ media }: { media: Media }) {
  const parts: Array<[string, ReactNode]> = []
  const source = sourceWords(media.source)
  if (source) parts.push(['source', source])
  if (studioList(media.studios).length > 0) parts.push(['studio', <Studios key="studio" studios={media.studios} />])
  const aired = airedWords(media)
  if (aired) parts.push(['aired', aired])
  if (media.duration && !isFilm(media)) parts.push(['length', `${media.duration} min episodes`])
  if (parts.length === 0) return null
  return (
    <p className="mt-3 max-w-[45rem] text-sm text-base-400">
      {parts.map(([key, part], i) => (
        <Fragment key={key}>
          {i > 0 && ' · '}
          {part}
        </Fragment>
      ))}
    </p>
  )
}

function FactList({ media }: { media: Media }) {
  const rows: Array<[string, ReactNode]> = []
  const aired = airedWords(media)
  if (aired) rows.push(['Aired', aired])
  const studios = studioList(media.studios)
  if (studios.length > 0) rows.push([studios.length > 1 ? 'Studios' : 'Studio', <Studios key="studio" studios={media.studios} />])
  const source = sourceName(media.source)
  if (source) rows.push(['Source', source])
  const season = seasonName(media)
  if (season) rows.push(['Season', season])
  if (media.duration) rows.push(['Length', isFilm(media) ? `${media.duration} min` : `${media.duration} min per episode`])
  if (rows.length === 0) return null
  return (
    <section>
      <h3 className="mb-2 text-sm font-semibold text-base-100">About</h3>
      <dl className="grid grid-cols-[5rem_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm">
        {rows.map(([label, value]) => (
          <Fragment key={label}>
            <dt className="text-[13px] font-semibold text-base-400">{label}</dt>
            <dd className="min-w-0 text-base-100">{value}</dd>
          </Fragment>
        ))}
      </dl>
    </section>
  )
}

function Genres({ genres }: { genres: string[] }) {
  return (
    <div className="no-scrollbar -mx-4 mt-3 flex gap-2 overflow-x-auto px-4 py-1.5 sm:mx-0 sm:max-w-[45rem] sm:flex-wrap sm:overflow-visible sm:px-0">
      {genres.map((genre) => (
        <Link
          key={genre}
          to={`/browse?genres=${encodeURIComponent(genre)}`}
          className="tap relative flex h-8 shrink-0 items-center rounded-poster bg-base-800 px-3 text-[13px] font-medium text-base-200 transition-colors hover:bg-base-750 hover:text-white"
        >
          {genre}
        </Link>
      ))}
    </div>
  )
}

function Synopsis({ text }: { text: string }) {
  const [open, setOpen] = useState(false)
  const [clamped, setClamped] = useState(false)
  const body = useRef<HTMLParagraphElement>(null)

  useEffect(() => {
    const el = body.current
    if (!el) return
    const check = () => setClamped(el.scrollHeight > el.clientHeight + 4)
    check()
    window.addEventListener('resize', check)
    return () => window.removeEventListener('resize', check)
  }, [text])

  return (
    <div className="mt-4 max-w-[45rem]">
      <p
        ref={body}
        onClick={() => clamped && setOpen(true)}
        className={cx(
          'text-[15px] leading-relaxed text-base-300 sm:text-sm',
          !open && 'line-clamp-3',
          clamped && !open && 'cursor-pointer',
        )}
      >
        {text}
      </p>
      {(clamped || open) && (
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="tap relative mt-1 text-sm font-medium text-accent-300 hover:underline"
        >
          {open ? 'Show less' : 'Show more'}
        </button>
      )}
    </div>
  )
}

function TrailerButton({ videoId, className }: { videoId: string; className?: string }) {
  const [showing, setShowing] = useState(false)
  return (
    <>
      <button
        type="button"
        onClick={() => setShowing(true)}
        className={cx(
          'press flex h-11 items-center gap-1.5 rounded-card bg-base-950/70 px-4 text-sm font-medium text-white ring-1 ring-white/[0.06] backdrop-blur-sm transition-[background-color,transform] hover:bg-base-950/90',
          className,
        )}
      >
        <PlayIcon className="size-4" />
        Trailer
      </button>
      {showing && <TrailerOverlay videoId={videoId} onClose={() => setShowing(false)} />}
    </>
  )
}

const EPISODE_BYTES_GUESS = 1.5 * 1024 ** 3

export function Anime() {
  const { animeId } = useParams()
  const id = Number(animeId)

  const detail = useQuery({
    enabled: Number.isFinite(id) && id !== 0,
    queryKey: ['anime', id],
    queryFn: () => api.get<AnimeDetail>(`/api/anime/${id}`),
  })

  const episodes = useEpisodes(id)
  const extra = useAnimeExtra(id)
  const franchise = useFranchise(id !== 0 ? id : undefined)
  const recommended = useRecommendations(id > 0 ? id : undefined)
  const wide = useMediaQuery('(min-width: 80rem)')

  const clock = useNow()
  const list = episodes.data?.items ?? []
  const progress = detail.data?.progress ?? 0
  const resume = detail.data?.resume
  const now = clock / 1000
  const next = list.find((e) => e.number > progress)
  const total = detail.data?.episodeCount || detail.data?.anime.episodes || list.length
  const first = resume?.episode ?? next?.number ?? (Math.min(progress + 1, list.length || progress + 1) || 1)
  const firstLabel = list.find((e) => e.number === first)?.display ?? first
  const finished =
    !resume &&
    (detail.data?.listStatus === 'COMPLETED' || (progress > 0 && total > 0 && progress >= total && !next))
  const unaired = !resume && !finished && progress > 0 && !!next?.airDate && next.airDate > now
  const lead: Lead = resume
    ? {
        kind: 'play',
        label: resumeLabel(firstLabel, resume),
        done: resume.duration > 0 ? Math.min(1, resume.position / resume.duration) : undefined,
      }
    : finished
      ? { kind: 'rewatch' }
      : unaired
        ? { kind: 'wait', label: `Ep ${next!.display} out ${outLabel(next!.airDate!, clock)}` }
        : { kind: 'play', label: `Play Ep ${firstLabel}` }
  const ready = !!detail.data && !episodes.isPending

  const navigate = useNavigate()
  useDocumentTitle(detail.data?.title)
  const rewatch = useSetStatus()
  const [confirmRewatch, setConfirmRewatch] = useState(false)
  const startRewatch = () => {
    if (!confirmRewatch) return setConfirmRewatch(true)
    rewatch.mutate(
      { animeId: id, status: 'REPEATING' },
      { onSuccess: () => navigate(`/watch/${id}/1`) },
    )
  }

  useEffect(() => {
    if (!ready || !Number.isFinite(id) || !id || !first || lead.kind !== 'play') return
    void api.post('/api/prepare', { animeId: id, episode: first }).catch(() => {})
  }, [ready, id, first, lead.kind])

  if (!Number.isFinite(id) || id === 0) {
    return <ErrorState error={new Error('Unknown anime')} />
  }
  if (detail.isError && !detail.data) {
    return <ErrorState error={detail.error} retry={() => detail.refetch()} />
  }

  const media = detail.data?.anime
  const cover = media?.coverImage?.extraLarge ?? media?.cover
  const banner = media?.bannerImage
  const art = banner ?? cover
  const description = (media?.description ?? media?.Description ?? '').replace(/<[^>]+>/g, '')
  const trailer =
    (media?.trailer?.site?.toLowerCase() === 'youtube' ? media.trailer.id : undefined) ??
    youtubeID(extra.data?.trailer) ??
    undefined
  const anilist = detail.data?.source === 'anilist' ? id : undefined
  const mal = detail.data?.malId
  const others = media ? otherTitles(media, detail.data?.title) : []

  return (
    <div className="space-y-8">
      <section className={cx('relative -mx-4 -mt-4 sm:-mx-6', art && 'sm:min-h-[26rem]')}>
        {art && (
          <div className="relative aspect-video overflow-hidden sm:absolute sm:top-0 sm:right-[calc(50%_-_min(50vw,1425px))] sm:left-[calc(50%_-_min(50vw,1425px))] sm:aspect-auto sm:h-[26rem] min-[1700px]:[mask-image:linear-gradient(to_right,transparent,black_6rem,black_calc(100%-6rem),transparent)]">
            <img
              src={art}
              alt=""
              className={cx(
                'size-full object-cover max-sm:[view-transition-name:show-art]',
                banner ? 'object-center' : 'object-[center_20%] sm:scale-125 sm:opacity-50 sm:blur-2xl',
              )}
            />
            <div className="absolute inset-0 bg-gradient-to-t from-base-950 to-transparent to-40% sm:hidden" />
            <div className="absolute inset-0 hidden bg-gradient-to-t from-base-950 via-base-950/80 to-base-950/30 sm:block" />
            <div className="absolute inset-0 hidden bg-gradient-to-r from-base-950/70 to-transparent sm:block" />
            {trailer && <TrailerButton videoId={trailer} className="absolute right-4 bottom-3 z-10 sm:hidden" />}
          </div>
        )}

        <div
          className={cx(
            'relative mx-auto flex max-w-[1600px] flex-col gap-6 px-4 sm:flex-row sm:p-6',
            art ? 'pt-1 sm:pt-28' : 'pt-4 sm:pt-8',
          )}
        >
          {art && trailer && <TrailerButton videoId={trailer} className="absolute top-4 right-6 z-10 max-sm:hidden" />}
          {detail.isPending ? (
            <Skeleton className="hidden h-72 w-48 shrink-0 rounded-card sm:block" />
          ) : cover ? (
            <img
              src={cover}
              alt=""
              loading="lazy"
              className="hidden h-72 w-48 shrink-0 rounded-card object-cover shadow-lift ring-1 ring-white/[0.06] sm:block sm:[view-transition-name:show-art]"
            />
          ) : null}

          <div className="min-w-0 flex-1 sm:pt-4">
            {detail.isPending ? (
              <Skeleton className="h-9 w-2/3" />
            ) : (
              <h1 className="max-w-4xl font-display text-[26px] leading-[30px] font-bold tracking-tight text-balance text-white sm:text-4xl sm:leading-[1.08] lg:text-5xl lg:leading-[1.05]">
                {detail.data?.title}
              </h1>
            )}
            {others.length > 0 && <p className="mt-1.5 max-w-4xl text-sm text-base-400">{others.join(' · ')}</p>}
            {media && <MetaLine media={media} delay={detail.data?.delay} repeat={detail.data?.repeat} />}
            {detail.data?.source === 'corpus' && (
              <p className="mt-2">
                <Badge tone="warning">Limited info: MyAnimeList unreachable</Badge>
              </p>
            )}

            <div className="mt-4 flex flex-col gap-3 sm:mt-5 sm:flex-row sm:flex-wrap sm:items-center sm:gap-2">
              {!ready ? (
                <Skeleton className="h-12 w-full rounded-card sm:h-11 sm:w-56" />
              ) : lead.kind === 'play' ? (
                <LinkButton
                  to={`/watch/${id}/${first}`}
                  variant="primary"
                  size="lg"
                  className="relative overflow-hidden font-semibold max-sm:w-full"
                >
                  <PlayIcon />
                  {lead.label}
                  {lead.done !== undefined && (
                    <span aria-hidden className="absolute inset-x-0 bottom-0 h-[3px] bg-black/25">
                      <span className="block h-full bg-white" style={{ width: `${lead.done * 100}%` }} />
                    </span>
                  )}
                </LinkButton>
              ) : lead.kind === 'rewatch' && confirmRewatch ? (
                <div className="flex flex-wrap items-center gap-2">
                  <ConfirmInline
                    label={rewatch.isPending ? 'Starting…' : 'Restart from Ep 1'}
                    note="Progress and ticks reset"
                    tone="accent"
                    size="lg"
                    pending={rewatch.isPending}
                    onConfirm={startRewatch}
                    onCancel={() => setConfirmRewatch(false)}
                  />
                </div>
              ) : lead.kind === 'rewatch' ? (
                <Button
                  onClick={startRewatch}
                  title="Marks the show as rewatching, which counts progress from the start again"
                  variant="primary"
                  size="lg"
                  className="font-semibold max-sm:w-full"
                >
                  <RewatchIcon />
                  Rewatch from Ep 1
                </Button>
              ) : (
                <p
                  title="You are caught up. The next episode has not aired yet."
                  className="flex min-h-11 items-center gap-2 text-[15px] font-medium text-sky-300 max-sm:justify-center"
                >
                  <ClockIcon />
                  {lead.label}
                </p>
              )}

              {ready && detail.data ? (
                <div className="grid grid-cols-4 gap-2 sm:flex sm:flex-wrap">
                  <ListAction animeId={id} current={detail.data.listStatus} />
                  <RateAction animeId={id} score={detail.data.score ?? 0} />
                  <FavouriteAction animeId={id} bookmark={detail.data.bookmark} />
                  <DownloadAction animeId={id} episodes={list} />
                </div>
              ) : (
                <div className="grid grid-cols-4 gap-2 sm:flex">
                  {Array.from({ length: 4 }, (_, i) => (
                    <Skeleton key={i} className="h-16 rounded-card sm:h-10 sm:w-28" />
                  ))}
                </div>
              )}
            </div>

            <ShowBetterCopy animeId={id} />
            {description && <Synopsis text={description} />}
            {media && !wide && <FactsLine media={media} />}
            {media?.genres && media.genres.length > 0 && <Genres genres={media.genres} />}
            {trailer && !art && <TrailerButton videoId={trailer} className="mt-3 bg-base-800 ring-0 hover:bg-base-750" />}
            {detail.data && !wide && (
              <ThisShowRow
                animeId={id}
                notify={detail.data.notify}
                bookmark={detail.data.bookmark}
                sync={detail.data.sync}
              />
            )}
          </div>
        </div>
      </section>

      <div className="xl:grid xl:grid-cols-[minmax(0,1fr)_21.25rem] xl:items-start xl:gap-10">
        <div className="min-w-0 space-y-8">
          {(franchise.data?.seasons?.length ?? 0) > 1 && (
            <Rail title="Seasons">
              {franchise.data!.seasons.map((season) => (
                <SeasonChip key={season.id} season={season} current={season.id === id} />
              ))}
            </Rail>
          )}

          <section>
            <div className="mb-3 flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
              <h2 className="section-title">Episodes</h2>
              <SeasonSwitch seasons={franchise.data?.seasons ?? []} current={id} />
            </div>

            {episodes.isPending ? (
              <Skeleton className="h-80 w-full" />
            ) : episodes.isError && !episodes.data ? (
              <ErrorState error={episodes.error} retry={() => void episodes.refetch()} />
            ) : list.length === 0 ? (
              <div className="surface p-6 text-center">
                <p className="text-sm text-base-400">No episode list for this show yet.</p>
                <LinkButton to={`/watch/${id}/1`} className="mt-3">
                  Try episode 1 anyway
                </LinkButton>
              </div>
            ) : (
              <EpisodeList
                animeId={id}
                episodes={list}
                upNext={lead.kind === 'play' ? first : undefined}
                selectable
              />
            )}
          </section>

          <CharacterRail animeId={id} />
          <RelatedRails entries={franchise.data?.related ?? []} />
          <ShowCredits animeId={id} />

          {(recommended.data?.items?.length ?? 0) > 0 && (
            <Rail title={recommended.data?.source === 'genre' ? 'Similar genres' : 'More like this'}>
              {recommended.data!.items.map((anime: DiscoverItem) => (
                <RailItem key={anime.id}>
                  <PosterCard anime={toCard(anime)} />
                </RailItem>
              ))}
            </Rail>
          )}

          {!wide && (anilist || mal) && (
            <section className="space-y-3">
              <h2 className="section-title">Links</h2>
              <ShowLinks anilist={anilist} mal={mal} className="sm:max-w-xl" />
            </section>
          )}
        </div>

        {wide && detail.data && (
          <aside
            aria-label="This show"
            className="sticky top-20 max-h-[calc(100dvh-6rem)] space-y-6 overflow-y-auto rounded-card bg-base-900 p-4 scrollbar-thin"
          >
            <section>
              <h2 className="text-base font-semibold text-base-100">This show</h2>
              <ShowSettings
                animeId={id}
                notify={detail.data.notify}
                bookmark={detail.data.bookmark}
                sync={detail.data.sync}
              />
            </section>
            <FactList media={detail.data.anime} />
            {(anilist || mal) && (
              <section>
                <h3 className="mb-2 text-sm font-semibold text-base-100">Links</h3>
                <ShowLinks anilist={anilist} mal={mal} tone="bg-base-800 hover:bg-base-750" />
              </section>
            )}
          </aside>
        )}
      </div>
    </div>
  )
}

const TILE =
  'press flex min-h-16 min-w-0 flex-col items-center justify-center gap-1 rounded-card bg-base-800 px-1 py-2 text-center text-[13px] leading-tight font-medium text-base-100 transition-[background-color,color,transform] hover:bg-base-750 focus:outline-hidden focus-visible:ring-2 focus-visible:ring-white focus-visible:ring-offset-2 focus-visible:ring-offset-base-950 disabled:opacity-50 sm:h-10 sm:min-h-0 sm:flex-row sm:gap-1.5 sm:px-4 sm:py-0 sm:text-sm sm:whitespace-nowrap sm:pointer-coarse:min-h-11'

function Tile({
  ref,
  icon,
  label,
  caption,
  wide,
  chevron,
  className,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  ref?: Ref<HTMLButtonElement>
  icon: ReactNode
  label: ReactNode
  caption?: ReactNode
  wide?: ReactNode
  chevron?: boolean
}) {
  return (
    <button ref={ref} type="button" className={cx(TILE, className)} {...props}>
      {icon}
      <span className="flex items-center gap-0.5 sm:hidden">
        {label}
        {chevron && <ChevronDown />}
      </span>
      {caption && <span className="text-xs font-normal text-base-400 sm:hidden">{caption}</span>}
      <span className="hidden items-center gap-1.5 sm:flex">
        {wide ?? label}
        {chevron && <ChevronDown />}
      </span>
    </button>
  )
}

const CHOICE =
  'flex min-h-11 w-full items-center justify-between gap-3 rounded-lg px-3 text-left text-[15px] transition-colors hover:bg-base-700 disabled:opacity-50 sm:min-h-10 sm:text-sm'

function Confirming({ note, children }: { note: string; children: ReactNode }) {
  return (
    <div className="mt-1 flex flex-wrap items-center gap-2 rounded-lg bg-base-800 px-3 py-2.5">
      <span className="basis-full text-[13px] text-base-300">{note}</span>
      {children}
    </div>
  )
}

function ListAction({ animeId, current }: { animeId: number; current?: string }) {
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const setStatus = useSetStatus({ inline: true })
  const remove = useRemoveStatus()
  const [confirm, setConfirm] = useState<'rewatch' | 'remove' | null>(null)
  const active = LIST_STATUSES.find((s) => s.value === current)
  const close = () => {
    setOpen(false)
    setConfirm(null)
  }
  const choose = (status: ListStatus) => {
    if (status === 'REPEATING' && current !== 'REPEATING') return setConfirm('rewatch')
    setConfirm(null)
    setStatus.mutate({ animeId, status }, { onSuccess: close })
  }
  const error = (setStatus.error ?? remove.error) as Error | null

  return (
    <>
      <Tile
        ref={trigger}
        icon={<BookmarkIcon filled={!!active} />}
        label={active?.label ?? 'Add to list'}
        chevron
        aria-label={active ? `Listed as ${active.label}` : 'Add to list'}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => {
          setConfirm(null)
          setStatus.reset()
          remove.reset()
          setOpen(true)
        }}
      />
      <PopSheet open={open} onClose={close} title="Your list" anchor={trigger} width={256}>
        <div role="menu" aria-label="Your list">
          {LIST_STATUSES.map((status) => (
            <button
              key={status.value}
              type="button"
              role="menuitemradio"
              aria-checked={status.value === current}
              disabled={setStatus.isPending}
              onClick={() => choose(status.value)}
              className={cx(CHOICE, status.value === current ? 'text-accent-300' : 'text-base-100')}
            >
              {status.label}
              {status.value === current && <CheckIcon />}
            </button>
          ))}
        </div>
        {confirm === 'rewatch' && (
          <Confirming note="Progress and ticks reset">
            <ConfirmInline
              label={setStatus.isPending ? 'Starting…' : 'Rewatch'}
              tone="accent"
              size="sm"
              pending={setStatus.isPending}
              onConfirm={() => setStatus.mutate({ animeId, status: 'REPEATING' }, { onSuccess: close })}
              onCancel={() => setConfirm(null)}
            />
          </Confirming>
        )}
        {current &&
          (confirm === 'remove' ? (
            <Confirming note="Also removes it from AniList and MyAnimeList">
              <ConfirmInline
                label={remove.isPending ? 'Removing…' : 'Remove'}
                size="sm"
                pending={remove.isPending}
                onConfirm={() => remove.mutate(animeId, { onSuccess: close })}
                onCancel={() => setConfirm(null)}
              />
            </Confirming>
          ) : (
            <button type="button" onClick={() => setConfirm('remove')} className={cx(CHOICE, 'mt-1 text-red-300 hover:bg-recap/15')}>
              Remove from list
            </button>
          ))}
        {error && (
          <p className="mt-1 flex flex-wrap items-center gap-2 px-3 py-2 text-sm text-red-300">
            {error.message}
            <Button
              size="sm"
              onClick={() =>
                setStatus.isError && setStatus.variables
                  ? setStatus.mutate(setStatus.variables, { onSuccess: close })
                  : remove.mutate(animeId, { onSuccess: close })
              }
            >
              Try again
            </Button>
          </p>
        )}
      </PopSheet>
    </>
  )
}

function RateAction({ animeId, score }: { animeId: number; score: number }) {
  const qc = useQueryClient()
  const set = useMutation({
    mutationFn: (value: number) => api.post('/api/score', { animeId, score: value }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['anime', animeId] })
      void qc.invalidateQueries({ queryKey: ['library'] })
    },
  })
  const tens = Math.round(score / 10)
  const [open, setOpen] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const pick = (n: number) => {
    set.mutate(n * 10)
    setOpen(false)
  }

  return (
    <>
      <Tile
        ref={trigger}
        icon={<StarIcon filled={tens > 0} />}
        label={tens > 0 ? `${tens}/10` : 'Rate'}
        aria-label="Your score"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(true)}
      />
      <PopSheet open={open} onClose={() => setOpen(false)} title="Your score" anchor={trigger} width={272}>
        <div role="menu" aria-label="Your score" className="grid grid-cols-5 gap-1.5 sm:p-1">
          {Array.from({ length: 10 }, (_, i) => i + 1).map((n) => (
            <button
              key={n}
              type="button"
              role="menuitemradio"
              aria-checked={n === tens}
              autoFocus={n === tens}
              disabled={set.isPending}
              onClick={() => pick(n)}
              className={cx(
                'h-12 rounded-lg text-base font-semibold tabular-nums transition-colors sm:h-10 sm:text-sm',
                n === tens ? 'bg-accent-500 text-white' : 'bg-base-800 text-base-100 hover:bg-base-700',
              )}
            >
              {n}
            </button>
          ))}
          <button
            type="button"
            role="menuitemradio"
            aria-checked={tens === 0}
            disabled={set.isPending}
            onClick={() => pick(0)}
            className={cx(
              'col-span-5 h-12 rounded-lg text-[15px] font-medium transition-colors sm:h-10 sm:text-sm',
              tens === 0 ? 'bg-base-700 text-base-100' : 'bg-base-800 text-base-300 hover:bg-base-700',
            )}
          >
            Unrated
          </button>
        </div>
      </PopSheet>
    </>
  )
}

function useSetBookmark(animeId: number) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (b: Partial<Bookmark>) => api.post('/api/bookmarks', { animeId, ...b }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['anime', animeId] })
      void qc.invalidateQueries({ queryKey: ['bookmarks'] })
    },
  })
}

function FavouriteAction({ animeId, bookmark }: { animeId: number; bookmark?: Bookmark }) {
  const set = useSetBookmark(animeId)
  const on = !!bookmark?.favourite
  return (
    <Tile
      icon={<HeartIcon filled={on} />}
      label="Favourite"
      aria-pressed={on}
      title={on ? 'Remove from favourites' : 'Add to favourites'}
      disabled={set.isPending}
      onClick={() => set.mutate({ favourite: !on })}
    />
  )
}

const OPTION =
  'flex min-h-14 w-full items-center gap-3 rounded-lg px-3 py-2 text-left transition-colors hover:bg-base-700 disabled:opacity-50'

function DownloadAction({ animeId, episodes }: { animeId: number; episodes: Episode[] }) {
  const qc = useQueryClient()
  const clock = useNow()
  const queue = useDownloadQueue()
  const downloads = useDownloads()
  const room = useDriveRoom()
  const access = useAccess()
  const [open, setOpen] = useState(false)
  const [step, setStep] = useState<'choose' | 'space' | 'stop'>('choose')
  const [note, setNote] = useState<string | null>(null)
  const trigger = useRef<HTMLButtonElement>(null)

  const waiting = queue.data?.waiting?.[String(animeId)] ?? 0
  const aired = episodes.filter((e) => !isUnaired(e, clock))
  const missing = aired.filter((e) => !e.onDisk && !e.local)
  const unwatched = missing.filter((e) => !e.watched)
  const toDownload = (unwatched.length ? unwatched : missing).map((e) => e.number)
  const what =
    toDownload.length === 1
      ? '1 episode'
      : toDownload.length < missing.length
        ? `${toDownload.length} unwatched`
        : `all ${toDownload.length}`
  const sized = (downloads.data?.items ?? []).filter((d) => d.animeId === animeId && d.episodes.length > 0 && d.totalBytes > 0)
  const perEpisode = sized.length
    ? sized.reduce((n, d) => n + d.totalBytes / d.episodes.length, 0) / sized.length
    : EPISODE_BYTES_GUESS
  const need = toDownload.length * perEpisode
  const free = room.data?.freeBytes
  const wontFit = free != null && need > free
  const guest = !!access.data && !access.data.host
  const unseen = aired.filter((e) => !e.watched)
  const toSave = (unseen.length ? unseen : aired).map((e) => e.number)
  const why = guest ? cannotSave() : null

  const close = () => {
    setOpen(false)
    setStep('choose')
  }
  const refresh = () => refreshDownloads(qc)
  const downloadAll = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<{ queued: number }>('/api/download/episodes', { animeId, episodes: toDownload }),
    onSuccess: refresh,
  })
  const cancelAll = useMutation({
    mutationFn: () => api.del(`/api/download/all/${animeId}`),
    onSuccess: refresh,
  })
  const queueAll = () =>
    downloadAll.mutate(undefined, {
      onSuccess: close,
      onError: () => {
        setStep('choose')
        setOpen(true)
      },
    })
  const start = () => (wontFit ? setStep('space') : queueAll())
  const back = () => (guest ? setStep('choose') : close())
  const press = () => {
    setNote(null)
    downloadAll.reset()
    if (!guest && waiting > 0) setStep('stop')
    else if (!guest && toDownload.length > 0) {
      if (!wontFit) return queueAll()
      setStep('space')
    } else setStep('choose')
    setOpen(true)
  }

  const idle = waiting === 0 && toDownload.length === 0
  const done = idle && aired.length > 0
  const wideLabel = downloadAll.isPending
    ? 'Queuing…'
    : waiting > 0
      ? `${waiting} queued`
      : toDownload.length > 0
        ? `Download ${what}`
        : done
          ? 'All on the PC'
          : episodes.length > 0
            ? 'Nothing out yet'
            : 'No episodes yet'
  const caption = downloadAll.isPending ? (
    'Queuing…'
  ) : waiting > 0 ? (
    `${waiting} queued`
  ) : toDownload.length > 0 ? (
    what.charAt(0).toUpperCase() + what.slice(1)
  ) : done ? (
    <span className="text-emerald-300">All on PC</span>
  ) : episodes.length > 0 ? (
    'Not out yet'
  ) : (
    'No episodes'
  )

  return (
    <>
      <Tile
        ref={trigger}
        icon={done ? <CheckIcon className="size-6 shrink-0 text-emerald-300 sm:size-4" /> : <DownloadIcon />}
        label={<span className={cx(idle && !done && 'text-base-600')}>Download</span>}
        caption={caption}
        wide={
          idle && !done ? (
            <>
              <span className="text-base-600">Download</span>
              <span className="font-normal text-base-400">{wideLabel}</span>
            </>
          ) : (
            wideLabel
          )
        }
        aria-label={wideLabel}
        aria-haspopup={guest || waiting > 0 || wontFit || idle ? 'dialog' : undefined}
        disabled={downloadAll.isPending}
        onClick={press}
      />
      <PopSheet open={open} onClose={close} title="Download" anchor={trigger} width={320}>
        {step === 'space' ? (
          <div className="flex flex-wrap items-center gap-2 sm:p-1.5">
            <ConfirmInline
              tone="accent"
              label={downloadAll.isPending ? 'Queuing…' : 'Download anyway'}
              note={`${toDownload.length === 1 ? 'It needs' : `These ${toDownload.length} need`} about ${bytes(need)}, and ${room.data?.drive} has ${bytes(free ?? 0)} free.`}
              pending={downloadAll.isPending}
              onConfirm={queueAll}
              onCancel={back}
            />
          </div>
        ) : step === 'stop' ? (
          <div className="flex flex-wrap items-center gap-2 sm:p-1.5">
            <ConfirmInline
              label={cancelAll.isPending ? 'Stopping…' : `Stop ${waiting === 1 ? 'the download' : `${waiting} downloads`}`}
              note="The one in progress too"
              pending={cancelAll.isPending}
              onConfirm={() => cancelAll.mutate(undefined, { onSettled: close })}
              onCancel={back}
            />
          </div>
        ) : (
          <div className="space-y-1">
            {waiting > 0 ? (
              <div className="flex min-h-14 items-center gap-3 px-3 py-2">
                <DownloadIcon className="size-5 shrink-0 text-base-300" />
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium text-base-100">{waiting} queued on the PC</span>
                  <span className="block text-[13px] text-base-400">They download one at a time</span>
                </span>
                <Button size="sm" onClick={() => setStep('stop')}>
                  Stop
                </Button>
              </div>
            ) : toDownload.length > 0 ? (
              <button type="button" onClick={start} disabled={downloadAll.isPending} className={OPTION}>
                <DownloadIcon className="size-5 shrink-0 text-base-300" />
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium text-base-100">
                    {downloadAll.isPending ? 'Queuing…' : `Download ${what} to the PC`}
                  </span>
                  <span className="block text-[13px] text-base-400">About {bytes(need)}, one at a time</span>
                </span>
              </button>
            ) : done ? (
              <Link to="/downloads" className={OPTION}>
                <CheckIcon className="size-5 shrink-0 text-emerald-300" />
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium text-base-100">Every episode out is on the PC</span>
                  <span className="block text-[13px] text-base-400">Open Downloads</span>
                </span>
              </Link>
            ) : (
              <p className="px-3 py-2 text-sm text-base-400">
                {episodes.length > 0
                  ? 'Nothing is out yet. Auto-download in This show gets each episode as it comes out.'
                  : 'Yoru has no episode list for this show yet.'}
              </p>
            )}

            {guest &&
              aired.length > 0 &&
              (why ? (
                <Link to="/saved" className={OPTION}>
                  <DeviceIcon className="size-5 shrink-0 text-base-600" />
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium text-base-600">Save to this device</span>
                    <span className="block text-[13px] text-base-400">{why}</span>
                  </span>
                </Link>
              ) : (
                <button
                  type="button"
                  className={OPTION}
                  onClick={() => {
                    saveEpisodes(toSave.map((episode) => ({ animeId, episode })))
                    setNote(`Saving ${toSave.length} to this device: see Downloads, On this device`)
                  }}
                >
                  <DeviceIcon className="size-5 shrink-0 text-base-300" />
                  <span className="min-w-0 flex-1">
                    <span className="block text-sm font-medium text-base-100">
                      Save {toSave.length === 1 ? '1 episode' : toSave.length} to this device
                    </span>
                    <span className="block text-[13px] text-base-400">Watch with the PC off</span>
                  </span>
                </button>
              ))}

            {note && <p className="px-3 py-2 text-sm text-base-300">{note}</p>}
            {downloadAll.isError && (
              <p className="flex flex-wrap items-center gap-2 px-3 py-2 text-sm text-red-300">
                {(downloadAll.error as Error).message}
                <Button size="sm" onClick={start}>
                  Try again
                </Button>
              </p>
            )}
          </div>
        )}
      </PopSheet>
    </>
  )
}

function ShowLinks({
  anilist,
  mal,
  tone = 'bg-base-900 hover:bg-base-800',
  className,
}: {
  anilist?: number
  mal?: number
  tone?: string
  className?: string
}) {
  const links = [
    anilist ? { name: 'AniList', href: `https://anilist.co/anime/${anilist}` } : null,
    mal ? { name: 'MyAnimeList', href: `https://myanimelist.net/anime/${mal}` } : null,
  ].filter((l) => l !== null)
  if (links.length === 0) return null
  return (
    <ul className={cx('space-y-1.5', className)}>
      {links.map((l) => (
        <li key={l.name}>
          <a
            href={l.href}
            target="_blank"
            rel="noreferrer"
            className={cx(
              'flex min-h-11 items-center justify-between gap-3 rounded-card px-4 text-sm font-medium text-base-100 transition-colors',
              tone,
            )}
          >
            {l.name}
            <span aria-hidden className="text-base-400">
              ↗
            </span>
          </a>
        </li>
      ))}
    </ul>
  )
}

const BECAUSE: Record<string, string> = {
  watching: "On because it's on Watching",
  followed: 'On',
  auto: 'On with auto-download',
}

function ThisShowRow({
  animeId,
  notify,
  bookmark,
  sync,
}: {
  animeId: number
  notify?: FollowState
  bookmark?: Bookmark
  sync?: AnimeDetail['sync']
}) {
  const prefs = usePrefs(animeId)
  const [open, setOpen] = useState(false)
  const allOff = useAlertsOff()
  const auto = prefs.data?.effective['autodownload.enabled'] === 'true'
  const words = [
    notify && (allOff ? 'Alerts off in Settings' : notify.on ? 'Alerts on' : 'Alerts off'),
    prefs.data && (auto ? 'Auto-download on' : 'Auto-download off'),
    notify?.wanted ? `Ep ${notify.wanted} downloads when out` : null,
  ]
    .filter(Boolean)
    .join(' · ')

  return (
    <>
      <button
        type="button"
        aria-haspopup="dialog"
        onClick={() => setOpen(true)}
        className="press mt-4 flex min-h-14 w-full items-center gap-3 rounded-card bg-base-900 px-4 py-2.5 text-left transition-[background-color,transform] hover:bg-base-800 sm:max-w-xl"
      >
        <BellIcon on={!!notify?.on && !allOff} />
        <span className="min-w-0 flex-1">
          <span className="block text-[15px] font-medium text-base-100 sm:text-sm">This show</span>
          <span className="block text-[13px] text-base-400">{words || 'Alerts, auto-download, release groups, your note'}</span>
          {bookmark?.note && <span className="block truncate text-[13px] text-base-400">Note: {bookmark.note}</span>}
        </span>
        <span className="flex shrink-0 items-center gap-0.5 text-sm font-medium text-accent-300">
          Change
          <ChevronRight />
        </span>
      </button>
      <Sheet open={open} onClose={() => setOpen(false)} title="This show">
        <ShowSettings animeId={animeId} notify={notify} bookmark={bookmark} sync={sync} />
      </Sheet>
    </>
  )
}

function ShowSettings({
  animeId,
  notify,
  bookmark,
  sync,
}: {
  animeId: number
  notify?: FollowState
  bookmark?: Bookmark
  sync?: AnimeDetail['sync']
}) {
  const prefs = usePrefs(animeId)
  return (
    <>
      <div className="divide-y divide-white/[0.06]">
        {notify && <NotifyMe animeId={animeId} state={notify} />}
        <AutoDownload animeId={animeId} />
        <ShowGroups animeId={animeId} />
      </div>
      {prefs.isError && !prefs.data && (
        <p className="flex flex-wrap items-center gap-2 py-2 text-sm text-red-300">
          Couldn't load this show's settings.
          <Button size="sm" onClick={() => void prefs.refetch()}>
            Try again
          </Button>
        </p>
      )}
      <Note animeId={animeId} bookmark={bookmark} />
      <SyncNote sync={sync} />
    </>
  )
}

function ShowRow({ label, hint, control, children }: { label: string; hint?: ReactNode; control: ReactNode; children?: ReactNode }) {
  return (
    <div className="pb-1">
      <label className="flex min-h-14 cursor-pointer items-center justify-between gap-4 py-2">
        <span className="min-w-0">
          <span className="block text-[15px] text-base-100 sm:text-sm">{label}</span>
          {hint && <span className="mt-0.5 block text-[13px] text-base-400">{hint}</span>}
        </span>
        <ControlLabel.Provider value={label}>{control}</ControlLabel.Provider>
      </label>
      {children}
    </div>
  )
}

function ShowSubRow({ children }: { children: ReactNode }) {
  return (
    <div className="mb-2 flex flex-wrap items-center justify-between gap-x-4 gap-y-2 rounded-lg bg-base-800 px-3 py-2 text-sm text-base-300">
      {children}
    </div>
  )
}

function NotifyMe({ animeId, state }: { animeId: number; state: FollowState }) {
  const qc = useQueryClient()
  const update = (next: FollowState) =>
    qc.setQueryData<AnimeDetail>(['anime', animeId], (old) => (old ? { ...old, notify: next } : old))
  const set = useSetNotify(animeId)
  const unwant = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<FollowState>(`/api/anime/${animeId}/want`, { episode: 0 }),
    onSuccess: update,
  })
  const allOff = useAlertsOff()
  const words = allOff
    ? 'Off for every show in Settings'
    : state.on
      ? (BECAUSE[state.because ?? ''] ?? 'On')
      : 'Off'
  const error = (set.error ?? unwant.error) as Error | null

  return (
    <ShowRow
      label="Alert me about new episodes"
      hint={words}
      control={<Switch on={state.on} onChange={(on) => set.mutate(on)} disabled={set.isPending} />}
    >
      {allOff && (
        <ShowSubRow>
          Alerts are off in Settings
          <LinkButton to="/settings?tab=Notifications" size="sm">
            Turn alerts on
          </LinkButton>
        </ShowSubRow>
      )}
      {state.wanted ? (
        <ShowSubRow>
          Episode {state.wanted} downloads when it's out
          <Button size="sm" onClick={() => unwant.mutate()} disabled={unwant.isPending}>
            Cancel
          </Button>
        </ShowSubRow>
      ) : null}
      {error && (
        <p className="mb-2 flex flex-wrap items-center gap-2 text-sm text-red-300">
          {error.message}
          <Button
            size="sm"
            onClick={() => (set.isError ? set.mutate(set.variables ?? !state.on) : unwant.mutate())}
          >
            Try again
          </Button>
        </p>
      )}
    </ShowRow>
  )
}

function ShowBetterCopy({ animeId }: { animeId: number }) {
  const better = useBetterCopies(animeId)
  const offer = better.data?.items[0]
  if (!offer) return null
  return <BetterCopyOffer offer={offer} showTitle={false} className="surface mt-4 p-3 sm:max-w-xl" />
}

function AutoDownload({ animeId }: { animeId: number }) {
  const prefs = usePrefs(animeId)
  const setPref = useSetPref()

  const on = prefs.data?.effective['autodownload.enabled'] === 'true'
  if (!prefs.isSuccess) return null

  return (
    <ShowRow
      label="Auto-download new episodes"
      hint="Each one is queued as soon as it's out."
      control={
        <Switch
          on={on}
          onChange={(next) => setPref.mutate({ animeId, key: 'autodownload.enabled', value: String(next) })}
          disabled={setPref.isPending}
        />
      }
    >
      {on && (
        <ShowSubRow>
          Wait for a better release
          <WaitSelect
            value={prefs.data.overrides?.['autodownload.wait_hours'] ?? ''}
            inherit={prefs.data.defaults?.['autodownload.wait_hours'] ?? '0'}
            onChange={(value) => setPref.mutate({ animeId, key: 'autodownload.wait_hours', value })}
          />
        </ShowSubRow>
      )}
    </ShowRow>
  )
}

function parseGroups(raw: string | undefined): string[] {
  try {
    const parsed = JSON.parse(raw || '[]')
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

const SHOW_LISTS = [
  { key: 'release.prefer_groups', label: 'Prefer groups', short: 'Prefer', fallback: 'SubsPlease' },
  { key: 'release.avoid_groups', label: 'Avoid groups', short: 'Avoid', fallback: 'A group' },
  { key: 'release.exclude_terms', label: 'Exclude words', short: 'Exclude', fallback: 'x265 mini' },
] as const

function ShowGroups({ animeId }: { animeId: number }) {
  const prefs = usePrefs(animeId)
  const [open, setOpen] = useState(false)
  if (!prefs.isSuccess) return null

  const set = SHOW_LISTS.map((l) => ({ ...l, own: parseGroups(prefs.data.overrides?.[l.key]) })).filter((l) => l.own.length > 0)
  const summary = set.length
    ? set.map((l) => `${l.short} ${l.own.join(', ')}`).join(' · ')
    : 'Your choices from Settings'
  return (
    <ShowRow
      label="Release groups"
      hint={summary}
      control={
        <Button size="sm" onClick={() => setOpen((v) => !v)} aria-expanded={open} aria-label="Edit release groups">
          {open ? 'Done' : 'Edit'}
        </Button>
      }
    >
      {open && (
        <div className="mb-2 space-y-2">
          {SHOW_LISTS.map((l) => (
            <ShowList
              key={l.key}
              animeId={animeId}
              setting={l.key}
              label={l.label}
              own={parseGroups(prefs.data.overrides?.[l.key])}
              inherited={parseGroups(prefs.data.defaults?.[l.key])}
              fallback={l.fallback}
            />
          ))}
        </div>
      )}
    </ShowRow>
  )
}

function ShowList({
  animeId,
  setting,
  label,
  own,
  inherited,
  fallback,
}: {
  animeId: number
  setting: string
  label: string
  own: string[]
  inherited: string[]
  fallback: string
}) {
  const setPref = useSetPref()
  const saved = own.join(', ')
  const [text, setText] = useState(saved)
  const [shown, setShown] = useState(saved)
  if (shown !== saved) {
    setShown(saved)
    setText(saved)
  }

  const sent = useRef(saved)
  const save = () => {
    const items = text.split(',').map((g) => g.trim()).filter(Boolean)
    const value = items.join(',')
    if (value === own.join(',') || value === sent.current) return
    sent.current = value
    setPref.mutate({ animeId, key: setting, value: items.length > 0 ? JSON.stringify(items) : '' })
  }
  const latest = useRef(save)
  useEffect(() => {
    latest.current = save
  })
  useEffect(() => () => latest.current(), [])
  const id = `${setting}-${animeId}`

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-sm text-base-300">
      <label htmlFor={id} className="w-28 shrink-0">
        {label}
      </label>
      <TextInput
        id={id}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onBlur={save}
        onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
        placeholder={inherited.length > 0 ? inherited.join(', ') : fallback}
        className="min-w-40 flex-1"
      />
      <SaveHint state={setPref} />
      {own.length > 0 && (
        <button
          type="button"
          onClick={() => {
            setText('')
            sent.current = ''
            setPref.mutate({ animeId, key: setting, value: '' })
          }}
          className="tap relative text-base-400 transition-colors hover:text-base-100"
        >
          Clear
        </button>
      )}
    </div>
  )
}

function Note({ animeId, bookmark }: { animeId: number; bookmark?: Bookmark }) {
  const set = useSetBookmark(animeId)
  const saved = bookmark?.note ?? ''
  const [text, setText] = useState(saved)
  const [open, setOpen] = useState(!!saved)
  const [shown, setShown] = useState(saved)
  if (shown !== saved) {
    setShown(saved)
    setText(saved)
  }

  const sent = useRef(saved)
  const save = () => {
    if (text === saved || text === sent.current) return
    sent.current = text
    set.mutate({ note: text })
  }
  const latest = useRef(save)
  useEffect(() => {
    latest.current = save
  })
  useEffect(() => () => latest.current(), [])
  const id = `note-${animeId}`

  if (!open) {
    return (
      <Button size="sm" className="mt-3" onClick={() => setOpen(true)}>
        <PencilIcon />
        Add a note
      </Button>
    )
  }
  return (
    <div className="mt-3 space-y-1">
      <label htmlFor={id} className="block text-[13px] font-semibold text-base-400">
        Your note
      </label>
      <textarea
        id={id}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onBlur={save}
        placeholder="Your note (only you see it)"
        rows={2}
        className={cx(inputClass, 'block h-auto w-full py-2 pointer-coarse:h-auto')}
      />
      <SaveHint state={set} />
    </div>
  )
}

const KIND_LABELS: Record<string, string> = {
  SIDE_STORY: 'Side story',
  PARENT: 'Main story',
  SPIN_OFF: 'Spin-off',
  ALTERNATIVE: 'Alternative',
  SUMMARY: 'Recap',
}

const KIND_ORDER = [
  'Movie',
  'Special',
  'OVA',
  'ONA',
  'Side story',
  'Main story',
  'Spin-off',
  'Alternative',
  'Recap',
  'Related',
]

function rank(kind: string): number {
  const i = KIND_ORDER.indexOf(kind)
  return i === -1 ? KIND_ORDER.length : i
}

function RelatedRails({ entries }: { entries: RelatedEntry[] }) {
  if (entries.length === 0) return null

  const groups = new Map<string, RelatedEntry[]>()
  for (const e of entries) {
    const key = label(e)
    groups.set(key, [...(groups.get(key) ?? []), e])
  }
  if (groups.size < 2 || entries.length <= 6) {
    return <RelatedRail title="Related" entries={entries} />
  }

  return (
    <>
      {[...groups.keys()].sort((a, b) => rank(a) - rank(b)).map((kind) => (
        <RelatedRail
          key={kind}
          title={groups.get(kind)!.length > 1 ? plural(kind) : kind}
          entries={groups.get(kind)!}
          kind={kind}
        />
      ))}
    </>
  )
}

function RelatedRail({
  title,
  entries,
  kind,
}: {
  title: string
  entries: RelatedEntry[]
  kind?: string
}) {
  return (
    <Rail title={entries.length > 1 ? `${title} · ${entries.length}` : title}>
      {entries.map((e) => (
        <SeasonChip key={e.id} season={e} current={false} kind={kind ? undefined : label(e)} />
      ))}
    </Rail>
  )
}

function plural(kind: string): string {
  if (kind === 'Related') return 'Related'
  return kind === 'Alternative' ? 'Alternatives' : `${kind}s`
}

function label(e: RelatedEntry): string {
  if (e.format === 'MOVIE') return 'Movie'
  if (e.kind === 'SUMMARY') return 'Recap'
  if (e.format === 'OVA' || e.format === 'ONA' || e.format === 'SPECIAL') {
    return e.format === 'SPECIAL' ? 'Special' : e.format
  }
  return KIND_LABELS[e.kind] ?? 'Related'
}

function SeasonChip({
  season,
  current,
  kind,
}: {
  season: Season
  current: boolean
  kind?: string
}) {
  const tag = statusLabel(season.listStatus)
  const finished = !!season.episodes && season.progress >= season.episodes
  const watched = season.progress > 0 ? (finished ? 'Finished' : `${season.progress} watched`) : ''
  const done = season.listStatus === 'COMPLETED' || (!tag && finished)

  return (
    <Link
      to={`/anime/${season.id}`}
      aria-current={current ? 'page' : undefined}
      className={cx(
        'flex w-56 shrink-0 items-center gap-2.5 rounded-card p-2 transition-colors',
        current ? 'bg-accent-500/15' : 'bg-base-900 hover:bg-base-800',
      )}
    >
      {season.cover ? (
        <img src={season.cover} alt="" loading="lazy" className="h-14 w-10 shrink-0 rounded-md object-cover" />
      ) : (
        <div className="h-14 w-10 shrink-0 rounded-md bg-base-800" />
      )}
      <div className="min-w-0">
        {kind && <p className="text-xs font-semibold text-base-400">{kind}</p>}
        <p className="line-clamp-2 text-[13px] leading-snug text-base-100" title={season.english || season.romaji || undefined}>
          {season.english || season.romaji || `Anime ${season.id}`}
        </p>
        <p className="mt-0.5 text-xs text-base-500 tabular-nums">
          {season.year ?? 'TBA'}
          {season.episodes ? ` · ${season.episodes} eps` : ''}
        </p>
        {(tag || watched) && (
          <p className={cx('text-xs', done ? 'text-emerald-300' : 'text-accent-300')}>{tag || watched}</p>
        )}
      </div>
    </Link>
  )
}

function SyncNote({ sync }: { sync?: { anilist: boolean; mal: boolean } }) {
  if (!sync) return null
  const targets = [sync.anilist && 'AniList', sync.mal && 'MyAnimeList'].filter(Boolean)
  return (
    <p className="mt-3 text-[13px] text-base-400">
      {targets.length === 0
        ? 'Not on either tracker, so progress stays in Yoru.'
        : `Syncs to ${targets.join(' and ')}${targets.length === 1 ? ' only' : ''}.`}
    </p>
  )
}

const ICON = 'size-6 shrink-0 sm:size-4'

function BookmarkIcon({ filled }: { filled: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className={ICON} aria-hidden>
      <path
        d="M6 4.5A1.5 1.5 0 0 1 7.5 3h9A1.5 1.5 0 0 1 18 4.5v16l-6-4-6 4v-16Z"
        fill={filled ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function StarIcon({ filled }: { filled: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className={ICON} aria-hidden>
      <path
        d="m12 3.5 2.6 5.3 5.9.9-4.3 4.1 1 5.8L12 16.9l-5.2 2.7 1-5.8-4.3-4.1 5.9-.9L12 3.5Z"
        fill={filled ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function HeartIcon({ filled }: { filled: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className={cx(ICON, filled && 'text-accent-300')} aria-hidden>
      <path
        d="M12 20s-7.5-4.6-7.5-10.1A4.4 4.4 0 0 1 12 7.2a4.4 4.4 0 0 1 7.5 2.7C19.5 15.4 12 20 12 20Z"
        fill={filled ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function DownloadIcon({ className = ICON }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <path
        d="M12 4v11m0 0-4.5-4.5M12 15l4.5-4.5M5 19.5h14"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function DeviceIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <rect x="7" y="3" width="10" height="18" rx="2" fill="none" stroke="currentColor" strokeWidth="1.6" />
      <path d="M11 18h2" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
    </svg>
  )
}

function BellIcon({ on }: { on: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className="size-5 shrink-0 text-base-300" aria-hidden>
      <path
        d="M6 16.5V11a6 6 0 1 1 12 0v5.5l1.5 1.5h-15L6 16.5ZM10 20.5a2 2 0 0 0 4 0"
        fill={on ? 'currentColor' : 'none'}
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function PencilIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0" aria-hidden>
      <path
        d="M4.5 19.5h4l10-10a2.1 2.1 0 0 0-3-3l-10 10v3Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function ChevronDown() {
  return (
    <svg viewBox="0 0 20 20" className="size-3.5 shrink-0 text-base-400" aria-hidden>
      <path d="M6 8l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function ChevronRight() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0" aria-hidden>
      <path d="m9 5 7 7-7 7" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function CheckIcon({ className = 'size-4 shrink-0' }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden>
      <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function RewatchIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path
        d="M4 12a8 8 0 0 1 13.7-5.6L20 8.5M20 4v4.5h-4.5M20 12a8 8 0 0 1-13.7 5.6L4 15.5M4 20v-4.5h4.5"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function ClockIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <circle cx="12" cy="12" r="8.5" fill="none" stroke="currentColor" strokeWidth="1.8" />
      <path d="M12 7.5V12l3 2" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  )
}
