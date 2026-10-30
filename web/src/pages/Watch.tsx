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
import { airTime, bytes, cx, languageName, relativeTime } from '../lib/format'
import { setFocus } from '../lib/focus'
import {
  refreshDownloads,
  useAccess,
  useDownloads,
  useEpisodes,
  useFranchise,
  useNow,
  usePrefs,
  useRecommendations,
  useSetNotify,
  useAlertsOff,
  useSetPref,
  useTrackers,
  type Season,
} from '../lib/queries'
import { joinRoom, overlayOn, parseTogether, saveSeat, savedSeat, useRoom, useTogetherSync } from '../lib/together'
import { JoinCard, TogetherPanel, TogetherToasts } from '../components/Together'
import { SaveToDevice } from '../components/SaveToDevice'
import { toast } from '../lib/toast'
import { saveNextAfter } from '../lib/offline'
import { Anime4KDialog } from '../components/Anime4KDialog'
import { CharacterRail } from '../components/CharacterRail'
import { EpisodeList } from '../components/EpisodeList'
import { isUnaired } from '../lib/episodes'
import { ReleasePicker } from '../components/ReleasePicker'
import { OpenInApp } from '../components/OpenInApp'
import { historyIndex } from '../components/places'
import { StatusMenu } from '../components/StatusMenu'
import { Button, ErrorState, LinkButton } from '../components/ui'
import { PopSheet } from '../components/PopSheet'
import { Sheet } from '../components/Sheet'
import { useDocumentTitle, useMediaQuery } from '../lib/hooks'
import { buttonClass } from '../lib/ui'
import { getSleep, noteAutoStart, noteInput, setSleep, unattendedRun, useSleep, type Unattended } from '../lib/watching'

