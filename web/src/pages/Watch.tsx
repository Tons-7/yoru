import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import {
  api,
  ApiError,
  beacon,
  query,
  type Episode,
  type FollowState,
  type PlaySession,
  type SkipMarkResult,
  type SkipRange,
  SOUND_PREFS,
  type SoundOptions,
  type StreamInfo,
  type UpNextItem,
  type VoiceLevel,
} from '../lib/api'
import { bytes, cx, relativeTime } from '../lib/format'
import {
  refreshDownloads,
  useAccess,
  useDownloads,
  useEpisodes,
  useFranchise,
  useNow,
  usePrefs,
  useRecommendations,
  useSetPref,
  useTrackers,
  type Season,
} from '../lib/queries'
import { joinRoom, overlayOn, parseTogether, saveSeat, savedSeat, useRoom, useTogetherSync } from '../lib/together'
import { JoinCard, TogetherPanel, TogetherToasts } from '../components/Together'
import { SaveToDevice } from '../components/SaveToDevice'
import { Anime4KDialog } from '../components/Anime4KDialog'
import { CharacterRail } from '../components/CharacterRail'
import { EpisodeList } from '../components/EpisodeList'
import { isUnaired } from '../lib/episodes'
import { ReleasePicker } from '../components/ReleasePicker'
import { OpenInApp } from '../components/OpenInApp'
import { StatusMenu } from '../components/StatusMenu'
import { Badge, Button, ErrorState, LinkButton, Segmented, Skeleton, Spinner } from '../components/ui'
import { useDocumentTitle } from '../lib/hooks'
import { buttonClass } from '../lib/ui'
import { getSleep, noteAutoStart, noteInput, setSleep, unattendedRun, useSleep, type Unattended } from '../lib/watching'

type AudioChoice = 'sub' | 'dub' | 'either'
import { Player } from '../player/Player'
import { deviceCaps } from '../player/decode'
import { cellular, LOWER, SAVER, setOnMobile, setSitting, stepBytes, streamChoice, useNetwork } from '../player/network'
import { MobileDataCard, NetworkNote } from '../components/MobileData'
import { SUB_LOOKS, type SubLook } from '../player/subLook'
import { currentVideo, togglePlay, useCurrentVideo, useDockDrag, usePlayingNow, type Corner } from '../lib/miniplayer'
import {
  autoTier,
  deviceUpscale,
  lowerTier,
  setAutoTier,
  setDeviceUpscale,
  upscaleHere,
  type AutoTier,
} from '../player/anime4k'