type AudioChoice = 'sub' | 'dub' | 'either'
import { Player, type PlayerControls, type PlayerFacts } from '../player/Player'
import { StartingSteps, type Starting } from '../player/Starting'
import { ActionRow, ChoiceRow, Choices, PanelGroup, SegmentRow, SwitchRow } from '../player/panel'
import { SKIP_MODES, skipMode } from '../player/skipMode'
import { skipKind } from '../player/hooks'
import { keyHint } from '../lib/keymap'
import { deviceCaps } from '../player/decode'
import { cellular, LOWER, SAVER, setOnMobile, setSitting, stepBytes, streamChoice, useNetwork } from '../player/network'
import { MobileDataCard, NetworkNote } from '../components/MobileData'
import { asSubLook, type SubLook } from '../player/subLook'
import {
  currentVideo,
  minimize,
  togglePlay,
  unminimize,
  useCurrentVideo,
  useDockDrag,
  usePlayingNow,
  type Corner,
} from '../lib/miniplayer'
import {
  autoTier,
  deviceUpscale,
  lowerTier,
  setAutoTier,
  setDeviceUpscale,
  upscaleHere,
  type AutoTier,
} from '../player/anime4k'

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
  const navigate = useCallback(
    (path: string) => (mini && onGo && path.startsWith('/watch/') ? onGo(path) : routerNavigate(path)),
    [mini, onGo, routerNavigate],
  )
  const dock = useDockDrag(mini)
  const wide = useMediaQuery('(min-width: 40rem)')
  const side = useMediaQuery('(min-width: 80rem)')
  const playerControls = useRef<PlayerControls>(null)
  const [facts, setFacts] = useState<PlayerFacts>({})
  const [avSheet, setAvSheet] = useState(false)
  const enteredAt = useRef<number | null>(null)
  useEffect(() => {
    if (mini) {
      enteredAt.current = null
      return
    }
    unminimize()
    enteredAt.current ??= historyIndex()
  }, [mini])
  const onMinimize = useCallback(() => {
    minimize()
    const at = enteredAt.current ?? historyIndex()
    if (at > 0) routerNavigate(at - historyIndex() - 1)
    else routerNavigate(`/anime/${id}`)
  }, [routerNavigate, id])
  const [focused, setFocused] = useState(false)
  if (mini && focused) setFocused(false)
  const focus = focused && !mini
  const toggleFocus = useCallback(() => setFocused((v) => !v), [])
  useEffect(() => {
    setFocus(focus)
    return () => setFocus(false)
  }, [focus])
  useEffect(() => {
    if (!focus) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || e.defaultPrevented || document.fullscreenElement) return
      if (document.querySelector('[role="menu"], [role="dialog"], [role="listbox"]')) return
      setFocused(false)
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [focus])
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

  const overrides = prefs.data?.overrides ?? {}
  const setFlag = (key: string) => (v: boolean) =>
    setPref.mutate({
      key,
      value: String(v),
      animeId: key in overrides ? id : undefined,
    })

  const epKey = `${id}-${ep}`

  const [rawFor, setRawFor] = useState('')
  const allowRaw = rawFor === epKey
  const setAllowRaw = (on: boolean) => setRawFor(on ? epKey : '')

  const [audioPick, setAudioPick] = useState<{ key: number; audio: AudioChoice } | null>(null)
  const audio = audioPick?.key === id ? audioPick.audio : null
  const setAudio = (a: AudioChoice | null) => {
    setAudioPick(a ? { key: id, audio: a } : null)
    if (a) setPref.mutate({ key: 'audio.prefer', value: a, animeId: id })
  }
  const wantAudio: AudioChoice =
    audio ?? ((effective['audio.prefer'] as AudioChoice | undefined) ?? 'sub')

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
    queryFn: ({ signal }) =>
      api.post<PlaySession>('/api/play', {
        animeId: id, episode: ep, external, allowRaw, audio: audio ?? undefined, infoHash,
      }, signal),
    retry: false,
    staleTime: 'static',
    gcTime: 0,
  })

  const playingInMpv = !!play.data && play.data.player !== 'browser'

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
  const watchHere = `/watch/${id}/${ep}${together ? `?together=${encodeURIComponent(together.href)}` : ''}`
  const leaveTogether = () => {
    void room.act('leave').catch(() => {})
    if (together) saveSeat(together, null)
    setTogether(null)
    setSeat(null)
    setInvite(undefined)
  }

  const source = playingInMpv ? undefined : play.data?.streamUrl
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
  const [convertAll, setConvertAll] = useState<string | null>(null)
  const strict = !!source && convertAll === source
  const net = useNetwork()
  const access = useAccess()
  const away = access.data ? !!access.data.away : access.isError
  const [stepped, setStepped] = useState<{ source: string; step: string } | null>(null)
  const choice = streamChoice(net, away, stepped && stepped.source === source ? stepped.step : undefined)
  const lastPosition = useRef<{ source?: string; at: number }>({ at: 0 })
  const [resumeAt, setResumeAt] = useState<{ source?: string; at: number } | null>(null)
  const reopenHere = () => {
    if (lastPosition.current.source === source) setResumeAt({ source, at: lastPosition.current.at })
  }
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
    gcTime: 0,
  })

  const startingNote = useStartingNote(
    id,
    ep,
    episodes.data?.items.find((e) => e.number === ep)?.display ?? ep,
    play.isPending || (!!source && stream.isPending),
    !!play.data,
  )

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
  const resetDownload = download.reset
  useEffect(() => resetDownload(), [id, ep, resetDownload])

  const downloads = useDownloads()
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
  const skipFiller = flag('playback.skip_filler')
  const now = useNow()
  const next = list.find(
    (e) =>
      e.number > ep &&
      !isUnaired(e, now) &&
      !(skipFiller && (e.filler === 'filler' || e.recap)),
  )
  const after = useQuery({
    enabled: id !== 0 && ep > 0,
    queryKey: ['upnext-after', id, ep],
    queryFn: () =>
      api.get<{ fromList: boolean; next?: UpNextItem; then?: UpNextItem }>(`/api/upnext/after${query({ id, episode: ep })}`),
    refetchInterval: 2 * 60_000,
  })
  const fromList = after.data?.fromList ? after.data.next : undefined
  const franchise = useFranchise(id)
  const upcoming = next ? undefined : list.find((e) => e.number > ep && isUnaired(e, now))
  const total = detail.data?.episodes ?? 0
  const finale = !fromList && !next && !upcoming && total > 0 && ep >= total
  const seasons = franchise.data?.seasons ?? []
  const thisSeason = seasons.find((s) => s.id === id)
  const sequel =
    thisSeason && seasons.filter((s) => s.ordinal > thisSeason.ordinal).sort((a, b) => a.ordinal - b.ordinal)[0]
  const sequelOut = !!sequel && (sequel.status === 'RELEASING' || sequel.status === 'FINISHED')
  const sequelPath = finale && sequelOut ? `/watch/${sequel.id}/1` : undefined
  const hasNext = !!fromList || !!next || !!sequelPath

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
      if (!final) return beacon('/api/progress', body)
      void api
        .post<{ watched: boolean }>('/api/progress', body)
        .then((r) => {
          if (!r.watched) return
          void qc.invalidateQueries({ queryKey: ['episodes', id] })
          void qc.invalidateQueries({ queryKey: ['anime', id] })
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
  const note = useCallback(
    (event: string, detail: Record<string, unknown>) =>
      beacon('/api/playback/note', { animeId: id, episode: ep, event, detail }),
    [id, ep],
  )
  const endHides = (animeId: number, number: number, spoiler?: boolean) =>
    !!spoiler && !(effective['display.spoilers'] !== 'unwatched' && animeId === id && number === next?.number)
  const nextAnime = fromList ? fromList.animeId : !next && sequelPath ? sequel!.id : id
  const nextEpisode = fromList ? fromList.episode : next ? next.number : sequelPath ? 1 : undefined
  const advance = useCallback(
    (by: string) => {
      if (!nextPath) return
      if (by === 'countdown' && nextEpisode) noteAutoStart({ animeId: nextAnime, episode: nextEpisode })
      else noteInput()
      if (by !== 'countdown' && getSleep().kind === 'fired') setSleep({ kind: 'off' })
      note('next', { by, to: nextPath })
      navigate(nextPath)
    },
    [nextPath, navigate, note, nextAnime, nextEpisode],
  )
  const goNext = useCallback((by: 'button' | 'key' | 'media' = 'button') => advance(by), [advance])
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
  const setSubLook = useCallback((look: SubLook) => setPrefMutate({ key: 'subtitle.look', value: look }), [setPrefMutate])
  const setSubRaised = useCallback(
    (raised: boolean) => setPrefMutate({ key: 'subtitle.raise', value: String(raised) }),
    [setPrefMutate],
  )
  const previous = [...list].reverse().find((e) => e.number < ep && !isUnaired(e, now))
  const previousPath = previous ? `/watch/${id}/${previous.number}` : undefined
  const goPrevious = useCallback(() => {
    if (!previousPath) return
    note('previous', { to: previousPath })
    navigate(previousPath)
  }, [previousPath, navigate, note])

  const [streamed, setStreamed] = useState(false)
  useEffect(() => {
    if (stream.data) setStreamed(true)
  }, [stream.data])

  const [endedKey, setEndedKey] = useState<string | null>(null)
  const ended = endedKey === `${id}-${ep}`
  const endedAt = useRef(0)
  const endedNow = useRef(ended)
  endedNow.current = ended
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
  const autoNext = flag('playback.autonext') && (!inRoom || isHost)
  const sleep = useSleep()
  const stillLimit = Number(effective['playback.still_watching'] ?? '3')
  const [asked, setAsked] = useState<{ key: string; run: Unattended[] } | null>(null)
  const askedRun = asked?.key === `${id}-${ep}` ? asked.run : null
  const counting = autoNext && sleep.kind !== 'fired' && sleep.kind !== 'episode' && !askedRun
  const onEnded = useCallback(() => {
    endedAt.current = Date.now()
    setEndedKey(`${id}-${ep}`)
    saveNextAfter(id, ep)
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

  const refetchPlay = play.refetch
  const refetchStream = stream.refetch
  const recoverSession = useCallback(async () => {
    const revived = await refetchPlay({ throwOnError: true })
    if (revived.data?.streamUrl !== source) return
    await refetchStream({ throwOnError: true })
  }, [refetchPlay, refetchStream, source])

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

  useEffect(() => {
    const streamId = stream.data?.id
    return () => {
      if (streamId) void api.del(`/api/stream/${streamId}`).catch(() => {})
    }
  }, [stream.data?.id])

  const bookend =
    effective['playback.skip_keep_bookends'] !== 'false' &&
    (ep === 1 || (detail.data?.status === 'FINISHED' && !!detail.data.episodes && ep === detail.data.episodes))
  const opMode = skipMode(effective['playback.autoskip_op'])
  const edMode = skipMode(effective['playback.autoskip_ed'])
  const skipOp = opMode === 'auto' && !bookend
  const skipEd = edMode === 'auto' && !bookend
  const skipOff = useMemo(() => ({ op: opMode === 'off', ed: edMode === 'off' }), [opMode, edMode])
  const skipRecap = flag('playback.autoskip_recap')
  const skipPreview = flag('playback.autoskip_preview')
  const guest = inRoom && !isHost
  const autoSkip = useMemo(
    () => ({ op: skipOp && !guest, ed: skipEd && !guest, recap: skipRecap && !guest, preview: skipPreview && !guest }),
    [skipOp, skipEd, skipRecap, skipPreview, guest],
  )

  const [upscale, setUpscale] = useState<{ enabled: boolean; mode: string } | null>(null)
  const [tuning, setTuning] = useState(false)
  useEffect(() => setUpscale(null), [id, ep])

  const [deviceChoice, setDeviceChoice] = useState(deviceUpscale)
  const [settled, setSettled] = useState(autoTier)
  const [steppedTo, setSteppedTo] = useState<AutoTier | null>(null)
  const here = upscaleHere(deviceChoice, flag('playback.anime4k'), settled)
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
    onSlow: here.auto && !upscale ? stepDown : undefined,
  }

  if (!id || !ep) return <ErrorState error={new Error('Unknown episode')} />

  const failure =
    play.error instanceof ApiError
      ? (play.error.body as { rawAvailable?: boolean; rawTitle?: string; notOut?: boolean } | undefined)
      : undefined

  const playerShown = !(play.isError && !streamed) && !(play.isPending && !streamed) && !playingInMpv
  const cover = detail.data?.coverImage?.extraLarge ?? detail.data?.coverImage?.large ?? detail.data?.cover
  const cancelStart = () => routerNavigate(`/anime/${id}`)
  const minutes = episodeLength ? Math.round(episodeLength / 60) : current?.runtime
  const height = opened?.plan?.maxRate ? opened.plan.height : opened?.video?.height
  const factLine = [
    minutes ? `${minutes} min` : '',
    facts.audio ? `${facts.audio} audio` : '',
    facts.subtitles === 'off'
      ? 'Subtitles off'
      : facts.subtitles === 'none'
        ? 'No subtitles'
        : facts.subtitles
          ? `${facts.subtitles} subtitles`
          : '',
    height ? `${height}p` : '',
    play.data?.source === 'local' ? 'Local file' : '',
  ]
    .filter(Boolean)
    .join(' · ')

  const sequelTitle = sequel ? sequel.english || sequel.romaji : ''
  const listed = fromList?.animeId === id ? list.find((e) => e.number === fromList.episode) : undefined
  const nextText = fromList
    ? fromList.animeId === id
      ? `Next: Episode ${listed?.display || fromList.episode}`
      : `Up next: ${fromList.title} · Ep ${fromList.episode}`
    : next
      ? `Next: Episode ${next.display || next.number}`
      : sequelPath
        ? `Next: ${sequelTitle} · Ep 1`
        : undefined
  const previousName = previous ? previous.display || previous.number : 0
  const nextButton = nextText ? (
    <button
      type="button"
      onClick={() => goNext('button')}
      title={`${nextText} (${keyHint('next')})`}
      className={cx(buttonClass('primary'), 'min-w-0 max-sm:min-h-12')}
    >
      {fromList && fromList.animeId !== id && fromList.cover && (
        <img src={fromList.cover} alt="" className="size-6 shrink-0 rounded object-cover" />
      )}
      <span className="truncate">{nextText}</span>
      <span aria-hidden>›</span>
    </button>
  ) : upcoming ? (
    <NotOut label={`Episode ${upcoming.display || upcoming.number} · ${outDay(upcoming.airDate, now)}`}>
      <p className="text-sm text-base-100">
        Episode {upcoming.display || upcoming.number}{' '}
        {upcoming.airDate ? `comes out ${outFull(upcoming.airDate)}, ${relativeTime(upcoming.airDate)}.` : "isn't out yet."}
      </p>
      <p className="mt-1 text-sm text-base-400">Yoru can tell you when it's out, or download it then.</p>
      <WhenOut animeId={id} episode={upcoming.number} alerting={!!detail.data?.notify?.on} start />
    </NotOut>
  ) : finale && sequel ? (
    <NotOut label={`Next season · ${sequel.year ? `coming ${sequel.year}` : 'announced'}`}>
      <SequelLater sequel={sequel} />
    </NotOut>
  ) : null

  const scope = (key: string) => (key in overrides ? 'This show' : 'All devices')
  const partMissing = (kind: 'op' | 'ed') =>
    !!skips.data && !skips.data.finding && !skips.data.ranges.some((r) => skipKind(r.kind) === (kind === 'op' ? 'opening' : 'ending'))
  const skipCaption = (kind: 'op' | 'ed') => {
    const mode = kind === 'op' ? opMode : edMode
    if (mode === 'auto' && bookend) return kind === 'op' ? 'This show · this episode keeps its opening' : 'This show · this episode keeps its ending'
    if (partMissing(kind)) return `This show · no ${kind === 'op' ? 'opening' : 'ending'} times for this episode yet`
    return 'This show'
  }
  const subtitleLanguage = languageName(subLanguages[0]) ?? 'English'
  const audioChoice = (
    <Choices label="Audio">
      {(
        [
          { value: 'sub', label: `Japanese audio, ${subtitleLanguage} subtitles` },
          { value: 'dub', label: 'English dub' },
          { value: 'either', label: 'Whichever is ready first' },
        ] as const
      ).map((c) => (
        <ChoiceRow
          key={c.value}
          label={c.label}
          caption={c.value === wantAudio ? 'This show' : undefined}
          selected={c.value === wantAudio}
          onClick={() => setAudio(c.value)}
        />
      ))}
    </Choices>
  )
  const playbackRows = (
    <>
      <SegmentRow
        label="Skip openings"
        caption={skipCaption('op')}
        value={opMode}
        options={SKIP_MODES}
        onChange={(v) => setPref.mutate({ key: 'playback.autoskip_op', value: v, animeId: id })}
      />
      <SegmentRow
        label="Skip endings"
        caption={skipCaption('ed')}
        value={edMode}
        options={SKIP_MODES}
        onChange={(v) => setPref.mutate({ key: 'playback.autoskip_ed', value: v, animeId: id })}
      />
      <SwitchRow
        label="Skip filler episodes"
        caption="This show"
        on={skipFiller}
        onChange={(v) => setPref.mutate({ key: 'playback.skip_filler', value: String(v), animeId: id })}
      />
      <SwitchRow
        label="Play the next episode by itself"
        caption={scope('playback.autonext')}
        on={flag('playback.autonext')}
        onChange={setFlag('playback.autonext')}
      />
      <SwitchRow
        label="Start playing when opened"
        caption={scope('playback.autoplay')}
        on={flag('playback.autoplay')}
        onChange={setFlag('playback.autoplay')}
      />
      {!playingInMpv && (
        <ActionRow
          label="Sharper picture (Anime4K)"
          caption={wide ? 'This device' : 'This phone'}
          value={upscaling.enabled ? `On · ${upscaling.mode}` : 'Off'}
          onClick={() => {
            playerControls.current?.settings(false)
            setAvSheet(false)
            setTuning(true)
          }}
        />
      )}
    </>
  )
  const openAudioSubtitles = () => {
    if (playerShown && playerControls.current) playerControls.current.settings(true)
    else setAvSheet(true)
  }
  const anime4kDialog = (
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
        if (choice === 'auto') {
          setAutoTier(null)
          setSettled(null)
        }
        setSteppedTo(null)
        setUpscale(null)
      }}
    />
  )
  const episodesSection = (
    <section aria-labelledby="watch-episodes">
      <h2 id="watch-episodes" className="mb-2 section-title">
        Episodes
      </h2>
      {list.length > 0 ? (
        <EpisodeList animeId={id} episodes={list} current={ep} compact />
      ) : (
        <p className="rounded-lg border border-base-850 p-4 text-sm text-base-500">No episode list available.</p>
      )}
    </section>
  )

  return (
    <div
      ref={dock.ref}
      onPointerDownCapture={dock.onPointerDownCapture}
      onClickCapture={dock.onClickCapture}
      className={cx(
        focus ? 'fixed inset-0 z-40 overflow-y-auto bg-black' : 'bg-base-950',
        mini &&
          'fixed z-40 w-[22rem] cursor-grab touch-none rounded-xl shadow-2xl ring-1 shadow-black/60 ring-white/10 select-none active:cursor-grabbing max-sm:inset-x-2 max-sm:w-auto',
        mini && DOCK[dock.corner],
      )}
    >
      <div
        className={cx(
          !mini && !focus && 'mx-auto w-full max-w-[1600px] px-0 sm:px-6 sm:py-4 min-[1800px]:max-w-[2200px]',
          focus && 'flex min-h-full flex-col justify-center',
        )}
      >
        <div className={cx(!mini && !focus && 'grid grid-cols-[minmax(0,1fr)] gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]')}>
          <div className={cx('min-w-0', mini && 'max-sm:flex max-sm:items-center')}>
            <div
              className={cx(
                'relative bg-black',
                mini
                  ? 'rounded-t-xl max-sm:w-36 max-sm:shrink-0 max-sm:rounded-l-xl max-sm:rounded-tr-none'
                  : focus
                    ? 'mx-auto w-full max-w-[calc((100dvh-3.5rem)*16/9)]'
                    : 'sm:rounded-xl max-sm:sticky max-sm:top-0 max-sm:z-30 max-sm:pt-[env(safe-area-inset-top)]',
              )}
            >
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
                <div className="relative aspect-video overflow-hidden sm:rounded-xl">
                  <StartingSteps
                    starting={startingNote}
                    backdrop={cover}
                    onPick={() => setPicking(true)}
                    onCancel={cancelStart}
                    onRetry={() => void play.refetch()}
                  />
                </div>
              ) : playingInMpv ? (
                <MpvPanel title={play.data?.title ?? showTitle} animeId={id} episode={ep} player={play.data?.player ?? 'mpv'} />
              ) : (
                <Player
                  stream={stream.data}
                  starting={startingNote}
                  onStartCancel={cancelStart}
                  onStartRetry={() => void play.refetch()}
                  startAt={resumeAt && resumeAt.source === source ? resumeAt.at : (play.data?.startAt ?? 0)}
                  skips={skips.data?.ranges ?? []}
                  autoSkip={autoSkip}
                  skipOff={skipOff}
                  onMinimize={!mini && !focus ? onMinimize : undefined}
                  settings={{ audio: audioChoice, playback: playbackRows }}
                  controls={playerControls}
                  theatre={focus}
                  onTheatre={wide ? toggleFocus : undefined}
                  onFacts={setFacts}
                  nextLabel={nextText}
                  onSkipVote={skips.data?.share ? voteSkip : undefined}
                  skipMarks={skipMarks}
                  autoPlay={flag('playback.autoplay')}
                  subLanguages={subLanguages}
                  withDub={(['signs', 'off', 'same'] as const).find((v) => v === effective['subtitle.with_dub']) ?? 'signs'}
                  keptDelay={keptDelay}
                  onKeepDelay={group ? keepDelay : undefined}
                  subLook={asSubLook(effective['subtitle.look'])}
                  onSubLook={setSubLook}
                  subRaised={flag('subtitle.raise')}
                  onSubRaised={setSubRaised}
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
                  onResume={clearEnded}
                  onEndStuck={(detail) => note('end-stuck', detail)}
                  onSessionLost={recoverSession}
                  onUndecodable={convertInstead}
                  onSlowLink={slowLink}
                  onPickRelease={() => setPicking(true)}
                  castBurn={flag('cast.burn_subtitles')}
                  mini={mini ? { onExpand: () => routerNavigate(watchHere) } : undefined}
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
                        label={nextText ?? `Up next: ${fromList.title}`}
                        line={
                          fromList.episodeTitle && !endHides(fromList.animeId, fromList.episode, fromList.spoiler)
                            ? fromList.episodeTitle
                            : 'From your Up next list'
                        }
                        thumb={
                          fromList.animeId !== id
                            ? fromList.cover
                            : listed?.still && !endHides(id, listed.number, listed.spoiler)
                              ? listed.still
                              : undefined
                        }
                        then={after.data?.then && `Then ${after.data.then.title} · Ep ${after.data.then.episode}`}
                        auto={counting}
                        stopped={sleep.kind === 'fired' && autoNext}
                        onPlay={advance}
                        onCancel={() => clearEnded('cancel')}
                      />
                    ) : ended && next ? (
                      <UpNext
                        label={`Next: Episode ${next.display || next.number}`}
                        line={next.titleEn && !endHides(id, next.number, next.spoiler) ? next.titleEn : undefined}
                        thumb={next.still && !endHides(id, next.number, next.spoiler) ? next.still : undefined}
                        auto={counting}
                        stopped={sleep.kind === 'fired' && autoNext}
                        onPlay={advance}
                        onCancel={() => clearEnded('cancel')}
                      />
                    ) : ended && sequelPath && sequel ? (
                      <UpNext
                        label="Next season"
                        line={`${sequelTitle} · Episode 1`}
                        thumb={sequel.cover ?? undefined}
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
                    {anime4kDialog}
                  </>}
                />
              )}
            </div>

            {!mini && (
            <div
              className={cx(
                focus ? 'mx-auto w-full max-w-[calc((100dvh-3.5rem)*16/9)]' : 'space-y-4 p-3 sm:px-0 sm:pt-4',
              )}
            >
              {focus ? (
                <div className="flex h-14 items-center gap-2 px-4">
                  <p className="min-w-0 flex-1 truncate text-sm font-semibold text-white">
                    {showTitle} · Episode {current?.display ?? ep}
                  </p>
                  {previous && (
                    <Button onClick={goPrevious} title="The episode before">
                      ‹ Episode {previousName}
                    </Button>
                  )}
                  {nextButton}
                  <Button variant="ghost" onClick={() => setFocused(false)} aria-pressed title="Back to the whole page (Esc)">
                    Exit focus
                    <kbd className="rounded bg-white/10 px-1.5 py-0.5 font-sans text-xs text-base-300">Esc</kbd>
                  </Button>
                </div>
              ) : (
                <>
                  <div className="flex flex-wrap items-start gap-x-6 gap-y-3">
                    <div className="mr-auto grid min-w-0 gap-0.5">
                      <Link
                        to={`/anime/${id}`}
                        className="group -my-2 flex min-h-11 w-fit max-w-full items-center gap-1 text-sm font-semibold text-accent-300 transition-colors hover:text-accent-200"
                      >
                        <span className="line-clamp-2">{showTitle || 'Series'}</span>
                        <span aria-hidden className="transition-transform group-hover:translate-x-0.5">
                          ›
                        </span>
                      </Link>
                      <h1 className="line-clamp-2 font-display text-xl font-bold tracking-tight text-white sm:text-2xl">
                        {episodeLabel}
                      </h1>
                      {factLine && <p className="text-[13px] text-base-400">{factLine}</p>}
                    </div>
                    {wide && (previous || nextButton) && (
                      <div className="flex max-w-full flex-wrap items-center gap-2">
                        {previous && (
                          <Button onClick={goPrevious} title={`The episode before (${keyHint('previous')})`}>
                            ‹ Episode {previousName}
                          </Button>
                        )}
                        {nextButton}
                      </div>
                    )}
                  </div>
                  <Synopsis key={epKey} text={current?.overview} file={play.data?.title} />
                  {!wide && (previous || nextButton) && (
                    <div className="grid grid-cols-3 gap-2">
                      {previous && (
                        <button type="button" onClick={goPrevious} className={cx(buttonClass('secondary'), 'min-h-12')}>
                          ‹ Ep {previousName}
                        </button>
                      )}
                      {nextButton && <div className={cx('grid', previous ? 'col-span-2' : 'col-span-3')}>{nextButton}</div>}
                    </div>
                  )}
                </>
              )}

              {!focus && (
                <div className="flex flex-wrap items-center gap-2 max-sm:grid max-sm:grid-cols-4">
                  <StatusMenu animeId={id} current={detail.data?.listStatus} tile />
                  <button type="button" className={ACTION} onClick={() => setPicking(true)}>
                    <ActionLabel icon={<ReleaseGlyph />} long={infoHash ? 'Release chosen' : 'Choose release'} short="Release" />
                  </button>
                  {held ? (
                    <span className={cx(ACTION, 'pointer-events-none text-emerald-300')}>
                      <ActionLabel icon={<DoneGlyph />} long="Downloaded" />
                    </span>
                  ) : download.isSuccess ? (
                    <span className={cx(ACTION, 'pointer-events-none text-amber-300')}>
                      <ActionLabel icon={<DownloadGlyph />} long="Queued" />
                    </span>
                  ) : (
                    <button type="button" className={ACTION} onClick={() => download.mutate()} disabled={download.isPending}>
                      <ActionLabel icon={<DownloadGlyph />} long="Download" />
                    </button>
                  )}
                  {source && (
                    <button type="button" className={ACTION} onClick={() => setInApp(true)}>
                      <ActionLabel icon={<OpenGlyph />} long="Open or save" short="Open" />
                    </button>
                  )}
                  {!playingInMpv && <SaveToDevice animeId={id} episode={ep} className={ACTION} icon={<PhoneGlyph />} />}
                  {!together && !naming && !playingInMpv && (
                    <button
                      type="button"
                      className={ACTION}
                      onClick={() => {
                        if (!play.data) {
                          toast('Watch together starts once the episode is playing.', 'info')
                          return
                        }
                        startTogether.reset()
                        setNaming(true)
                      }}
                      title="Watch in step with friends who have their own Yoru"
                    >
                      <ActionLabel
                        icon={<TogetherGlyph />}
                        long="Watch together"
                        short="Together"
                        note={play.data ? undefined : 'once playing'}
                      />
                    </button>
                  )}
                  <button type="button" className={ACTION} onClick={openAudioSubtitles} aria-haspopup="dialog">
                    <ActionLabel icon={<SubtitlesGlyph />} long="Audio & subtitles" short="Audio & subs" />
                  </button>
                  {!playingInMpv &&
                    (wide ? (
                      <button
                        type="button"
                        className={ACTION}
                        onClick={() => setFocused(true)}
                        aria-pressed={false}
                        title="Just the player on black; Esc leaves"
                      >
                        <ActionLabel icon={<FocusGlyph />} long="Focus" />
                      </button>
                    ) : (
                      <button
                        type="button"
                        className={ACTION}
                        onClick={() => playerControls.current?.fullscreen()}
                        disabled={!playerShown}
                      >
                        <ActionLabel
                          icon={<FocusGlyph />}
                          long="Full screen"
                          note={playerShown ? undefined : 'once open'}
                        />
                      </button>
                    ))}
                </div>
              )}

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

              {!focus && (
                <>
                  {!side && episodesSection}
                  <CharacterRail animeId={id} limit={8} title="Cast" />
                </>
              )}
            </div>
            )}
            {mini && (
              <MiniBar title={showTitle} episode={episodeLabel} onExpand={() => routerNavigate(watchHere)} onClose={() => onClose?.()} />
            )}
          </div>

          {!mini && !focus && side && <aside className="pb-8">{episodesSection}</aside>}
        </div>
      </div>

      {!playerShown && anime4kDialog}
      <Sheet open={avSheet} onClose={() => setAvSheet(false)} title="Audio & subtitles">
        <div className="-mx-4 sm:-mx-5">
          <PanelGroup title="Audio">{audioChoice}</PanelGroup>
          <PanelGroup title="Subtitles">
            <p className="px-4 py-2 text-sm text-base-400">
              {playingInMpv
                ? `${play.data?.player === 'vlc' ? 'VLC' : 'mpv'} shows its own subtitle tracks while it plays.`
                : "The episode's subtitle tracks, size and look are here once it opens."}
            </p>
          </PanelGroup>
          <PanelGroup title="Playback">{playbackRows}</PanelGroup>
        </div>
      </Sheet>

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

function Synopsis({ text, file }: { text?: string; file?: string }) {
  const [open, setOpen] = useState(false)
  if (!text) return null
  return (
    <div className="text-sm text-base-300">
      <p className={cx('leading-relaxed', !open && 'line-clamp-2')}>{text}</p>
      {open && file && <p className="mt-1.5 text-xs break-all text-base-400">Release: {file}</p>}
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="tap relative mt-0.5 text-sm font-semibold text-base-100 transition-colors hover:text-white"
      >
        {open ? 'Less' : 'More'}
      </button>
    </div>
  )
}

function outDay(unix: number | undefined, now: number): string {
  if (!unix) return 'not out yet'
  const ms = unix * 1000 - now
  if (ms < 86_400_000) return `out ${relativeTime(unix)}`
  if (ms < 6 * 86_400_000) return `out ${new Date(unix * 1000).toLocaleDateString(undefined, { weekday: 'short' })}`
  return `out ${new Date(unix * 1000).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}`
}

function outFull(unix: number): string {
  const day = new Date(unix * 1000).toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })
  return `${day} at ${airTime(unix)}`
}

function NotOut({ label, children }: { label: string; children: React.ReactNode }) {
  const [open, setOpen] = useState(false)
  const anchor = useRef<HTMLButtonElement>(null)
  return (
    <>
      <button
        ref={anchor}
        type="button"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(true)}
        className={cx(buttonClass('secondary'), 'min-w-0 text-base-300 max-sm:min-h-12')}
      >
        <span className="truncate">{label}</span>
      </button>
      <PopSheet open={open} onClose={() => setOpen(false)} title="Not out yet" anchor={anchor} width={340}>
        <div className="p-2.5 max-sm:px-0">{children}</div>
      </PopSheet>
    </>
  )
}