/** Mounted once by the layout: the watch page, or docked in a corner (mini) once left while playing. */
export function Watch({
  animeId,
  episode,
  mini = false,
  onGo,
  onClose,
}: {
  animeId: string
  episode: string
  mini?: boolean
  onGo?: (path: string) => void
  onClose?: () => void
}) {
  const id = Number(animeId)
  const ep = Number(episode)
  const routerNavigate = useNavigate()
  // Docked, auto next stays docked.
  const navigate = useCallback(
    (path: string) => (mini && onGo && path.startsWith('/watch/') ? onGo(path) : routerNavigate(path)),
    [mini, onGo, routerNavigate],
  )
  const dock = useDockDrag(mini)
  const qc = useQueryClient()

  const prefs = usePrefs(id)
  const setPref = useSetPref()
  const episodes = useEpisodes(id)

  const effective = prefs.data?.effective ?? {}
  const flag = (key: string) => effective[key] === 'true'
  const subLanguagesRaw = effective['subtitle.languages']
  const subLanguages = useMemo(() => {
    try {
      const parsed = JSON.parse(subLanguagesRaw || '["en"]')
      return Array.isArray(parsed) && parsed.length > 0 ? (parsed as string[]) : ['en']
    } catch {
      return ['en']
    }
  }, [subLanguagesRaw])
  const external = effective['playback.player'] !== 'browser' && !!effective['playback.player']

  // Write where the shown value came from, or a per-show override keeps winning and the switch looks stuck.
  const overrides = prefs.data?.overrides ?? {}
  const setFlag = (key: string) => (v: boolean) =>
    setPref.mutate({
      key,
      value: String(v),
      animeId: key in overrides ? id : undefined,
    })

  // Per-episode choices carry their episode: reset in an effect, they reached
  // the next episode's play a render late, hand-picked release and all.
  const epKey = `${id}-${ep}`

  // Prefs first, or the browser starts a stream mpv takes over; a raw is opted into per attempt only.
  const [rawFor, setRawFor] = useState('')
  const allowRaw = rawFor === epKey
  const setAllowRaw = (on: boolean) => setRawFor(on ? epKey : '')

  // Sub or dub is saved per show, and held here too so the switch doesn't wait on the save.
  const [audioPick, setAudioPick] = useState<{ key: number; audio: AudioChoice } | null>(null)
  const audio = audioPick?.key === id ? audioPick.audio : null
  const setAudio = (a: AudioChoice | null) => {
    setAudioPick(a ? { key: id, audio: a } : null)
    if (a) setPref.mutate({ key: 'audio.prefer', value: a, animeId: id })
  }
  const wantAudio: AudioChoice =
    audio ?? ((effective['audio.prefer'] as AudioChoice | undefined) ?? 'sub')

  // A release chosen by hand, for this episode only; a room's comes in the link.
  const [params] = useSearchParams()
  const [hashPick, setHashPick] = useState<{ key: string; hash: string } | null>(() => {
    const hash = params.get('hash')
    return hash ? { key: epKey, hash } : null
  })
  const infoHash = hashPick?.key === epKey ? hashPick.hash : undefined
  const setInfoHash = (hash?: string) => setHashPick(hash ? { key: epKey, hash } : null)
  const [picking, setPicking] = useState(false)
  const [inApp, setInApp] = useState(false)

  const [together, setTogether] = useState(() => parseTogether(params.get('together') ?? ''))
  const [seat, setSeat] = useState(() => (together ? savedSeat(together) : null))
  const [invite, setInvite] = useState(() => seat?.invite)
  // A room link opened while this view stays mounted (docked, or another room).
  const linkParam = params.get('together') ?? ''
  const hashParam = params.get('hash')
  useEffect(() => {
    const next = parseTogether(linkParam)
    if (!next || next.code === together?.code) return
    const kept = savedSeat(next)
    setTogether(next)
    setSeat(kept)
    setInvite(kept?.invite)
    if (hashParam) setHashPick({ key: epKey, hash: hashParam })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [linkParam])
  const room = useRoom(together, seat)
  const inRoom = !!together && !!seat && room.status === 'in'
  const isHost = !!room.me?.host
  const [toasts, setToasts] = useState(overlayOn)
  const trackers = useTrackers()
  const account = (provider: string) => trackers.data?.trackers.find((t) => t.provider === provider)?.user
  const trackerName = account('anilist') || account('mal') || ''
  useTogetherSync(useCurrentVideo(), inRoom ? room : null)

  const play = useQuery({
    enabled: id !== 0 && ep > 0 && prefs.isSuccess,
    queryKey: ['play', id, ep, external, allowRaw, wantAudio, infoHash],
    // Audio only when chosen here, so a held release in the other one is replaced.
    queryFn: ({ signal }) =>
      api.post<PlaySession>('/api/play', {
        animeId: id, episode: ep, external, allowRaw, audio: audio ?? undefined, infoHash,
      }, signal),
    retry: false,
    // 'static', not Infinity: a refetch of everything (title language, Try now) must not start playback again.
    staleTime: 'static',
    gcTime: 0,
  })

  const playingInMpv = !!play.data && play.data.player !== 'browser'

  // Follow the room's episode and release; moved on here, tell the room.
  const showTitleRef = useRef('')
  const roomState = room.view?.state
  const following = useRef(false)
  useEffect(() => {
    if (!inRoom || !together || !roomState || (roomState.animeId === id && roomState.episode === ep)) return
    following.current = true
    if (roomState.infoHash) setHashPick({ key: `${roomState.animeId}-${roomState.episode}`, hash: roomState.infoHash })
    navigate(`/watch/${roomState.animeId}/${roomState.episode}?together=${encodeURIComponent(together.href)}`)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inRoom, roomState?.animeId, roomState?.episode])
  const playedHash = play.data?.infoHash
  useEffect(() => {
    if (!inRoom || !roomState || !playedHash) return
    if (roomState.animeId === id && roomState.episode === ep) {
      following.current = false
      return
    }
    if (!following.current) void room.act('episode', { animeId: id, episode: ep, infoHash: playedHash }).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [inRoom, playedHash, id, ep])

  const [naming, setNaming] = useState(false)
  const startTogether = useMutation({
    mutationFn: (name: string) =>
      api.post<{ link: string; member: string; token: string }>('/api/together', {
        name, animeId: id, episode: ep, infoHash: play.data?.infoHash, title: showTitleRef.current,
        position: currentVideo()?.currentTime ?? 0, playing: !(currentVideo()?.paused ?? true),
      }),
    onSuccess: (r) => {
      const shared = parseTogether(r.link)
      if (!shared) return
      // Our own address here, the public one only for the invite; in the URL so a reload keeps the room.
      const href = `${window.location.origin}/together/${shared.code}?k=${shared.key}`
      const here = { ...shared, base: window.location.origin, href }
      const s = { member: r.member, token: r.token, invite: r.link }
      saveSeat(here, s)
      setTogether(here)
      setSeat(s)
      setInvite(r.link)
      routerNavigate(`/watch/${id}/${ep}?together=${encodeURIComponent(href)}`, { replace: true })
    },
  })
  // Back to full size, still in the room.
  const watchHere = `/watch/${id}/${ep}${together ? `?together=${encodeURIComponent(together.href)}` : ''}`
  const leaveTogether = () => {
    void room.act('leave').catch(() => {})
    if (together) saveSeat(together, null)
    setTogether(null)
    setSeat(null)
    setInvite(undefined)
  }

  // mpv opens the file itself and reports over its own IPC socket, so there is nothing here to transcode for.
  const source = playingInMpv ? undefined : play.data?.streamUrl
  // Read at open time, not part of the key: changing it mid-episode restarts the pass in place.
  const soundOptions = useMemo<SoundOptions>(
    () => ({
      voice: (effective['playback.voice'] || 'off') as VoiceLevel,
      night: flag('playback.night'),
      headphones: flag('playback.headphones'),
      mono: flag('playback.mono'),
      boost: (effective['playback.boost'] || 'off') as VoiceLevel,
    }),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- effective and flag follow prefs.data
    [prefs.data],
  )
  const soundAtOpen = useRef(soundOptions)
  soundAtOpen.current = soundOptions
  // Stable, or the player's key handler is torn down and re-added on every progress tick.
  const setSoundMutate = setPref.mutate
  const soundProp = useMemo(
    () => ({
      options: soundOptions,
      onChange: (next: SoundOptions) => {
        for (const key of Object.keys(SOUND_PREFS) as (keyof SoundOptions)[]) {
          if (next[key] !== soundOptions[key]) setSoundMutate({ key: SOUND_PREFS[key], value: String(next[key]) })
        }
      },
    }),
    [soundOptions, setSoundMutate],
  )
  // A copy this device said it plays but couldn't: that source opens converted from then on.
  const [convertAll, setConvertAll] = useState<string | null>(null)
  const strict = !!source && convertAll === source
  // This device's quality: its step, the mobile-data rule (or its card), or Auto's step down for a slow link.
  const net = useNetwork()
  // Nothing opens until the server says where this device is; unanswered counts as away, safer for data.
  const access = useAccess()
  const away = access.data ? !!access.data.away : access.isError
  const [stepped, setStepped] = useState<{ source: string; step: string } | null>(null)
  const choice = streamChoice(net, away, stepped && stepped.source === source ? stepped.step : undefined)
  // A reopen mid-episode (quality, converting) picks up where the picture was, this source's place only.
  const lastPosition = useRef<{ source?: string; at: number }>({ at: 0 })
  const [resumeAt, setResumeAt] = useState<{ source?: string; at: number } | null>(null)
  const reopenHere = () => {
    if (lastPosition.current.source === source) setResumeAt({ source, at: lastPosition.current.at })
  }
  // A note answered or closed stays away for its source.
  const [noteClosed, setNoteClosed] = useState<string | null>(null)
  const lastStep = useRef(choice.step)
  useEffect(() => {
    const here = lastPosition.current
    if (lastStep.current !== choice.step && here.source === source && here.at > 0) setResumeAt({ source, at: here.at })
    lastStep.current = choice.step
  }, [choice.step, source])
  const stream = useQuery({
    enabled: !!source && !choice.card && !access.isPending,
    queryKey: ['stream', id, ep, source, wantAudio, strict, choice.step],
    // Aborted when superseded: an abandoned open would otherwise pin and hold its episode when it lands.
    queryFn: async ({ signal }) =>
      api.post<StreamInfo>(
        `/api/stream/open${query({
          id, episode: ep, source, audio: wantAudio, caps: await deviceCaps(strict), quality: choice.step, ...soundAtOpen.current,
        })}`,
        undefined,
        signal,
      ),
    retry: 1,
    staleTime: 'static',
    // Never carried across a mount: leaving the page closes the session, so a
    // cached answer would describe a playlist that is no longer there.
    gcTime: 0,
  })

  // From pressing play to the stream opening: the search, then the release's first data, then its start.
  const startingNote = useStartingNote(id, ep, play.isPending || (!!source && stream.isPending))

  // AniSkip needs the stream's real length, remembered since an audio switch briefly clears it.
  const [lengthFor, setLengthFor] = useState<{ key: string; seconds: number } | null>(null)
  const episodeLength = lengthFor?.key === epKey ? lengthFor.seconds : 0
  useEffect(() => {
    const seconds = Math.round(stream.data?.duration ?? 0)
    if (seconds > 0) setLengthFor({ key: epKey, seconds })
  }, [stream.data?.duration, epKey])

  const skips = useQuery({
    enabled: id !== 0 && ep > 0 && episodeLength > 0,
    queryKey: ['skips', id, ep, episodeLength],
    queryFn: () =>
      api.get<{ ranges: SkipRange[]; finding: boolean; share: boolean }>(
        `/api/episode/skips${query({ anime: id, episode: ep, duration: episodeLength })}`,
      ),
    staleTime: 60 * 60_000,
    // Yoru is finding the opening or ending in the sound: it lands in seconds.
    refetchInterval: (q) => (q.state.data?.finding ? 5000 : false),
  })
  const refetchSkips = skips.refetch
  const skipMarks = useMemo(
    () => ({
      mark: (kind: 'op' | 'ed', start: number, end: number) =>
        api
          .post<SkipMarkResult>('/api/episode/skips/mark', { animeId: id, episode: ep, kind, start, end, duration: episodeLength })
          .then((saved) => {
            void refetchSkips()
            return saved
          }),
      forget: () => void api.post('/api/episode/skips/forget', { animeId: id, episode: ep }).then(() => refetchSkips()),
    }),
    [id, ep, episodeLength, refetchSkips],
  )
  const voteSkip = useCallback(
    (skipId: string, up: boolean) => beacon('/api/episode/skips/vote', { id: skipId, up }),
    [],
  )

  const download = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post('/api/download', { animeId: id, episode: ep }),
    onSuccess: () => refreshDownloads(qc),
  })
  // The page no longer remounts, so "Queued" would follow you to the next one.
  const resetDownload = download.reset
  useEffect(() => resetDownload(), [id, ep, resetDownload])

  // An episode already on disk isn't offered for download.
  const downloads = useDownloads()
  // A release bigger than the cache budget is removed once nothing plays it; said now, with the way out.
  const cache = useQuery({
    enabled: play.data?.source === 'torrent',
    queryKey: ['cache'],
    queryFn: () => api.get<{ budget: number }>('/api/cache'),
    staleTime: 60_000,
  })
  const playing = (downloads.data?.items ?? []).find((d) => !!play.data?.infoHash && d.infoHash === play.data.infoHash)
  const overBudget = !!playing && !playing.kept && !!cache.data?.budget && playing.totalBytes > cache.data.budget
  const keep = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post(`/api/downloads/${playing!.infoHash}/keep`),
    onSuccess: () => refreshDownloads(qc),
  })
  // A season pack lists every episode it serves.
  const held = (downloads.data?.items ?? []).some(
    (d) => d.animeId === id && d.episodes.includes(String(ep)) && d.percent >= 100,
  )

  const detail = useQuery({
    enabled: id !== 0,
    queryKey: ['anime', id],
    queryFn: () =>
      api.get<{
        title: string
        listStatus?: string
        score?: number
        notify?: FollowState
        status?: string
        episodes?: number
        coverImage?: { large?: string; extraLarge?: string }
        cover?: string
      }>(`/api/anime/${id}`),
    staleTime: 5 * 60_000,
  })

  const list = episodes.data?.items ?? []
  const current = list.find((e) => e.number === ep)
  // The next episode worth playing, as store.NextEpisode picks it (filler rule, nothing unaired).
  const skipFiller = flag('playback.skip_filler')
  const now = useNow()
  const next = list.find(
    (e) =>
      e.number > ep &&
      !isUnaired(e, now) &&
      !(skipFiller && (e.filler === 'filler' || e.recap)),
  )
  // With an Up next list, the list plays next; the show's own next is for when it has nothing else.
  const after = useQuery({
    enabled: id !== 0 && ep > 0,
    queryKey: ['upnext-after', id, ep],
    queryFn: () =>
      api.get<{ fromList: boolean; next?: UpNextItem; then?: UpNextItem }>(`/api/upnext/after${query({ id, episode: ep })}`),
    refetchInterval: 2 * 60_000,
  })
  const fromList = after.data?.fromList ? after.data.next : undefined
  // Past this entry's last aired episode: still airing (one is due), or a finale leading to the next season.
  const franchise = useFranchise(id)
  const upcoming = next ? undefined : list.find((e) => e.number > ep && isUnaired(e, now))
  // The entry's last episode by its count, as the server's play order has it: not filler skipped to the end.
  const total = detail.data?.episodes ?? 0
  const finale = !fromList && !next && !upcoming && total > 0 && ep >= total
  const seasons = franchise.data?.seasons ?? []
  const thisSeason = seasons.find((s) => s.id === id)
  const sequel =
    thisSeason && seasons.filter((s) => s.ordinal > thisSeason.ordinal).sort((a, b) => a.ordinal - b.ordinal)[0]
  // Played on into only once it has aired, as the server's play order does.
  const sequelOut = !!sequel && (sequel.status === 'RELEASING' || sequel.status === 'FINISHED')
  const sequelPath = finale && sequelOut ? `/watch/${sequel.id}/1` : undefined
  const hasNext = !!fromList || !!next || !!sequelPath

  // The release filename is what the server resolved; it is not what anyone
  // calls the episode. Fall back to it only when nothing better is known.
  const showTitle = detail.data?.title ?? play.data?.title ?? ''
  showTitleRef.current = showTitle
  const episodeLabel = [`Episode ${current?.display ?? ep}`, current?.titleEn]
    .filter(Boolean)
    .join(' · ')
  useDocumentTitle(showTitle && !mini ? `${showTitle} · Ep ${current?.display ?? ep}` : undefined)

  const reportProgress = useCallback(
    (position: number, duration: number, played: number, final: boolean, paused: boolean) => {
      lastPosition.current = { source, at: position }
      const body = { animeId: id, episode: ep, position, duration, played, paused, final }
      // sendBeacon survives the tab closing, which a fetch does not.
      if (!final) return beacon('/api/progress', body)
      // The last report of an episode (its end, Next, leaving) refreshes the ticks once the server has it.
      void api
        .post<{ watched: boolean }>('/api/progress', body)
        .then((r) => {
          if (!r.watched) return
          void qc.invalidateQueries({ queryKey: ['episodes', id] })
          void qc.invalidateQueries({ queryKey: ['anime', id] })
          // A watched episode left the list.
          void qc.invalidateQueries({ queryKey: ['upnext'] })
        })
        .catch(() => {})
    },
    [id, ep, qc, source],
  )

  const nextPath = fromList
    ? `/watch/${fromList.animeId}/${fromList.episode}`
    : next
      ? `/watch/${id}/${next.number}`
      : sequelPath
  // The end of an episode is decided here, so what this page saw then goes to the server's log.
  const note = useCallback(
    (event: string, detail: Record<string, unknown>) =>
      beacon('/api/playback/note', { animeId: id, episode: ep, event, detail }),
    [id, ep],
  )
  // At the end the episode after this one is the next to watch, so under the default rule its title shows.
  const endHides = (animeId: number, number: number, spoiler?: boolean) =>
    !!spoiler && !(effective['display.spoilers'] !== 'unwatched' && animeId === id && number === next?.number)
  const nextAnime = fromList ? fromList.animeId : !next && sequelPath ? sequel!.id : id
  const nextEpisode = fromList ? fromList.episode : next ? next.number : sequelPath ? 1 : undefined
  const advance = useCallback(
    (by: string) => {
      if (!nextPath) return
      // Started by the countdown alone, it adds to the run that "Still watching?" counts.
      if (by === 'countdown' && nextEpisode) noteAutoStart({ animeId: nextAnime, episode: nextEpisode })
      else noteInput()
      // Played on by hand after the sleep timer stopped things: someone's awake.
      if (by !== 'countdown' && getSleep().kind === 'fired') setSleep({ kind: 'off' })
      note('next', { by, to: nextPath })
      navigate(nextPath)
    },
    [nextPath, navigate, note, nextAnime, nextEpisode],
  )
  const goNext = useCallback((by: 'button' | 'key' | 'media' = 'button') => advance(by), [advance])
  // A subtitle delay kept for the show belongs to one group's timing; another group's release starts at zero.
  const group = /^\[([^\]]+)\]/.exec(play.data?.title ?? '')?.[1]
  const keptRaw = effective['subtitle.delay']
  const keptDelay = useMemo(() => {
    try {
      const kept = JSON.parse(keptRaw || 'null') as { group?: string; delay?: number } | null
      return kept && group && kept.group === group && typeof kept.delay === 'number' ? kept.delay : undefined
    } catch {
      return undefined
    }
  }, [keptRaw, group])
  const setPrefMutate = setPref.mutate
  const keepDelay = useCallback(
    (delay: number) =>
      setPrefMutate({ animeId: id, key: 'subtitle.delay', value: delay === 0 ? '' : JSON.stringify({ group, delay }) }),
    [id, group, setPrefMutate],
  )
  // Everywhere, not per show: a look is the viewer's eyes, not the release's.
  const setSubLook = useCallback((look: SubLook) => setPrefMutate({ key: 'subtitle.look', value: look }), [setPrefMutate])
  // The aired episode before this one, filler or not: going back is to rewatch, not to follow the list.
  const previous = [...list].reverse().find((e) => e.number < ep && !isUnaired(e, now))
  const previousPath = previous ? `/watch/${id}/${previous.number}` : undefined
  const goPrevious = useCallback(() => {
    if (!previousPath) return
    note('previous', { to: previousPath })
    navigate(previousPath)
  }, [previousPath, navigate, note])

  // Swapping in the searching panel would destroy the element holding
  // fullscreen, so after the first episode the player waits with its own spinner.
  const [streamed, setStreamed] = useState(false)
  useEffect(() => {
    if (stream.data) setStreamed(true)
  }, [stream.data])

  // Which episode ended, not just that one did: a plain flag survives navigation and skips an episode.
  const [endedKey, setEndedKey] = useState<string | null>(null)
  const ended = endedKey === `${id}-${ep}`
  const endedAt = useRef(0)
  const endedNow = useRef(ended)
  endedNow.current = ended
  // The card the overlay below would show at the end, as the log has it.
  const card =
    play.isError && !stream.data
      ? 'release error'
      : stream.isError && !stream.data
        ? 'stream error'
        : fromList
          ? 'up next list'
          : next
            ? 'up next'
            : upcoming
              ? 'airs later'
              : finale
                ? sequelPath
                  ? 'next season'
                  : 'finale'
                : 'none'
  // In a room only the host's countdown runs; the rest follow.
  const autoNext = flag('playback.autonext') && (!inRoom || isHost)
  const sleep = useSleep()
  // Past the limit of untouched episodes the card asks instead of counting down; so does a timer gone off.
  const stillLimit = Number(effective['playback.still_watching'] ?? '3')
  const [asked, setAsked] = useState<{ key: string; run: Unattended[] } | null>(null)
  const askedRun = asked?.key === `${id}-${ep}` ? asked.run : null
  const counting = autoNext && sleep.kind !== 'fired' && sleep.kind !== 'episode' && !askedRun
  const onEnded = useCallback(() => {
    endedAt.current = Date.now()
    setEndedKey(`${id}-${ep}`)
    if (getSleep().kind === 'episode') setSleep({ kind: 'fired' })
    const run = unattendedRun()
    if (autoNext && stillLimit > 0 && run.length >= stillLimit) setAsked({ key: `${id}-${ep}`, run })
    note('ended', {
      card,
      next: next?.number ?? 'none',
      list: fromList ? `${fromList.animeId}-${fromList.episode}` : 'none',
      autoNext,
      episodes: list.length,
      unattended: run.length,
    })
  }, [id, ep, card, next, fromList, autoNext, stillLimit, list.length, note])

  // Any key, tap, wheel or mouse movement on the page counts as someone watching.
  useEffect(() => {
    const seen = (e: Event) => {
      if (e instanceof PointerEvent && e.type === 'pointermove' && e.pointerType !== 'mouse') return
      noteInput()
    }
    const events = ['keydown', 'pointerdown', 'pointermove', 'wheel'] as const
    for (const name of events) window.addEventListener(name, seen, { passive: true })
    return () => {
      for (const name of events) window.removeEventListener(name, seen)
    }
  }, [])
  const clearEnded = useCallback(
    (by: string) => {
      if (endedNow.current) note('countdown-cleared', { by, afterMs: Date.now() - endedAt.current })
      setEndedKey(null)
    },
    [note],
  )

  // A reaped session: /api/play revives the torrent and the reopen rebuilds it; throws so the player shows it.
  // Refetched outright: invalidating leaves a 'static' query alone.
  const refetchPlay = play.refetch
  const refetchStream = stream.refetch
  const recoverSession = useCallback(async () => {
    const revived = await refetchPlay({ throwOnError: true })
    // Another address for the file is a new stream query, which opens on its own.
    if (revived.data?.streamUrl !== source) return
    await refetchStream({ throwOnError: true })
  }, [refetchPlay, refetchStream, source])

  // Copying more than H.264 and AAC or MP3, a stream may hold what this device claimed but can't decode.
  const opened = stream.data
  const sent = opened?.audio?.[opened.audioTrack ?? 0]
  const copiesMore =
    (!!opened?.plan?.videoCopy && !(opened.video?.codec === 'h264' && (opened.video.bitDepth ?? 8) <= 8)) ||
    (!!opened?.plan?.audioCopy && !['aac', 'mp3'].includes(sent?.codec ?? 'aac'))
  const convertInstead = useCallback(() => {
    if (!copiesMore || strict || !source) return false
    if (lastPosition.current.source === source) setResumeAt({ source, at: lastPosition.current.at })
    setConvertAll(source)
    return true
  }, [copiesMore, strict, source])

  // Auto, away from home: a link that keeps stalling steps down a quality; "Back to original" keeps it there.
  const [keepOriginal, setKeepOriginal] = useState<string | null>(null)
  const slowLink =
    net.quality === 'auto' && away && !choice.card && keepOriginal !== source && LOWER[choice.step ?? 'original']
      ? () => {
          reopenHere()
          setStepped({ source: source!, step: LOWER[choice.step ?? 'original']! })
          setNoteClosed(null)
        }
      : undefined
  const networkNote =
    choice.card && stream.data && noteClosed !== source ? (
      // Onto mobile data mid-episode: it keeps playing, data saver a tap away.
      <NetworkNote
        text={cellular() ? 'Now on mobile data' : 'Now away from home'}
        action="Data saver"
        onAction={() => {
          reopenHere()
          setSitting('saver')
        }}
        onClose={() => {
          setNoteClosed(source ?? null)
          setSitting('full')
        }}
      />
    ) : stepped && stepped.source === source && noteClosed !== source ? (
      <NetworkNote
        text={`${stepped.step}p: the connection couldn't keep up`}
        action="Back to original"
        onAction={() => {
          reopenHere()
          setStepped(null)
          setKeepOriginal(source ?? null)
        }}
        onClose={() => setNoteClosed(source ?? null)}
      />
    ) : null

  // Releasing the transcode session frees an ffmpeg process; leaving it to the
  // idle reaper keeps a CPU busy for a minute after leaving the page.
  useEffect(() => {
    const streamId = stream.data?.id
    return () => {
      if (streamId) void api.del(`/api/stream/${streamId}`).catch(() => {})
    }
  }, [stream.data?.id])

  // A first episode's opening is new and a finale's ending is often its own: heard, unless asked otherwise.
  const bookend =
    effective['playback.skip_keep_bookends'] !== 'false' &&
    (ep === 1 || (detail.data?.status === 'FINISHED' && !!detail.data.episodes && ep === detail.data.episodes))
  const skipOp = flag('playback.autoskip_op') && !bookend
  const skipEd = flag('playback.autoskip_ed') && !bookend
  const skipRecap = flag('playback.autoskip_recap')
  const skipPreview = flag('playback.autoskip_preview')
  // In a room only the host's auto-skips act, or each guest's settings would move everyone.
  const guest = inRoom && !isHost
  const autoSkip = useMemo(
    () => ({ op: skipOp && !guest, ed: skipEd && !guest, recap: skipRecap && !guest, preview: skipPreview && !guest }),
    [skipOp, skipEd, skipRecap, skipPreview, guest],
  )

  // Per episode, unlike the toggles beside it: the saved default is untouched.
  const [upscale, setUpscale] = useState<{ enabled: boolean; mode: string } | null>(null)
  const [tuning, setTuning] = useState(false)
  const [optionsOpen, setOptionsOpen] = useState(false)
  useEffect(() => setUpscale(null), [id, ep])

  // The saved default is shared with mpv on the host; each browser decides whether it applies here.
  const [deviceChoice, setDeviceChoice] = useState(deviceUpscale)
  const [settled, setSettled] = useState(autoTier)
  const [steppedTo, setSteppedTo] = useState<AutoTier | null>(null)
  const here = upscaleHere(deviceChoice, flag('playback.anime4k'), settled)
  // Under Auto a GPU that falls behind drops a tier, and the device remembers where it settled.
  const stepDown = () => {
    const next = lowerTier(here.tier)
    setAutoTier(next)
    setSettled(next)
    setSteppedTo(next)
  }
  const upscaling = {
    enabled: upscale?.enabled ?? here.enabled,
    mode: upscale?.mode ?? effective['playback.anime4k_mode'] ?? 'A',
    tier: here.tier,
    // Not for a tier picked by hand, nor an episode switched on by hand.
    onSlow: here.auto && !upscale ? stepDown : undefined,
  }

  if (!id || !ep) return <ErrorState error={new Error('Unknown episode')} />

  const failure =
    play.error instanceof ApiError
      ? (play.error.body as { rawAvailable?: boolean; rawTitle?: string; notOut?: boolean } | undefined)
      : undefined

  // Docked, only classes change, never the tree, so the player isn't rebuilt.
  return (
    <div
      ref={dock.ref}
      onPointerDownCapture={dock.onPointerDownCapture}
      onClickCapture={dock.onClickCapture}
      className={cx(
        'bg-base-950',
        mini &&
          'fixed z-40 w-[22rem] cursor-grab touch-none rounded-xl shadow-2xl ring-1 shadow-black/60 ring-white/10 select-none active:cursor-grabbing max-sm:inset-x-2 max-sm:w-auto',
        mini && DOCK[dock.corner],
      )}
    >
      <div className={cx(!mini && 'mx-auto w-full max-w-[1600px] px-0 sm:px-6 sm:py-4')}>
        <div className={cx(!mini && 'grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]')}>
          <div className={cx('min-w-0', mini && 'max-sm:flex max-sm:items-center')}>
            {/* No overflow-hidden: clipping a composited video blacks it out. */}
            <div className={cx('relative bg-black', mini ? 'rounded-t-xl max-sm:w-36 max-sm:shrink-0 max-sm:rounded-l-xl max-sm:rounded-tr-none' : 'sm:rounded-xl')}>
              {play.isError && !streamed ? (
                <NoRelease
                  message={(play.error as Error).message}
                  animeId={id}
                  episode={ep}
                  notOut={failure?.notOut}
                  rawAvailable={!allowRaw && !!failure?.rawAvailable}
                  rawTitle={failure?.rawTitle}
                  onRetry={() => play.refetch()}
                  onPlayRaw={() => setAllowRaw(true)}
                  onPick={() => setPicking(true)}
                />
              ) : play.isPending && !streamed ? (
                <Searching note={startingNote} />
              ) : playingInMpv ? (
                <MpvPanel title={play.data?.title ?? showTitle} animeId={id} episode={ep} player={play.data?.player ?? 'mpv'} />
              ) : (
                <Player
                  stream={stream.data}
                  starting={startingNote}
                  startAt={resumeAt && resumeAt.source === source ? resumeAt.at : (play.data?.startAt ?? 0)}
                  skips={skips.data?.ranges ?? []}
                  autoSkip={autoSkip}
                  onSkipVote={skips.data?.share ? voteSkip : undefined}
                  skipMarks={skipMarks}
                  autoPlay={flag('playback.autoplay')}
                  subLanguages={subLanguages}
                  withDub={(['signs', 'off', 'same'] as const).find((v) => v === effective['subtitle.with_dub']) ?? 'signs'}
                  keptDelay={keptDelay}
                  onKeepDelay={group ? keepDelay : undefined}
                  subLook={SUB_LOOKS.find((l) => l.value === effective['subtitle.look'])?.value ?? ''}
                  onSubLook={setSubLook}
                  upscale={upscaling}
                  sound={soundProp}
                  title={showTitle}
                  subtitle={episodeLabel}
                  artwork={detail.data?.coverImage?.large ?? detail.data?.coverImage?.extraLarge ?? detail.data?.cover}
                  onProgress={reportProgress}
                  onEnded={onEnded}
                  onNext={hasNext ? goNext : undefined}
                  onPrevious={previousPath ? goPrevious : undefined}
                  onTVMovedOn={(anime, episode) => navigate(`/watch/${anime}/${episode}`)}
                  // Back into the finished episode: the countdown must not take the viewer away mid-rewatch.
                  onResume={clearEnded}
                  onEndStuck={(detail) => note('end-stuck', detail)}
                  onSessionLost={recoverSession}
                  onUndecodable={convertInstead}
                  onSlowLink={slowLink}
                  onPickRelease={() => setPicking(true)}
                  castBurn={flag('cast.burn_subtitles')}
                  mini={mini ? { onExpand: () => routerNavigate(watchHere) } : undefined}
                  // Inside the player, or fullscreen hides it; failures too, unless the player can recover.
                  overlay={<>
                    {networkNote}
                    {inRoom && <TogetherToasts room={room} on={toasts} />}
                    {choice.card && !stream.data && !play.isError ? (
                      <MobileDataCard
                        card={choice.card}
                        original={play.data?.size}
                        saver={stepBytes(SAVER, current?.duration || (current?.runtime ?? 24) * 60)}
                        onPlay={(how, remember) => {
                          if (remember) setOnMobile(how)
                          setSitting(how)
                        }}
                        onCancel={() => navigate(`/anime/${id}`)}
                      />
                    ) : play.isError && !stream.data ? (
                      <NoRelease
                        inPlayer
                        message={(play.error as Error).message}
                        animeId={id}
                        episode={ep}
                        notOut={failure?.notOut}
                        rawAvailable={!allowRaw && !!failure?.rawAvailable}
                        rawTitle={failure?.rawTitle}
                        onRetry={() => play.refetch()}
                        onPlayRaw={() => setAllowRaw(true)}
                        onPick={() => setPicking(true)}
                      />
                    ) : stream.isError && !stream.data ? (
                      // The release was found; opening it is what failed.
                      <NoRelease
                        inPlayer
                        message={(stream.error as Error).message}
                        animeId={id}
                        needsTranscoder={
                          stream.error instanceof ApiError &&
                          (stream.error.body as { code?: string } | undefined)?.code === 'no-transcoder'
                        }
                        onRetry={() => stream.refetch()}
                        onPick={() => setPicking(true)}
                      />
                    ) : ended && askedRun && hasNext ? (
                      <StillWatching
                        run={askedRun}
                        onKeep={() => {
                          setAsked(null)
                          advance('card')
                        }}
                        onStop={() => {
                          setAsked(null)
                          clearEnded('still-watching')
                        }}
                      />
                    ) : ended && fromList ? (
                      <UpNext
                        label="Up next · from your list"
                        line={`${fromList.animeId === id ? '' : `${fromList.title} · `}Episode ${fromList.episode}${
                          fromList.animeId === id && fromList.episodeTitle && !endHides(fromList.animeId, fromList.episode, fromList.spoiler)
                            ? ` · ${fromList.episodeTitle}`
                            : ''
                        }`}
                        then={after.data?.then && `Then ${after.data.then.title} · Ep ${after.data.then.episode}`}
                        auto={counting}
                        stopped={sleep.kind === 'fired' && autoNext}
                        onPlay={advance}
                        onCancel={() => clearEnded('cancel')}
                      />
                    ) : ended && next ? (
                      <UpNext
                        label="Up next"
                        line={`Episode ${next.display || next.number}${
                          next.titleEn && !endHides(id, next.number, next.spoiler) ? ` · ${next.titleEn}` : ''
                        }`}
                        auto={counting}
                        stopped={sleep.kind === 'fired' && autoNext}
                        onPlay={advance}
                        onCancel={() => clearEnded('cancel')}
                      />
                    ) : ended && sequelPath && sequel ? (
                      <UpNext
                        label="Season finale · next season"
                        line={`${sequel.english || sequel.romaji} · Episode 1`}
                        auto={counting}
                        stopped={sleep.kind === 'fired' && autoNext}
                        onPlay={advance}
                        onCancel={() => clearEnded('cancel')}
                      />
                    ) : ended && (finale || upcoming) ? (
                      <EndOfAired
                        animeId={id}
                        upcoming={upcoming}
                        sequel={finale ? sequel : undefined}
                        score={detail.data?.score ?? 0}
                        alerting={!!detail.data?.notify?.on}
                        onClose={() => clearEnded('cancel')}
                      />
                    ) : null}
                  </>}
                />
              )}
            </div>

            {/* Not drawn while docked: its lists and rails would keep fetching for nobody. */}
            {!mini && (
            <div className="space-y-3 p-3 sm:px-0 sm:pt-4">
              <div className="flex flex-wrap items-start gap-x-4 gap-y-3">
                {/* What is playing, and the way back to its page. */}
                <div className="mr-auto min-w-0">
                  <Link
                    to={`/anime/${id}`}
                    className="group inline-flex items-center gap-1.5 font-display text-xl font-bold tracking-tight text-white transition-colors hover:text-accent-200 sm:text-2xl"
                  >
                    <span className="line-clamp-1">{showTitle || 'Series'}</span>
                    <span className="text-base-500 transition-transform group-hover:translate-x-0.5">›</span>
                  </Link>
                  <p className="mt-0.5 flex min-w-0 items-center gap-2 text-sm text-base-400">
                    <span className="line-clamp-1">{episodeLabel}</span>
                    {play.data?.source && (
                      <Badge tone={play.data.source === 'local' ? 'accent' : 'neutral'}>
                        {play.data.source === 'local' ? 'Local file' : 'Torrent'}
                      </Badge>
                    )}
                  </p>
                </div>

                <div className="flex flex-wrap items-center gap-2">
                  <StatusMenu animeId={id} current={detail.data?.listStatus} />

                  <Button onClick={() => setPicking(true)}>
                    {infoHash ? 'Release chosen' : 'Choose release'}
                  </Button>

                  <Button
                    onClick={() => download.mutate()}
                    disabled={held || download.isPending || download.isSuccess}
                  >
                    {held ? 'Downloaded' : download.isSuccess ? 'Queued' : 'Download'}
                  </Button>

                  {source && <Button onClick={() => setInApp(true)}>Open or save</Button>}

                  {!playingInMpv && <SaveToDevice animeId={id} episode={ep} />}

                  {!together && !naming && !playingInMpv && (
                    <Button
                      onClick={() => {
                        startTogether.reset()
                        setNaming(true)
                      }}
                      disabled={!play.data}
                      title="Watch in step with friends who have their own Yoru"
                    >
                      Watch together
                    </Button>
                  )}

                  {hasNext && (
                    <Button
                      variant="primary"
                      onClick={() => goNext('button')}
                      title={fromList ? `${fromList.title} · Episode ${fromList.episode}` : undefined}
                    >
                      {fromList ? 'Up next ›' : 'Next episode ›'}
                    </Button>
                  )}
                </div>
              </div>

              {startTogether.isError && (
                <p className="flex flex-wrap items-center gap-2 text-sm text-recap">
                  {(startTogether.error as Error).message}
                  {startTogether.error instanceof ApiError && (startTogether.error.body as { needs?: string })?.needs === 'https' && (
                    <LinkButton to="/settings?tab=Devices" size="sm">
                      Set up HTTPS
                    </LinkButton>
                  )}
                </p>
              )}
              {naming && !together && (
                <JoinCard
                  host
                  title={showTitle}
                  name={trackerName}
                  // A failure closes the card for the message below, with its Set up HTTPS.
                  onJoin={(name) => startTogether.mutateAsync(name).catch(() => {}).finally(() => setNaming(false))}
                  onCancel={() => setNaming(false)}
                />
              )}
              {together && !seat && (
                <JoinCard
                  title={showTitle}
                  name={trackerName}
                  onJoin={async (name) => {
                    const s = await joinRoom(together, name)
                    const kept = { member: s.member, token: s.token }
                    saveSeat(together, kept)
                    setSeat(kept)
                  }}
                />
              )}
              {together && seat && (
                <TogetherPanel room={room} invite={invite} onLeave={leaveTogether} overlay={toasts} onOverlay={setToasts} />
              )}

              {overBudget && (
                <div
                  role="status"
                  className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2.5 text-sm text-amber-100"
                >
                  <p>
                    This release is {bytes(playing.totalBytes)}, bigger than the {bytes(cache.data!.budget)} cache: it is
                    removed once you stop watching unless you keep it.
                  </p>
                  <Button variant="primary" onClick={() => keep.mutate()} disabled={keep.isPending}>
                    Keep this episode
                  </Button>
                  {keep.isError && <p className="w-full text-xs text-recap">{(keep.error as Error).message}</p>}
                </div>
              )}

              {/* Until settings load every switch reads off, and a click would write wrong. */}
              {!prefs.data ? (
                <Skeleton className="h-12 w-full rounded-lg" />
              ) : (
                <>
                  {/* A phone keeps the switches folded: they're set once a show, not every episode. */}
                  <button
                    type="button"
                    aria-expanded={optionsOpen}
                    onClick={() => setOptionsOpen((v) => !v)}
                    className="flex min-h-11 w-full items-center justify-between rounded-lg bg-base-900/60 px-3 text-sm text-base-200 shadow-card sm:hidden"
                  >
                    Playback options
                    <span aria-hidden className={cx('transition-transform', optionsOpen && 'rotate-180')}>
                      ▾
                    </span>
                  </button>
                  <div
                    className={cx(
                      'flex flex-wrap items-center gap-x-5 gap-y-2 rounded-lg bg-base-900/60 p-2.5 shadow-card',
                      !optionsOpen && 'max-sm:hidden',
                    )}
                  >
                    {/* Three scopes, each a group that wraps whole, so its label never leaves its switches. */}
                    <OptionGroup label="This show">
                      {/* Switching resolves another release, as most carry one language. */}
                      <Segmented
                        size="sm"
                        value={wantAudio}
                        onChange={setAudio}
                        options={[
                          { value: 'sub', label: 'Sub' },
                          { value: 'dub', label: 'Dub' },
                          { value: 'either', label: 'Either' },
                        ]}
                      />
                      <Toggle
                        label="Skip filler"
                        on={skipFiller}
                        onChange={(v) => setPref.mutate({ key: 'playback.skip_filler', value: String(v), animeId: id })}
                      />
                      {/* Per show: some openings are worth hearing every time. Settings holds the default. */}
                      <Toggle
                        label="Skip opening"
                        on={flag('playback.autoskip_op')}
                        onChange={(v) => setPref.mutate({ key: 'playback.autoskip_op', value: String(v), animeId: id })}
                      />
                      <Toggle
                        label="Skip ending"
                        on={flag('playback.autoskip_ed')}
                        onChange={(v) => setPref.mutate({ key: 'playback.autoskip_ed', value: String(v), animeId: id })}
                      />
                    </OptionGroup>

                    {/* A toggle flipped here becomes the new default. */}
                    <OptionGroup label="Everywhere">
                      <Toggle label="Auto play" on={flag('playback.autoplay')} onChange={setFlag('playback.autoplay')} />
                      <Toggle label="Auto next" on={flag('playback.autonext')} onChange={setFlag('playback.autonext')} />
                    </OptionGroup>

                    {!playingInMpv && (
                      <OptionGroup label="This device">
                        <button
                          onClick={() => setTuning(true)}
                          className={cx(
                            'rounded-md px-2.5 py-1 text-xs font-medium transition-colors pointer-coarse:min-h-11',
                            upscaling.enabled
                              ? 'bg-accent-500/15 text-accent-300 hover:bg-accent-500/25'
                              : 'bg-base-800 text-base-300 hover:bg-base-700',
                          )}
                        >
                          Anime4K{upscaling.enabled ? ` · ${upscaling.mode}` : ''}
                        </button>
                      </OptionGroup>
                    )}
                  </div>
                </>
              )}

              {steppedTo && (
                <p className="text-xs text-base-400">
                  {steppedTo === 'off'
                    ? 'Anime4K turned itself off: this device was falling behind.'
                    : `Anime4K dropped to ${steppedTo}: this device was falling behind.`}
                </p>
              )}

              {download.isError && (
                <p className="text-xs text-recap">{(download.error as Error).message}</p>
              )}

              <EpisodeAbout
                title={showTitle}
                label={episodeLabel}
                overview={current?.overview}
                still={current?.still}
                file={play.data?.title}
              />

              {/* The show's cast, not this episode's (MAL has none), as the heading says. */}
              <CharacterRail animeId={id} limit={8} title="Cast" />
            </div>
            )}
            {mini && (
              <MiniBar title={showTitle} episode={episodeLabel} onExpand={() => routerNavigate(watchHere)} onClose={() => onClose?.()} />
            )}
          </div>

          {!mini && (
          <aside className="px-3 pb-8 sm:px-0">
            <h2 className="mb-2 section-title">
              Episodes
            </h2>
            {list.length > 0 ? (
              <EpisodeList animeId={id} episodes={list} current={ep} compact />
            ) : (
              <p className="rounded-lg border border-base-850 p-4 text-sm text-base-500">
                No episode list available.
              </p>
            )}
          </aside>
          )}
        </div>
      </div>

      <Anime4KDialog
        open={tuning}
        enabled={upscaling.enabled}
        mode={upscaling.mode}
        onClose={() => setTuning(false)}
        onChange={setUpscale}
        device={deviceChoice}
        onDevice={(choice) => {
          setDeviceUpscale(choice)
          setDeviceChoice(choice)
          // Choosing Auto again starts it from the top.
          if (choice === 'auto') {
            setAutoTier(null)
            setSettled(null)
          }
          setSteppedTo(null)
          // The device's default just changed; this episode follows it.
          setUpscale(null)
        }}
      />

      {picking && (
        <ReleasePicker
          animeId={id}
          episode={ep}
          audio={wantAudio}
          current={play.data?.infoHash}
          onClose={() => setPicking(false)}
          onPick={(hash) => {
            setPicking(false)
            setInfoHash(hash)
          }}
        />
      )}
      {source && <OpenInApp source={source} open={inApp} onClose={() => setInApp(false)} />}
    </div>
  )
}