function SequelLater({ sequel }: { sequel: Season }) {
  const alert = useSetNotify(sequel.id)
  const alertsOff = useAlertsOff()
  const name = sequel.english || sequel.romaji
  return (
    <>
      <p className="text-sm text-base-100">
        {name} is {sequel.year ? `coming in ${sequel.year}` : 'announced'}. This was the last episode out so far.
      </p>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        {alertsOff ? (
          <LinkButton to="/settings?tab=Notifications">Alerts are off: turn them on</LinkButton>
        ) : alert.isSuccess ? (
          <p role="status" className="text-sm text-base-200">
            You get an alert when it starts.
          </p>
        ) : (
          <Button variant="primary" onClick={() => alert.mutate(true)} disabled={alert.isPending}>
            Tell me when it starts
          </Button>
        )}
        <LinkButton to={`/anime/${sequel.id}`}>Open {name}</LinkButton>
      </div>
      {alert.isError && <p className="mt-2 text-xs text-recap">{(alert.error as Error).message}</p>}
    </>
  )
}

interface PlayStage {
  step?: 'search' | 'files' | 'data' | 'start'
  release?: string
  peers?: number
  bytes?: number
}

function useStartingNote(animeId: number, episode: number, display: number, waiting: boolean, found: boolean): Starting {
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

  const name = `Episode ${display}`
  switch (stage?.step) {
    case 'files':
      return {
        step: 1,
        seconds,
        status: 'Found a release. Asking the people sharing it for its list of files.',
      }
    case 'data':
      return {
        step: 1,
        seconds,
        status: stage.peers
          ? `Connected to ${stage.peers} ${stage.peers === 1 ? 'person' : 'people'} sharing this release${
              stage.bytes ? `, ${bytes(stage.bytes)} so far` : ''
            }. This usually takes a few seconds.`
          : 'Looking for people sharing this release. No one has answered yet.',
      }
    case 'start':
      return {
        step: 2,
        seconds,
        status: 'Reading the start of the episode. A large release on a slow connection can take a few minutes.',
      }
    default:
      if (found) return { step: 2, seconds, status: `Opening ${name}.` }
      return {
        step: 0,
        seconds,
        status:
          seconds < 8
            ? `Checking release sites for ${name}.`
            : 'Still checking release sites. Sites limit how fast we can search, so this can take a minute.',
        stuck: seconds >= 60 ? `No release found yet for ${name}` : undefined,
      }
  }
}

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

function WhenOut({
  animeId,
  episode,
  alerting = false,
  start = false,
}: {
  animeId: number
  episode: number
  alerting?: boolean
  start?: boolean
}) {
  const qc = useQueryClient()
  const [done, setDone] = useState<'notify' | 'want' | null>(null)
  const notify = useSetNotify(animeId)
  const alertsOff = useAlertsOff()
  const want = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post(`/api/anime/${animeId}/want`, { episode }),
    onSuccess: () => {
      setDone('want')
      void qc.invalidateQueries({ queryKey: ['anime', animeId] })
    },
  })
  const pending = notify.isPending || want.isPending
  const failed = (notify.error ?? want.error) as Error | null
  if (done) {
    return (
      <p role="status" className={cx('text-sm text-base-200', start ? 'mt-3' : 'mt-4')}>
        {done === 'notify'
          ? 'You get an alert when it is out.'
          : alertsOff
            ? `Episode ${episode} downloads when it is out.`
            : `Episode ${episode} downloads when it is out, and you get an alert.`}
      </p>
    )
  }
  return (
    <div className={cx('flex flex-wrap items-center gap-2', start ? 'mt-3 justify-start' : 'mt-4 justify-center')}>
      {alertsOff ? (
        <LinkButton to="/settings?tab=Notifications">Alerts are off: turn them on</LinkButton>
      ) : alerting ? (
        <p className="basis-full text-sm text-base-300">Alerts are on for this show.</p>
      ) : (
        <Button variant="primary" onClick={() => notify.mutate(true, { onSuccess: () => setDone('notify') })} disabled={pending}>
          Tell me when it's out
        </Button>
      )}
      <Button variant={alertsOff ? 'primary' : 'secondary'} onClick={() => want.mutate()} disabled={pending}>
        Download it when it's out
      </Button>
      {failed && <p className="basis-full text-xs text-recap">{failed.message}</p>}
    </div>
  )
}

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
  notOut?: boolean
  rawAvailable?: boolean
  rawTitle?: string
  inPlayer?: boolean
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
        <p className="text-sm text-base-200">{message.charAt(0).toUpperCase() + message.slice(1)}</p>

        {rawAvailable && (
          <div className="mt-4 rounded-lg border border-base-800 bg-base-900/60 p-3 text-left">
            <p className="text-xs text-base-400">
              A raw broadcast is seeding now. It has no subtitles.
            </p>
            {rawTitle && (
              <p className="mt-1 line-clamp-2 font-mono text-xs text-base-500">{rawTitle}</p>
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
          <Button variant="primary" onClick={onRetry}>
            Retry
          </Button>
          <Button onClick={onPick}>Try another release</Button>
          <LinkButton to={`/anime/${animeId}`}>Other episodes</LinkButton>
          <LinkButton to="/settings?tab=Downloads" variant="ghost">Quality settings</LinkButton>
        </div>
        )}
      </div>
    </div>
  )
}