/** What the episode is, under the player, beyond a release filename. */
function EpisodeAbout({
  title,
  label,
  overview,
  still,
  file,
}: {
  title: string
  label: string
  overview?: string
  still?: string
  file?: string
}) {
  if (!title && !overview) return null

  return (
    <section className="flex gap-4 rounded-lg bg-base-900/60 p-4 shadow-card">
      {still && (
        <img
          src={still}
          alt=""
          loading="lazy"
          className="hidden h-24 w-40 shrink-0 rounded-md object-cover sm:block"
        />
      )}
      <div className="min-w-0">
        <h2 className="truncate text-base font-semibold text-base-100">{title}</h2>
        <p className="mt-0.5 text-sm text-accent-400">{label}</p>
        {overview ? (
          <p className="mt-2 line-clamp-3 text-sm leading-relaxed text-base-300">{overview}</p>
        ) : (
          <p className="mt-2 text-sm text-base-500">No synopsis for this episode yet.</p>
        )}
        {/* The release is worth being able to check, but it is not the title. */}
        {file && <p className="mt-2 truncate text-xs text-base-500">{file}</p>}
      </div>
    </section>
  )
}

/** What a starting episode is waiting on, as the server reports it. */
interface PlayStage {
  step?: 'search' | 'files' | 'data' | 'start'
  release?: string
  peers?: number
  bytes?: number
}