function MpvPanel({ title, animeId, episode, player }: { title: string; animeId: number; episode: number; player: string }) {
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

function UpNext({
  label,
  line,
  thumb,
  then,
  auto,
  stopped,
  onPlay,
  onCancel,
}: {
  label: string
  line?: string
  thumb?: string
  then?: string
  auto: boolean
  stopped?: boolean
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
      className="absolute right-3 bottom-16 z-20 w-[23rem] max-w-[calc(100%-1.5rem)] animate-rise rounded-sheet bg-base-950/90 p-3 shadow-xl shadow-black/60 ring-1 ring-white/10 backdrop-blur-sm sm:right-4 sm:bottom-20"
    >
      <div className="flex items-start gap-3">
        {thumb && (
          <img src={thumb} alt="" className="hidden h-[72px] w-32 shrink-0 rounded-md object-cover ring-1 ring-white/10 sm:block" />
        )}
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-semibold text-white">{label}</p>
          {line && <p className="mt-0.5 line-clamp-2 text-[13px] text-base-300">{line}</p>}
          {then && <p className="mt-0.5 truncate text-xs text-base-400">{then}</p>}
          {stopped && <p className="mt-1 text-xs text-base-400">The sleep timer stopped auto next.</p>}
        </div>
        {auto && <CountdownRing left={left} total={UP_NEXT_SECONDS} />}
      </div>
      <div className="mt-2.5 flex items-center gap-2">
        <button
          onClick={() => onPlay('card')}
          className="min-h-11 flex-1 rounded-xl bg-white px-4 text-sm font-semibold text-base-950 transition-colors hover:bg-base-100"
        >
          Play now
        </button>
        <button
          onClick={onCancel}
          className="min-h-11 rounded-xl px-4 text-sm text-base-200 transition-colors hover:bg-base-800 hover:text-white"
        >
          {auto ? 'Cancel' : 'Dismiss'}
        </button>
      </div>
    </div>
  )
}

function CountdownRing({ left, total }: { left: number; total: number }) {
  const r = 15
  const around = 2 * Math.PI * r
  return (
    <span role="timer" aria-label={`Plays in ${left} seconds`} className="relative grid size-11 shrink-0 place-items-center">
      <svg viewBox="0 0 36 36" className="absolute inset-0 size-full -rotate-90" aria-hidden>
        <circle cx="18" cy="18" r={r} fill="none" stroke="rgb(255 255 255 / 0.15)" strokeWidth="3" />
        <circle
          cx="18"
          cy="18"
          r={r}
          fill="none"
          stroke="var(--color-accent-400)"
          strokeWidth="3"
          strokeLinecap="round"
          strokeDasharray={around}
          strokeDashoffset={around * (1 - left / total)}
          className="transition-[stroke-dashoffset] duration-1000 ease-linear"
        />
      </svg>
      <span className="text-sm font-semibold text-white tabular-nums">{left}</span>
    </span>
  )
}

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
  alerting: boolean
  onClose: () => void
}) {
  const qc = useQueryClient()
  const alertFor = upcoming ? animeId : sequel?.id
  const alert = useSetNotify(alertFor ?? 0)
  const alertsOff = useAlertsOff()
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
      className="absolute right-4 bottom-20 z-20 w-[22rem] max-w-[calc(100%-2rem)] animate-rise rounded-sheet bg-base-950/90 p-3 shadow-xl shadow-black/60 ring-1 ring-white/10 backdrop-blur-sm"
    >
      <p className="text-[13px] font-semibold text-base-400">
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
        {alertFor && alertsOff ? (
          <span className="text-xs text-base-400">Alerts are off for every show in Settings.</span>
        ) : alertFor && upcoming && alerting ? (
          <span className="text-xs text-base-400">Alerts are on for this show.</span>
        ) : (
          alertFor && (
            <button
              onClick={() => alert.mutate(true)}
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
          <p className="text-xs text-base-400">Your score</p>
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
          <p className="text-xs text-base-400">More like this</p>
          <div className="mt-1.5 grid grid-cols-3 gap-2">
            {recs.data!.items.slice(0, 3).map((r) => (
              <Link key={r.id} to={`/anime/${r.id}`} className="group min-w-0">
                <img
                  src={r.cover ?? r.thumb}
                  alt=""
                  loading="lazy"
                  className="aspect-[2/3] w-full rounded-md object-cover ring-1 ring-white/10 transition-transform group-hover:scale-[1.03]"
                />
                <p className="mt-1 line-clamp-1 text-xs text-base-300 group-hover:text-white">{r.title}</p>
              </Link>
            ))}
          </div>
        </div>
      )}
      {failed && <p className="mt-2 text-xs text-recap">{failed.message}</p>}
    </div>
  )
}

function StillWatching({ run, onKeep, onStop }: { run: Unattended[]; onKeep: () => void; onStop: () => void }) {
  const qc = useQueryClient()
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
      className="absolute right-4 bottom-20 z-20 w-80 max-w-[calc(100%-2rem)] animate-rise rounded-sheet bg-base-950/90 p-3 shadow-xl shadow-black/60 ring-1 ring-white/10 backdrop-blur-sm"
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

const ACTION =
  'press relative flex h-10 min-w-0 shrink-0 items-center justify-center gap-2 rounded-xl bg-base-800 px-3.5 text-sm font-medium text-base-100 transition-colors hover:bg-base-750 disabled:opacity-50 max-sm:h-auto max-sm:min-h-16 max-sm:flex-col max-sm:gap-1 max-sm:px-1 max-sm:py-2 max-sm:text-xs'

function ActionLabel({ icon, long, short, note }: { icon: React.ReactNode; long: string; short?: string; note?: string }) {
  return (
    <>
      {icon}
      <span className="max-sm:hidden">{long}</span>
      <span className="leading-tight sm:hidden">{short ?? long}</span>
      {note && <span className="text-xs font-normal text-base-400 max-sm:-mt-1">{note}</span>}
    </>
  )
}

function Glyph({ d }: { d: string }) {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0 max-sm:size-5" aria-hidden>
      <path d={d} fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

const ReleaseGlyph = () => <Glyph d="M4 6h16M4 12h16M4 18h10" />
const DownloadGlyph = () => <Glyph d="M12 4v11m0 0 4-4m-4 4-4-4M5 19h14" />
const DoneGlyph = () => <Glyph d="m5 13 4 4L19 7" />
const OpenGlyph = () => <Glyph d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
const PhoneGlyph = () => <Glyph d="M8 3h8a1 1 0 0 1 1 1v16a1 1 0 0 1-1 1H8a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1ZM12 8v6m0 0 2.5-2.5M12 14l-2.5-2.5M11 18h2" />
const TogetherGlyph = () => <Glyph d="M9 11a3 3 0 1 0 0-6 3 3 0 0 0 0 6ZM3 19c0-3 2.7-5 6-5s6 2 6 5M16 5.2a3 3 0 0 1 0 5.6M18 14.4c1.8.7 3 2.3 3 4.6" />
const FocusGlyph = () => <Glyph d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
const SubtitlesGlyph = () => <Glyph d="M5 5h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2ZM7 15h4M13 15h4M7 11h10" />