/** Starting is search, file list, first data, file start; the wait says which step and for how long. */
function useStartingNote(animeId: number, episode: number, waiting: boolean): { title: string; detail: string } {
  // Counted per wait: a new episode, or a wait starting again, begins at zero (set during render, no effect).
  const wait = waiting ? `${animeId}:${episode}` : ''
  const [count, setCount] = useState({ wait, seconds: 0 })
  if (count.wait !== wait) setCount({ wait, seconds: 0 })
  const seconds = count.seconds
  useEffect(() => {
    if (!waiting) return
    const id = window.setInterval(() => setCount((c) => ({ ...c, seconds: c.seconds + 1 })), 1000)
    return () => window.clearInterval(id)
  }, [waiting, animeId, episode])

  const stage = useQuery({
    enabled: waiting,
    queryKey: ['play-stage', animeId, episode],
    queryFn: () => api.get<PlayStage>(`/api/play/stage${query({ id: animeId, episode })}`),
    refetchInterval: 1000,
    gcTime: 0,
  }).data

  const clock = seconds >= 20 ? ` · ${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}` : ''
  switch (stage?.step) {
    case 'files':
      return { title: 'Getting the file list…', detail: `${stage.release ?? 'From the people sharing it'}${clock}` }
    case 'data': {
      const swarm = stage.peers
        ? `${stage.peers} ${stage.peers === 1 ? 'peer' : 'peers'}, ${bytes(stage.bytes ?? 0)} so far`
        : 'no peers connected yet'
      return { title: 'Waiting for the first data…', detail: `${stage.release ? `${stage.release} · ` : ''}${swarm}${clock}` }
    }
    case 'start':
      return {
        title: 'Reading the start of the file…',
        detail: `A large release on a slow connection can take a few minutes${clock}`,
      }
    default:
      return {
        title: 'Searching for a release…',
        detail: `${seconds < 6 ? 'Checking the release sites under each of the show’s titles' : 'Still looking; searches are rate limited'}${clock}`,
      }
  }
}

// Clear of the header at the top; on a phone a full-width bar, above the tabs at the bottom.
const DOCK: Record<Corner, string> = {
  tl: 'top-20 left-4 max-sm:top-[4.5rem]',
  tr: 'top-20 right-4 max-sm:top-[4.5rem]',
  bl: 'bottom-4 left-4 max-sm:bottom-[calc(4.5rem+env(safe-area-inset-bottom))]',
  br: 'right-4 bottom-4 max-sm:bottom-[calc(4.5rem+env(safe-area-inset-bottom))]',
}

function MiniBar({ title, episode, onExpand, onClose }: { title: string; episode: string; onExpand: () => void; onClose: () => void }) {
  const playing = usePlayingNow()
  const button =
    'tap relative grid size-9 shrink-0 place-items-center rounded-full text-base-200 transition-colors hover:bg-base-800 hover:text-white'
  return (
    <div className="flex min-w-0 flex-1 items-center gap-1 px-3 py-2">
      <button type="button" onClick={onExpand} className="min-w-0 flex-1 text-left">
        <p className="truncate text-sm font-medium text-white">{title}</p>
        <p className="truncate text-xs text-base-400">{episode}</p>
      </button>
      <button type="button" onClick={togglePlay} aria-label={playing ? 'Pause' : 'Play'} title={playing ? 'Pause' : 'Play'} className={button}>
        <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
          {playing ? <path d="M7 5h3.5v14H7zM13.5 5H17v14h-3.5z" fill="currentColor" /> : <path d="M8 5.5v13l11-6.5-11-6.5Z" fill="currentColor" />}
        </svg>
      </button>
      <button type="button" onClick={onExpand} aria-label="Expand the player" title="Expand" className={cx(button, 'max-sm:hidden')}>
        <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
          <path d="M14 4h6v6M10 20H4v-6M20 4l-7 7M4 20l7-7" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </button>
      <button type="button" onClick={onClose} aria-label="Close the player" title="Close" className={button}>
        <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
          <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
        </svg>
      </button>
    </div>
  )
}

function Searching({ note }: { note: { title: string; detail: string } }) {
  return (
    <div className="grid aspect-video place-items-center">
      <div className="max-w-md px-4 text-center">
        <Spinner className="mx-auto size-8" />
        <p className="mt-3 text-sm text-base-300">{note.title}</p>
        <p className="mt-1 text-xs break-words text-base-500 tabular-nums">{note.detail}</p>
      </div>
    </div>
  )
}

// Not out yet, or raw only: an alert when it is, or the episode downloaded then.
function WhenOut({ animeId, episode }: { animeId: number; episode: number }) {
  const qc = useQueryClient()
  const [done, setDone] = useState<'notify' | 'want' | null>(null)
  const ask = useMutation({
    meta: { inline: true },
    mutationFn: (what: 'notify' | 'want') =>
      what === 'notify'
        ? api.post(`/api/anime/${animeId}/notify`, { on: true })
        : api.post(`/api/anime/${animeId}/want`, { episode }),
    onSuccess: (_, what) => {
      setDone(what)
      void qc.invalidateQueries({ queryKey: ['anime', animeId] })
    },
  })
  if (done) {
    return (
      <p role="status" className="mt-4 text-sm text-base-200">
        {done === 'notify'
          ? 'You get an alert when it is out.'
          : `Episode ${episode} downloads when it is out, and you get an alert.`}
      </p>
    )
  }
  return (
    <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
      <Button variant="primary" onClick={() => ask.mutate('notify')} disabled={ask.isPending}>
        Tell me when it's out
      </Button>
      <Button onClick={() => ask.mutate('want')} disabled={ask.isPending}>
        Download it when it's out
      </Button>
      {ask.isError && <p className="basis-full text-xs text-recap">{(ask.error as Error).message}</p>}
    </div>
  )
}

// Usually "nobody has subbed this yet", said plainly with the ways out.
function NoRelease({
  message,
  animeId,
  episode,
  notOut,
  rawAvailable,
  rawTitle,
  inPlayer,
  needsTranscoder,
  onRetry,
  onPlayRaw,
  onPick,
}: {
  message: string
  animeId: number
  episode?: number
  /** Not aired yet, or out raw only: worth waiting for. */
  notOut?: boolean
  rawAvailable?: boolean
  rawTitle?: string
  // Covering a player rather than standing in for one.
  inPlayer?: boolean
  // Retrying can't help; offer Setup instead.
  needsTranscoder?: boolean
  onRetry: () => void
  onPlayRaw?: () => void
  onPick: () => void
}) {
  return (
    <div
      className={cx(
        'grid place-items-center p-6 text-center',
        inPlayer ? 'absolute inset-0 z-30 bg-black/90' : 'aspect-video',
      )}
    >
      <div className="max-w-lg">
        {/* Server reasons are Go errors, lower-case: here one starts a sentence. */}
        <p className="text-sm text-base-200">{message.charAt(0).toUpperCase() + message.slice(1)}</p>

        {rawAvailable && (
          <div className="mt-4 rounded-lg border border-base-800 bg-base-900/60 p-3 text-left">
            <p className="text-xs text-base-400">
              A raw broadcast is seeding now. It has no subtitles.
            </p>
            {rawTitle && (
              <p className="mt-1 line-clamp-2 font-mono text-[11px] text-base-500">{rawTitle}</p>
            )}
            <button
              onClick={onPlayRaw}
              className={cx(buttonClass(), 'mt-2')}
            >
              Watch the raw anyway
            </button>
          </div>
        )}

        {notOut && episode ? <WhenOut animeId={animeId} episode={episode} /> : null}

        {needsTranscoder ? (
          <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
            <LinkButton to="/setup" variant="primary">Install the transcoder</LinkButton>
            <LinkButton to="/settings?tab=Playback" variant="ghost">Player settings</LinkButton>
          </div>
        ) : (
        <div className="mt-4 flex flex-wrap items-center justify-center gap-2">
          <Button onClick={onRetry}>Try again</Button>
          <Button onClick={onPick}>Choose a release</Button>
          <LinkButton to={`/anime/${animeId}`}>Other episodes</LinkButton>
          <LinkButton to="/settings?tab=Downloads" variant="ghost">Quality settings</LinkButton>
        </div>
        )}
      </div>
    </div>
  )
}

/** mpv plays in its own window, so this page only offers Stop and says which player is in charge. */
function MpvPanel({ title, animeId, episode, player }: { title: string; animeId: number; episode: number; player: string }) {
  // The episode too, so the server also stops fetching what nobody is watching now.
  const stop = useMutation({ mutationFn: () => api.post('/api/stop', { animeId, episode }) })

  return (
    <div className="grid aspect-video place-items-center bg-base-900 p-6 text-center">
      <div>
        <p className="text-sm font-medium text-base-100">Playing in {player === 'vlc' ? 'VLC' : 'mpv'}</p>
        <p className="mt-1 max-w-md truncate text-xs text-base-500">
          Episode {episode} · {title}
        </p>
        <div className="mt-4 flex items-center justify-center gap-2">
          <button
            onClick={() => stop.mutate()}
            disabled={stop.isPending}
            className={buttonClass()}
          >
            {stop.isSuccess ? 'Stopped' : 'Stop'}
          </button>
          <Link
            to="/settings?tab=Playback"
            className="rounded-md px-3 py-1.5 text-sm text-base-400 hover:text-base-100"
          >
            Use the browser instead
          </Link>
        </div>
      </div>
    </div>
  )
}

const UP_NEXT_SECONDS = 5

// Over the credits at the end: counts down with auto-next on, else offers the button.
function UpNext({
  label,
  line,
  then,
  auto,
  stopped,
  onPlay,
  onCancel,
}: {
  label: string
  line: string
  /** What follows it on the Up next list. */
  then?: string
  auto: boolean
  /** The sleep timer held back the countdown: said, so a missing one isn't a mystery. */
  stopped?: boolean
  /** By the countdown running out, or a press on the card. */
  onPlay: (by: 'countdown' | 'card') => void
  onCancel: () => void
}) {
  const [left, setLeft] = useState(UP_NEXT_SECONDS)
  useEffect(() => {
    if (!auto) return
    if (left <= 0) {
      onPlay('countdown')
      return
    }
    const id = window.setTimeout(() => setLeft((n) => n - 1), 1000)
    return () => window.clearTimeout(id)
  }, [auto, left, onPlay])

  return (
    <div
      role="dialog"
      aria-label="Up next"
      className="absolute right-4 bottom-20 z-20 w-72 animate-rise rounded-xl border border-base-700 bg-base-950/90 p-3 shadow-xl shadow-black/60 backdrop-blur-sm"
    >
      <p className="text-[11px] font-medium tracking-wide text-base-400 uppercase">{label}</p>
      <p className="mt-0.5 truncate text-sm text-base-100">{line}</p>
      {then && <p className="mt-0.5 truncate text-xs text-base-500">{then}</p>}
      {stopped && <p className="mt-1 text-xs text-base-400">The sleep timer stopped auto next.</p>}
      <div className="mt-2.5 flex items-center gap-2">
        <button
          onClick={() => onPlay('card')}
          className="rounded-md bg-white px-3 py-1.5 text-sm font-semibold text-base-950 hover:bg-base-100 pointer-coarse:min-h-11"
        >
          {auto ? `Play now · ${left}` : 'Play next'}
        </button>
        <button
          onClick={onCancel}
          className="rounded-md px-3 py-1.5 text-sm text-base-300 hover:bg-base-800 hover:text-white pointer-coarse:min-h-11"
        >
          {auto ? 'Cancel' : 'Dismiss'}
        </button>
      </div>
      {auto && (
        <div className="mt-2 h-0.5 overflow-hidden rounded-full bg-base-800">
          <div
            className="h-full bg-accent-500 transition-[width] duration-1000 ease-linear"
            style={{ width: `${((UP_NEXT_SECONDS - left) / UP_NEXT_SECONDS) * 100}%` }}
          />
        </div>
      )}
    </div>
  )
}

// Nothing aired to play on to: when the next episode airs; or at a finale, the coming season (Plan to watch, an
// alert), a score for this one, and with no season to come, a few shows like it.
function EndOfAired({
  animeId,
  upcoming,
  sequel,
  score,
  alerting,
  onClose,
}: {
  animeId: number
  upcoming?: Episode
  sequel?: Season
  score: number
  /** Alerts are on for this show already: said, not offered. */
  alerting: boolean
  onClose: () => void
}) {
  const qc = useQueryClient()
  const alertFor = upcoming ? animeId : sequel?.id
  const alert = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post(`/api/anime/${alertFor}/notify`, { on: true }),
  })
  const plan = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post('/api/status', { animeId: sequel!.id, status: 'PLANNING' }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['franchise', animeId] }),
  })
  const rate = useMutation({
    meta: { inline: true },
    mutationFn: (tens: number) => api.post('/api/score', { animeId, score: tens * 10 }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['anime', animeId] })
      void qc.invalidateQueries({ queryKey: ['library'] })
    },
  })
  const theEnd = !upcoming && !sequel
  const recs = useRecommendations(theEnd ? animeId : undefined)
  const scored = rate.variables ?? Math.round(score / 10)
  const button = 'rounded-md px-3 py-1.5 text-sm pointer-coarse:min-h-11'
  const failed = [alert, plan, rate].find((m) => m.isError)?.error as Error | undefined

  return (
    <div
      role="dialog"
      aria-label={upcoming ? 'Next episode' : 'Season finale'}
      className="absolute right-4 bottom-20 z-20 w-[22rem] max-w-[calc(100%-2rem)] animate-rise rounded-xl border border-base-700 bg-base-950/90 p-3 shadow-xl shadow-black/60 backdrop-blur-sm"
    >
      <p className="text-[11px] font-medium tracking-wide text-base-400 uppercase">
        {upcoming ? 'Up next' : sequel ? 'Season finale' : 'The end'}
      </p>
      <p className="mt-0.5 text-sm text-base-100">
        {upcoming
          ? `Episode ${upcoming.display || upcoming.number} airs ${upcoming.airDate ? relativeTime(upcoming.airDate) : 'later'}`
          : sequel
            ? `${sequel.english || sequel.romaji} · ${sequel.year ? `coming ${sequel.year}` : 'announced'}`
            : "You've seen every episode."}
      </p>

      <div className="mt-2.5 flex flex-wrap items-center gap-2">
        {sequel && !upcoming && !sequel.onList && (
          <button
            onClick={() => plan.mutate()}
            disabled={plan.isPending || plan.isSuccess}
            className={cx(button, 'bg-white font-semibold text-base-950 hover:bg-base-100 disabled:opacity-60')}
          >
            {plan.isSuccess ? 'In Planning' : 'Add to Planning'}
          </button>
        )}
        {alertFor && upcoming && alerting ? (
          <span className="text-xs text-base-400">Alerts are on for this show.</span>
        ) : (
          alertFor && (
            <button
              onClick={() => alert.mutate()}
              disabled={alert.isPending || alert.isSuccess}
              className={cx(button, 'bg-base-800 text-base-100 hover:bg-base-700 disabled:opacity-60')}
            >
              {alert.isSuccess ? "You'll get an alert" : "Alert me when it's out"}
            </button>
          )
        )}
        <button onClick={onClose} className={cx(button, 'text-base-300 hover:bg-base-800 hover:text-white')}>
          Dismiss
        </button>
      </div>

      {!upcoming && (
        <div className="mt-3 border-t border-base-800 pt-2">
          <p className="text-[11px] text-base-400">Your score</p>
          <div className="mt-1 flex flex-wrap gap-1" role="group" aria-label="Your score">
            {Array.from({ length: 10 }, (_, i) => i + 1).map((n) => (
              <button
                key={n}
                onClick={() => rate.mutate(n)}
                aria-pressed={scored === n}
                className={cx(
                  'grid size-7 place-items-center rounded-md text-xs tabular-nums transition-colors pointer-coarse:size-11',
                  scored === n ? 'bg-accent-500 text-white' : 'bg-base-850 text-base-300 hover:bg-base-750',
                )}
              >
                {n}
              </button>
            ))}
          </div>
        </div>
      )}

      {theEnd && (recs.data?.items.length ?? 0) > 0 && (
        <div className="mt-3 border-t border-base-800 pt-2">
          <p className="text-[11px] text-base-400">More like this</p>
          <div className="mt-1.5 grid grid-cols-3 gap-2">
            {recs.data!.items.slice(0, 3).map((r) => (
              <Link key={r.id} to={`/anime/${r.id}`} className="group min-w-0">
                <img
                  src={r.cover ?? r.thumb}
                  alt=""
                  loading="lazy"
                  className="aspect-[2/3] w-full rounded-md object-cover ring-1 ring-white/10 transition-transform group-hover:scale-[1.03]"
                />
                <p className="mt-1 line-clamp-1 text-[11px] text-base-300 group-hover:text-white">{r.title}</p>
              </Link>
            ))}
          </div>
        </div>
      )}
      {failed && <p className="mt-2 text-xs text-recap">{failed.message}</p>}
    </div>
  )
}

// After a run of episodes auto next started with no one touching anything: asks before going on, and can take
// back the ticks those episodes earned, in case no one saw them.
function StillWatching({ run, onKeep, onStop }: { run: Unattended[]; onKeep: () => void; onStop: () => void }) {
  const qc = useQueryClient()
  // Unwatching an episode rewinds everything after it too: one call per show, from its first.
  const firsts = [...new Map(run.map((e) => [e.animeId, e])).keys()].map((animeId) => ({
    animeId,
    episode: Math.min(...run.filter((e) => e.animeId === animeId).map((e) => e.episode)),
  }))
  const unmark = useMutation({
    meta: { inline: true },
    mutationFn: () =>
      Promise.all(firsts.map((e) => api.post('/api/watched', { animeId: e.animeId, episode: e.episode, watched: false }))),
    onSuccess: () => {
      for (const e of firsts) {
        void qc.invalidateQueries({ queryKey: ['episodes', e.animeId] })
        void qc.invalidateQueries({ queryKey: ['anime', e.animeId] })
      }
      void qc.invalidateQueries({ queryKey: ['upnext'] })
    },
  })
  const one = firsts.length === 1
  const span = one ? `${firsts[0].episode}${run.length > 1 ? `–${Math.max(...run.map((e) => e.episode))}` : ''}` : ''
  const unmarkLabel = one ? `Unmark episode${run.length > 1 ? 's' : ''} ${span}` : `Unmark these ${run.length}`
  const button = 'rounded-md px-3 py-1.5 text-sm pointer-coarse:min-h-11'

  return (
    <div
      role="dialog"
      aria-label="Still watching?"
      className="absolute right-4 bottom-20 z-20 w-80 max-w-[calc(100%-2rem)] animate-rise rounded-xl border border-base-700 bg-base-950/90 p-3 shadow-xl shadow-black/60 backdrop-blur-sm"
    >
      <p className="text-sm font-semibold text-base-100">Still watching?</p>
      <p className="mt-0.5 text-xs text-base-400">
        Paused after {run.length} episode{run.length === 1 ? '' : 's'} in a row with no one touching anything.
      </p>
      <div className="mt-2.5 flex flex-wrap items-center gap-2">
        <button onClick={onKeep} className={cx(button, 'bg-white font-semibold text-base-950 hover:bg-base-100')}>
          Keep watching
        </button>
        <button onClick={onStop} className={cx(button, 'text-base-300 hover:bg-base-800 hover:text-white')}>
          Stop here
        </button>
      </div>
      <div className="mt-2 border-t border-base-800 pt-2">
        {unmark.isSuccess ? (
          <p className="text-xs text-base-400">Unmarked, and your tracker is told.</p>
        ) : (
          <button
            onClick={() => unmark.mutate()}
            disabled={unmark.isPending}
            className={cx(button, '-mx-3 text-xs text-base-400 hover:bg-base-800 hover:text-white disabled:opacity-50')}
          >
            {unmark.isPending ? 'Unmarking…' : `Didn't see them? ${unmarkLabel}`}
          </button>
        )}
        {unmark.isError && <p className="text-xs text-recap">{(unmark.error as Error).message}</p>}
      </div>
    </div>
  )
}

// A scope's label and its controls, kept together when the row wraps.
function OptionGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div role="group" aria-label={label} className="flex flex-wrap items-center gap-2">
      <span className="text-[10px] font-semibold tracking-wider text-base-500 uppercase">{label}</span>
      {children}
    </div>
  )
}

function Toggle({
  label,
  on,
  onChange,
}: {
  label: string
  on: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      onClick={() => onChange(!on)}
      className={cx(
        'flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium transition-colors pointer-coarse:min-h-11 pointer-coarse:px-3',
        on ? 'bg-accent-500/20 text-accent-400' : 'bg-base-850 text-base-400 hover:text-base-200',
      )}
    >
      <span
        className={cx(
          'relative h-3.5 w-6 rounded-full transition-colors',
          on ? 'bg-accent-500' : 'bg-base-700',
        )}
      >
        <span
          className={cx(
            'absolute top-0.5 size-2.5 rounded-full bg-white transition-all',
            on ? 'left-3' : 'left-0.5',
          )}
        />
      </span>
      {label}
    </button>
  )
}
