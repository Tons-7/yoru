import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type RefObject,
} from 'react'
import { createPortal } from 'react-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import {
  api,
  type AudioTrack,
  type SkipMarkResult,
  type SkipRange,
  type StreamHealth,
  type StreamInfo,
  type SubtitleTrack,
  DEFAULT_SOUND,
  sameSound,
  soundFrom,
  type SoundOptions,
  VOICE_LEVELS,
  type VoiceLevel,
} from '../lib/api'
import { clockTime, cx, languageName, speed } from '../lib/format'
import { refreshDownloads, useAccess, useNow, useSpeedUnit } from '../lib/queries'
import { SHOW_SHORTCUTS } from '../components/keys'
import { PlayIcon } from '../components/PosterCard'
import { Spinner } from '../components/ui'
import { menuItems, useDismiss, useMediaQuery, useMenuButton, useModalFocus, useRoomAbove, walkMenu } from '../lib/hooks'
import { readKey, writeKey } from '../lib/storage'
import { buttonClass } from '../lib/ui'
import { copyText } from '../lib/downloads'
import { attachVideo, detachVideo } from '../lib/miniplayer'
import { actionFor, keyHint, keyScope, useKeymap, type KeyAction } from '../lib/keymap'
import { getSleep, noteInput, setSleep, useSleep, type Sleep } from '../lib/watching'
import { useAnime4K, type UpscaleTier } from './anime4k'
import { useTouchGestures } from './gestures'
import { castImpossible, episodeKey, isBrave, useCast, type CastPhase, type CastSdk, type QueuedMedia } from './cast'
import { useAirPlay } from './airplay'
import {
  SKIPPED_GUARD,
  skipKind,
  untilWatching,
  useAutoSkip,
  useDocumentPiP,
  useEmbeddedFonts,
  useFreshFrames,
  useHlsSource,
  useKeepAlive,
  useSeekRecovery,
  useSlowLink,
  useStallWatchdog,
  useSubtitles,
  useSystemFullscreen,
  useThumbnails,
  useVideoPiP,
  type AutoSkip,
  type Sheet,
} from './hooks'
import { useNativeCaptions } from './captions'
import { QUALITIES, setDeviceQuality, useNetwork } from './network'
import { ENCODERS } from './encoders'
import { readSubtitleFile } from './ownSubs'
import { castStyle, previewStyle, SUB_LOOKS, subLookLabel, type SubLook } from './subLook'
import { StartingSteps, type Starting } from './Starting'
import { ActionRow, ChoiceRow, Choices, PanelGroup, SegmentRow } from './panel'
import { SKIP_MODES, type SkipMode } from './skipMode'

function toggleFullscreen(shell: HTMLElement | null, video: HTMLVideoElement | null, onNoLock?: () => void) {
  if (document.fullscreenElement) {
    void document.exitFullscreen().catch(() => {})
  } else if (shell?.requestFullscreen) {
    void shell
      .requestFullscreen()
      .then(() => {
        const o = screen.orientation as ScreenOrientation & { lock?: (to: string) => Promise<void> }
        if (!video || video.videoWidth <= video.videoHeight) return
        if (!o?.lock) return onNoLock?.()
        return o.lock('landscape').catch(() => onNoLock?.())
      })
      .catch(() => {})
  } else {
    ;(video as (HTMLVideoElement & { webkitEnterFullscreen?: () => void }) | null)?.webkitEnterFullscreen?.()
  }
}

function resumeAfterRebuild(video: HTMLVideoElement | null, wasPlaying: boolean) {
  if (!video || !wasPlaying) return
  video.addEventListener('canplay', () => void video.play().catch(() => {}), { once: true })
}

const LANGUAGE_TAGS: Record<string, string[]> = {
  en: ['en', 'eng', 'english'],
  es: ['es', 'spa', 'spanish', 'es-419', 'es-es'],
  pt: ['pt', 'por', 'portuguese', 'pt-br'],
  fr: ['fr', 'fre', 'fra', 'french'],
  de: ['de', 'ger', 'deu', 'german'],
  it: ['it', 'ita', 'italian'],
  ru: ['ru', 'rus', 'russian'],
  ar: ['ar', 'ara', 'arabic'],
  zh: ['zh', 'chi', 'zho', 'chinese'],
  ko: ['ko', 'kor', 'korean'],
  id: ['id', 'ind', 'indonesian'],
  vi: ['vi', 'vie', 'vietnamese'],
  th: ['th', 'tha', 'thai'],
}
const SIGNS = /\b(signs?|songs?|s&s|forced)\b/i

function chooseTrack(tracks: SubtitleTrack[], languages: string[] = ['en']): SubtitleTrack {
  const dialogue = (t: SubtitleTrack) => !SIGNS.test(t.title ?? '')
  const speaks = (code: string) => (t: SubtitleTrack) =>
    (LANGUAGE_TAGS[code] ?? [code]).includes((t.language ?? '').toLowerCase())

  for (const code of languages) {
    const is = speaks(code)
    const found =
      tracks.find((t) => is(t) && dialogue(t) && t.default) ??
      tracks.find((t) => is(t) && dialogue(t)) ??
      tracks.find((t) => is(t))
    if (found) return found
  }
  return (
    tracks.find((t) => dialogue(t) && t.default) ??
    tracks.find((t) => dialogue(t)) ??
    tracks.find((t) => t.default) ??
    tracks[0]
  )
}

function chooseWithDub(
  tracks: SubtitleTrack[],
  languages: string[],
  audioLanguage: string,
  withDub: 'signs' | 'off' | 'same',
): SubtitleTrack | null {
  const understood = languages.some((code) => (LANGUAGE_TAGS[code] ?? [code]).includes(audioLanguage.toLowerCase()))
  if (!understood || withDub === 'same') return chooseTrack(tracks, languages)
  if (withDub === 'off') return null
  for (const code of languages) {
    const tags = LANGUAGE_TAGS[code] ?? [code]
    const signs = tracks.find((t) => tags.includes((t.language ?? '').toLowerCase()) && SIGNS.test(t.title ?? ''))
    if (signs) return signs
  }
  return null
}

const SPEEDS = [0.75, 1, 1.25, 1.5, 2]

const PERSIST_KEY = 'yoru.player'
interface Persisted {
  volume?: number
  muted?: boolean
  rate?: number
  subScale?: number
}

const VOLUME_NOTICE = 'volume'
const SUB_SIZES = [
  { value: '0.75', label: 'XS', name: 'Extra small' },
  { value: '0.9', label: 'S', name: 'Small' },
  { value: '1', label: 'M', name: 'Medium' },
  { value: '1.15', label: 'L', name: 'Large' },
  { value: '1.3', label: 'XL', name: 'Extra large' },
  { value: '1.5', label: 'XXL', name: 'Largest' },
] as const
const SIDEWAYS_KEY = 'yoru.sideways.hint'
const RUN_MS = 700
const DELAY_STEP = 0.1
const SUBTITLE_WAIT = 5000
const OWN_TRACK = -1
const JIMAKU_TRACK = -100
const NO_TRACKS: SubtitleTrack[] = []

interface JimakuFile {
  index: number
  name: string
  url: string
}

function delayLabel(d: number): string {
  return d === 0 ? '0.0 s' : `${d > 0 ? '+' : ''}${d.toFixed(1)} s`
}
function loadPersisted(): Persisted {
  try {
    return JSON.parse(localStorage.getItem(PERSIST_KEY) ?? '{}') as Persisted
  } catch {
    return {}
  }
}
function savePersisted(patch: Persisted) {
  try {
    localStorage.setItem(PERSIST_KEY, JSON.stringify({ ...loadPersisted(), ...patch }))
  } catch {
  }
}

export interface SkipSetting {
  value: SkipMode
  caption: string
  onChange: (value: SkipMode) => void
}

export interface PlaybackSettings {
  openings: SkipSetting
  endings: SkipSetting
  filler: { on: boolean; onChange: (on: boolean) => void }
  autoNext: { on: boolean; caption: string; onChange: (on: boolean) => void }
  autoplay: { on: boolean; caption: string; onChange: (on: boolean) => void }
  upscale?: { value: string; onOpen: () => void }
}

export interface PlayerControls {
  fullscreen: () => void
  settings: (open: boolean) => void
}

export interface PlayerFacts {
  audio?: string
  subtitles?: string
}

export interface PlayerProps {
  stream?: StreamInfo
  starting?: Starting
  onStartCancel?: () => void
  onStartRetry?: () => void
  startAt: number
  skips: SkipRange[]
  autoSkip: AutoSkip
  skipOff?: { op: boolean; ed: boolean }
  onMinimize?: () => void
  settings?: { audio?: ReactNode; playback?: PlaybackSettings }
  controls?: RefObject<PlayerControls | null>
  theatre?: boolean
  onTheatre?: () => void
  onFacts?: (facts: PlayerFacts) => void
  nextLabel?: string
  onSkipVote?: (id: string, up: boolean) => void
  skipMarks?: { mark: (kind: 'op' | 'ed', start: number, end: number) => Promise<SkipMarkResult>; forget: () => void }
  autoPlay: boolean
  subLanguages?: string[]
  withDub?: 'signs' | 'off' | 'same'
  subLook?: SubLook
  onSubLook?: (look: SubLook) => void
  subRaised?: boolean
  onSubRaised?: (raised: boolean) => void
  keptDelay?: number
  onKeepDelay?: (delay: number) => void
  upscale?: { enabled: boolean; mode: string; tier?: UpscaleTier; onSlow?: () => void }
  sound?: { options: SoundOptions; onChange: (next: SoundOptions) => void }
  title: string
  subtitle?: string
  artwork?: string
  onProgress: (position: number, duration: number, played: number, final: boolean, paused: boolean) => void
  onEnded: () => void
  onNext?: (by: 'button' | 'key' | 'media') => void
  onPrevious?: () => void
  onTVMovedOn?: (animeId: number, episode: number) => void
  onResume?: (by: 'play' | 'seek') => void
  onEndStuck?: (detail: Record<string, unknown>) => void
  onSessionLost?: () => Promise<unknown>
  onUndecodable?: () => boolean
  onSlowLink?: () => void
  onPickRelease?: () => void
  overlay?: ReactNode
  castBurn?: boolean
  mini?: { onExpand: () => void }
}

export function Player({
  stream,
  starting,
  onStartCancel,
  onStartRetry,
  startAt,
  skips,
  autoSkip,
  skipOff,
  onMinimize,
  settings,
  controls,
  theatre = false,
  onTheatre,
  onFacts,
  nextLabel,
  onSkipVote,
  skipMarks,
  autoPlay,
  subLanguages,
  withDub = 'signs',
  keptDelay,
  onKeepDelay,
  subLook: lookPreset = '',
  onSubLook,
  subRaised = false,
  onSubRaised,
  upscale,
  sound,
  title,
  subtitle,
  artwork,
  onProgress,
  onEnded,
  onNext,
  onPrevious,
  onTVMovedOn,
  onResume,
  onEndStuck,
  onSessionLost,
  onUndecodable,
  onSlowLink,
  onPickRelease,
  overlay,
  castBurn = false,
  mini,
}: PlayerProps) {
  const [video, setVideo] = useState<HTMLVideoElement | null>(null)
  const [canvas, setCanvas] = useState<HTMLCanvasElement | null>(null)
  const qc = useQueryClient()
  const shell = useRef<HTMLDivElement>(null)
  const [host] = useState(() => document.createElement('div'))
  const anchor = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    anchor.current?.append(host)
    return () => host.remove()
  }, [host])

  useEffect(() => {
    attachVideo(video)
    return () => detachVideo(video)
  }, [video])

  const [playing, setPlaying] = useState(false)
  const [waiting, setWaiting] = useState(true)
  const [time, setTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(1)
  const [muted, setMuted] = useState(false)
  const [rate, setRate] = useState(() => loadPersisted().rate ?? 1)
  const fast = useRef(false)
  const [controlsVisible, setControlsVisible] = useState(true)
  const [notice, setNotice] = useState<string | null>(null)
  const noticeTimer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(noticeTimer.current), [])
  const showNotice = useCallback((what: string, ms = 900) => {
    setNotice(what)
    window.clearTimeout(noticeTimer.current)
    noticeTimer.current = window.setTimeout(() => setNotice(null), ms)
  }, [])
  const showVolume = useCallback(() => showNotice(VOLUME_NOTICE), [showNotice])
  const [track, setTrack] = useState<number | null>(null)

  const [subScale, setSubScale] = useState(() => loadPersisted().subScale ?? 1)
  const changeSubScale = useCallback((s: number) => {
    setSubScale(s)
    savePersisted({ subScale: s })
  }, [])
  const stepSubSize = useCallback(
    (by: number) => {
      const at = SUB_SIZES.findIndex((s) => Number(s.value) >= subScale - 0.001)
      const from = at < 0 ? SUB_SIZES.length - 1 : at
      const to = SUB_SIZES[Math.min(SUB_SIZES.length - 1, Math.max(0, from + by))]
      changeSubScale(Number(to.value))
      showNotice(`Subtitle size: ${to.name}`)
    },
    [subScale, changeSubScale, showNotice],
  )
  const [subDelay, setSubDelay] = useState(0)
  useEffect(() => {
    setSubDelay(keptDelay ?? 0)
    if (keptDelay) showNotice(`Subtitle delay ${delayLabel(keptDelay)} (kept for this show)`, 2500)
  }, [stream?.id, keptDelay, showNotice])
  const imageSubtitles = !!stream?.imageSubtitles
  useEffect(() => {
    if (imageSubtitles) showNotice('Subtitles here are images; mpv or VLC shows them', 8000)
  }, [stream?.id, imageSubtitles, showNotice])
  const subDelayRef = useRef(subDelay)
  subDelayRef.current = subDelay
  const shiftSubDelay = useCallback(
    (by: number) => {
      const next = Math.round((subDelayRef.current + by) * 10) / 10
      setSubDelay(next)
      showNotice(`Subtitle delay ${delayLabel(next)}`)
    },
    [showNotice],
  )

  useEffect(() => {
    if (!video) return
    const saved = loadPersisted()
    if (typeof saved.volume === 'number') video.volume = Math.min(1, Math.max(0, saved.volume))
    if (typeof saved.muted === 'boolean') video.muted = saved.muted
    const apply = () => {
      const want = fast.current ? 2 : rate
      if (video.playbackRate !== want) video.playbackRate = want
    }
    apply()
    for (const ev of ['loadstart', 'loadedmetadata', 'canplay']) video.addEventListener(ev, apply)
    return () => {
      for (const ev of ['loadstart', 'loadedmetadata', 'canplay']) video.removeEventListener(ev, apply)
    }
  }, [video, rate])

  const changeRate = useCallback((r: number) => {
    setRate(r)
    savePersisted({ rate: r })
  }, [])

  const [audioTrack, setAudioTrack] = useState(0)
  const [soundState, setSoundState] = useState<SoundOptions>(sound?.options ?? DEFAULT_SOUND)
  const [epoch, setEpoch] = useState(0)
  const [resumeAt, setResumeAt] = useState(startAt)
  const [lostError, setLostError] = useState<string | null>(null)

  const [attached, setAttached] = useState(stream?.id)
  if (stream?.id !== attached) {
    setAttached(stream?.id)
    setAudioTrack(stream?.audioTrack ?? 0)
    setSoundState(stream?.sound ? soundFrom(stream.sound) : (sound?.options ?? DEFAULT_SOUND))
    setEpoch(0)
    setResumeAt(startAt)
    setTime(0)
    setDuration(0)
    setWaiting(true)
    setLostError(null)
  }

  const playlist = stream?.playlist
    ? `${stream.playlist}${epoch ? `?a=${epoch}` : ''}`
    : undefined

  const liveId = useRef(stream?.id)
  liveId.current = stream?.id
  const audioChosen = useRef(false)
  const switchAudio = useCallback(
    (index: number) => {
      const id = stream?.id
      if (!id || index === audioTrack) return
      audioChosen.current = true
      const before = audioTrack
      const playing = !!video && !video.paused && !video.ended
      setAudioTrack(index)
      void api.post(`/api/stream/${id}/audio?track=${index}`).then(
        () => {
          if (liveId.current !== id) return
          resumeAfterRebuild(video, playing)
          setResumeAt(video?.currentTime ?? startAt)
          setWaiting(true)
          setEpoch((e) => e + 1)
        },
        () => {
          if (liveId.current !== id) return
          setAudioTrack(before)
          showNotice("Couldn't switch the audio track", 3000)
        },
      )
    },
    [stream?.id, audioTrack, video, startAt, showNotice],
  )
  useEffect(() => {
    audioChosen.current = false
  }, [stream?.id])

  const switchSound = useCallback(
    (patch: Partial<SoundOptions>) => {
      const id = stream?.id
      const next = { ...soundState, ...patch }
      if (!id || sameSound(next, soundState)) return
      const before = soundState
      const playing = !!video && !video.paused && !video.ended
      setSoundState(next)
      void api.post(`/api/stream/${id}/sound${soundQuery(patch)}`).then(
        () => {
          if (liveId.current !== id) return
          resumeAfterRebuild(video, playing)
          setResumeAt(video?.currentTime ?? startAt)
          setWaiting(true)
          setEpoch((e) => e + 1)
          sound?.onChange(next)
        },
        () => {
          if (liveId.current !== id) return
          setSoundState(before)
          showNotice("Couldn't change the sound", 3000)
        },
      )
    },
    [stream?.id, soundState, video, startAt, sound, showNotice],
  )
  const cycleVoice = useCallback(() => {
    switchSound({ voice: VOICE_LEVELS[(VOICE_LEVELS.indexOf(soundState.voice) + 1) % VOICE_LEVELS.length] })
  }, [switchSound, soundState.voice])

  const recoverRef = useRef<() => void>(() => {})
  const recovering = useRef(false)
  const recover = useCallback(() => recoverRef.current(), [])

  const undecodable = useCallback(() => {
    if (!onUndecodable?.()) return false
    showNotice("This device couldn't play the original, so it's converted instead", 6000)
    return true
  }, [onUndecodable, showNotice])
  const airplaySub = track == null ? 'off' : stream?.subtitles.some((t) => t.index === track) ? String(track) : ''
  const airplayLink = stream?.airplay && (airplaySub ? `${stream.airplay}?sub=${airplaySub}` : stream.airplay)
  const { error, final, hls, streamEnded } = useHlsSource(video, playlist, resumeAt, recover, undecodable, airplayLink)
  const airplay = useAirPlay(video)
  useKeepAlive(video, playlist, recover)
  recoverRef.current = () => {
    if (recovering.current || !onSessionLost) return
    recovering.current = true
    const wasPlaying = !!video && !video.paused && !video.ended
    const track = audioTrack
    const snd = soundState
    void onSessionLost()
      .then(() => {
        setLostError(null)
        if (audioChosen.current && stream?.id) {
          void api.post(`/api/stream/${stream.id}/audio?track=${track}`).catch(() => {})
        }
        if (!sameSound(snd, DEFAULT_SOUND) && stream?.id) {
          void api.post(`/api/stream/${stream.id}/sound${soundQuery(snd)}`).catch(() => {})
        }
        setResumeAt(video?.currentTime ?? startAt)
        setWaiting(true)
        setEpoch((e) => e + 1)
        resumeAfterRebuild(video, wasPlaying)
      })
      .catch((err: unknown) => setLostError(err instanceof Error ? err.message : 'Playback stopped'))
      .finally(() => {
        recovering.current = false
      })
  }
  useSeekRecovery(video, hls)
  useStallWatchdog(video, hls)
  useSlowLink(video, onSlowLink)
  const fonts = useEmbeddedFonts(stream?.fontsUrl, epoch)
  const pip = useDocumentPiP(host)
  const [extras, setExtras] = useState<{ stream?: string; tracks: SubtitleTrack[] }>({ tracks: [] })
  const extraTracks = extras.stream === stream?.id ? extras.tracks : NO_TRACKS
  const subtitleStream = useMemo(
    () => (stream && extraTracks.length ? { ...stream, subtitles: [...stream.subtitles, ...extraTracks] } : stream),
    [stream, extraTracks],
  )
  const subtitleTracks = subtitleStream?.subtitles ?? NO_TRACKS
  const addTrack = useCallback(
    (t: SubtitleTrack) => {
      if (!stream?.id) return
      setExtras((prev) => {
        const kept = prev.stream === stream.id ? prev.tracks : []
        return { stream: stream.id, tracks: [...kept.filter((old) => old.index !== t.index), t] }
      })
      setTrack(t.index)
    },
    [stream?.id],
  )
  const blobs = useRef(new Set<string>())
  useEffect(() => {
    const live = new Set(extraTracks.map((t) => t.url).filter((url) => url.startsWith('blob:')))
    for (const url of blobs.current) if (!live.has(url)) URL.revokeObjectURL(url)
    blobs.current = live
  }, [extraTracks])
  useEffect(() => () => blobs.current.forEach((url) => URL.revokeObjectURL(url)), [])
  const subLook = useMemo(
    () => ({ scale: subScale, delay: subDelay, preset: lookPreset, raised: subRaised }),
    [subScale, subDelay, lookPreset, subRaised],
  )
  const subtitlesReady = useSubtitles(video, subtitleStream, track, fonts, pip.active, subLook, (problem) =>
    showNotice(problem, 6000),
  )
  const videoPip = useVideoPiP(video)
  const systemFullscreen = useSystemFullscreen(video)
  useNativeCaptions(
    video,
    subtitleTracks.find((t) => t.index === track && !t.pictures),
    videoPip.active || systemFullscreen,
    subDelay,
  )
  const loadOwnFile = useCallback(
    async (file: File) => {
      try {
        const ass = await readSubtitleFile(file)
        addTrack({ index: OWN_TRACK, title: file.name, url: URL.createObjectURL(new Blob([ass])), whole: true })
        showNotice(`Subtitles: ${file.name}`)
      } catch (e) {
        showNotice(e instanceof Error ? e.message : "Couldn't read that file", 6000)
      }
    },
    [addTrack, showNotice],
  )
  const saved = !!stream?.saved
  const jimakuSet = useQuery({
    queryKey: ['jimaku'],
    queryFn: () => api.get<{ set: boolean }>('/api/jimaku'),
    enabled: !saved,
  }).data?.set
  const autoSkipped = useAutoSkip(video, skips, autoSkip, (r) => {
    if (r.source === 'aniskip' && r.id) onSkipVote?.(r.id, true)
  })

  const upscaleState = useAnime4K({
    video,
    canvas,
    enabled: !!upscale?.enabled && !pip.active,
    mode: upscale?.mode ?? 'A',
    tier: upscale?.tier ?? 'full',
    onSlow: upscale?.onSlow,
    source: playlist,
  })
  const painted = useFreshFrames(video, upscaleState === 'on')
  const showUpscaled = upscaleState === 'on' && painted

  const trackList = (stream?.subtitles ?? [])
    .map((s) => `${s.index}:${s.language ?? ''}:${s.title ?? ''}`)
    .join('|')
  const audioLanguage = stream?.audio?.[audioTrack]?.language ?? ''
  const [chosenFor, setChosenFor] = useState<string>()
  useEffect(() => {
    const tracks = [...(stream?.subtitles ?? [])].sort((a, b) => Number(!!a.pictures) - Number(!!b.pictures))
    setTrack(tracks.length ? (chooseWithDub(tracks, subLanguages ?? ['en'], audioLanguage, withDub)?.index ?? null) : null)
    setChosenFor(trackList)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [trackList, audioLanguage, withDub])

  const [subtitleWaitOver, setSubtitleWaitOver] = useState<string>()
  useEffect(() => {
    if (!stream?.id) return
    const id = stream.id
    const timer = window.setTimeout(() => setSubtitleWaitOver(id), SUBTITLE_WAIT)
    return () => clearTimeout(timer)
  }, [stream?.id])
  const [subtitlesFor, setSubtitlesFor] = useState<string>()
  const subtitlesForRef = useRef(subtitlesFor)
  subtitlesForRef.current = subtitlesFor
  const subtitlesDrawable = chosenFor === trackList && subtitlesReady
  useEffect(() => {
    if (stream?.id && (subtitlesDrawable || subtitleWaitOver === stream.id)) setSubtitlesFor(stream.id)
  }, [stream?.id, subtitlesDrawable, subtitleWaitOver])
  const subtitlesSettled = !!stream?.id && subtitlesFor === stream.id
  const playedByHand = useRef<string | undefined>(undefined)
  useEffect(() => {
    if (!video || !stream?.id) return
    const id = stream.id
    const onPlay = () => {
      if (subtitlesForRef.current === id) return
      playedByHand.current = id
      setSubtitlesFor(id)
    }
    video.addEventListener('play', onPlay)
    return () => video.removeEventListener('play', onPlay)
  }, [video, stream?.id])

  const autoPlayRef = useRef(autoPlay)
  autoPlayRef.current = autoPlay
  const holdingStart = autoPlay && !!stream?.id && !subtitlesSettled
  useEffect(() => {
    if (!video || !stream?.id || !autoPlayRef.current || !subtitlesSettled || playedByHand.current === stream.id) return
    const id = stream.id
    const start = () => {
      if (castingRef.current) return
      void video.play().catch((e: unknown) => {
        if (e instanceof DOMException && e.name === 'NotAllowedError') setFramedFor(id)
      })
    }
    start()
    if (video.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA) return
    video.addEventListener('canplay', start, { once: true })
    return () => video.removeEventListener('canplay', start)
  }, [video, stream?.id, subtitlesSettled])

  const reportRef = useRef(onProgress)
  reportRef.current = onProgress
  const endedRef = useRef(onEnded)
  endedRef.current = onEnded
  const placeRef = useRef<((at: number, duration: number) => void) | null>(null)

  useEffect(() => {
    if (!video || !stream?.id) return
    const reporter = reportRef.current

    let played = 0
    let last = video.currentTime
    let position = video.currentTime
    let duration = video.duration
    const onTime = () => {
      const now = video.currentTime
      const step = now - last
      if (step > 0 && step < 2) played += step
      last = now
      if (video.duration > 0) {
        position = now
        duration = video.duration
      }
    }
    const onSeeking = () => {
      last = video.currentTime
    }

    const report = (at: number, final = false) => {
      reporter(at, duration, played, final, video.paused)
      played = 0
    }
    placeRef.current = (at, d) => {
      position = last = at
      if (d > 0) duration = d
    }
    const id = window.setInterval(() => {
      if (!video.paused && duration > 0) report(position)
    }, 10_000)

    const flush = () => {
      if (duration > 0) report(position)
    }
    const ended = () => {
      if (duration > 0) report(duration, true)
      endedRef.current()
    }
    video.addEventListener('timeupdate', onTime)
    video.addEventListener('seeking', onSeeking)
    video.addEventListener('pause', flush)
    video.addEventListener('play', flush)
    video.addEventListener('ended', ended)
    window.addEventListener('pagehide', flush)
    return () => {
      window.clearInterval(id)
      video.removeEventListener('timeupdate', onTime)
      video.removeEventListener('seeking', onSeeking)
      video.removeEventListener('pause', flush)
      video.removeEventListener('play', flush)
      video.removeEventListener('ended', ended)
      window.removeEventListener('pagehide', flush)
      placeRef.current = null
      if (duration > 0) report(position, true)
    }
  }, [video, stream?.id])

  const { phase: castPhase, sdkState: castSdk, device: castDevice, remote, playing: castPlaying, start: startCast,
    connect: connectCast, stop: stopCast, playPause: castPlayPause, seek: castSeek } = useCast()
  const casting = castPhase === 'connected' && !!stream?.id && castPlaying === episodeKey(stream.id)
  const castingRef = useRef(casting)
  castingRef.current = casting
  const castTo = useCallback(async () => {
    if (!stream?.id || !video) return
    try {
      const chosen = stream.subtitles.find((t) => t.index === track)
      const params = new URLSearchParams()
      if (chosen && (castBurn || chosen.pictures)) {
        params.set('burn', String(track))
        params.set('delay', String(subDelay))
      }
      if (chosen?.language) params.set('sub', chosen.language)
      const link = await api.post<{
        url: string
        subtitles: Array<{ index: number; language?: string; title?: string; url: string }>
        note?: string
        queue?: QueuedMedia[]
      }>(`/api/stream/${stream.id}/cast${params.size ? `?${params}` : ''}`)
      if (link.note) showNotice(link.note, 6000)
      else if (track != null && !chosen) showNotice("The TV shows the episode's own subtitles; loaded ones stay here", 6000)
      const shown = track == null ? -1 : link.subtitles.findIndex((s) => s.index === track)
      video.pause()
      await startCast({
        key: episodeKey(stream.id), url: link.url, title: title ?? '', subtitle, artwork, startAt: video.currentTime,
        subtitles: link.subtitles, subtitleTrack: shown >= 0 ? shown : undefined,
        textStyle: castStyle(lookPreset, subScale),
        queue: (link.queue ?? []).map((q) => ({ ...q, subtitleTrack: q.subtitles.length ? 0 : undefined })),
      })
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e)
      if (!/cancel/i.test(message)) showNotice(`Couldn't cast: ${message}`, 6000)
    }
  }, [stream, video, track, title, subtitle, artwork, startCast, showNotice, castBurn, subDelay, lookPreset, subScale])
  const castToRef = useRef(castTo)
  castToRef.current = castTo
  const tvMovedOn = useRef(onTVMovedOn)
  tvMovedOn.current = onTVMovedOn
  const lastOnTV = useRef(castPlaying)
  useEffect(() => {
    const was = lastOnTV.current
    lastOnTV.current = castPlaying
    if (!stream?.id || !castPlaying || castPlaying === was || was !== episodeKey(stream.id)) return
    const on = /^(-?\d+)-(\d+)$/.exec(castPlaying)
    if (on) tvMovedOn.current?.(Number(on[1]), Number(on[2]))
  }, [castPlaying, stream?.id])
  const chooseTV = useCallback(async () => {
    setNotice(null)
    try {
      if (!(await connectCast())) return
    } catch (e) {
      showNotice(`Couldn't cast: ${e instanceof Error ? e.message : String(e)}`, 6000)
      return
    }
    await castToRef.current()
  }, [connectCast, showNotice])
  useEffect(() => {
    if (casting) video?.pause()
  }, [casting, video])
  const remoteRef = useRef(remote)
  if (casting && remote.key === castPlaying) remoteRef.current = remote
  useEffect(() => {
    if (!casting || !stream?.id) return
    const reporter = reportRef.current
    const sid = stream.id
    let played = 0
    let last = remoteRef.current.time
    const id = window.setInterval(() => {
      const r = remoteRef.current
      const step = r.time - last
      if (step > 0 && step < 15) played += step
      last = r.time
      if (r.duration > 0 && !r.paused) {
        reporter(r.time, r.duration, played, false, false)
        played = 0
      }
    }, 10_000)
    return () => {
      window.clearInterval(id)
      const r = remoteRef.current
      if (video && r.time > 0 && liveId.current === sid) {
        video.currentTime = r.time
        placeRef.current?.(r.time, r.duration)
      }
      if (r.duration > 0) reporter(r.time, r.duration, played, false, true)
    }
  }, [casting, stream?.id, video])

  const stuckRef = useRef(onEndStuck)
  stuckRef.current = onEndStuck
  const planned = stream?.duration
  const copied = stream?.plan?.videoCopy
  useEffect(() => {
    if (!video || !stream?.id) return
    let last = -1
    let still = 0
    let told = false
    const id = window.setInterval(() => {
      const t = video.currentTime
      const d = video.duration
      if (told || video.ended || video.paused || !(d > 0) || d - t > 8 || Math.abs(t - last) > 0.05) {
        last = t
        still = 0
        return
      }
      if (++still < 5) return
      told = true
      let bufferedEnd = 0
      for (let i = 0; i < video.buffered.length; i++) bufferedEnd = Math.max(bufferedEnd, video.buffered.end(i))
      const assumed = streamEnded.current && d - t < 2 && bufferedEnd - t < 0.5
      stuckRef.current?.({
        t: t.toFixed(2),
        duration: d.toFixed(2),
        planned: planned?.toFixed(2),
        bufferedEnd: bufferedEnd.toFixed(2),
        ready: video.readyState,
        network: video.networkState,
        seeking: video.seeking,
        copied,
        eos: streamEnded.current,
        assumed,
      })
      if (assumed) {
        video.pause()
        video.dispatchEvent(new Event('ended'))
      }
    }, 1000)
    return () => window.clearInterval(id)
  }, [video, stream?.id, planned, copied, streamEnded])

  const togglePlay = useCallback(() => {
    if (!video) return
    if (video.paused) void video.play().catch(() => {})
    else video.pause()
  }, [video])

  const seekBy = useCallback(
    (delta: number) => {
      if (!video) return
      video.currentTime = Math.max(0, Math.min(video.duration || 0, video.currentTime + delta))
    },
    [video],
  )

  const onNextRef = useRef(onNext)
  onNextRef.current = onNext
  const hasNext = !!onNext
  const goNext = useCallback(() => onNextRef.current?.('key'), [])
  const onPreviousRef = useRef(onPrevious)
  onPreviousRef.current = onPrevious
  const hasPrevious = !!onPrevious
  const goPrevious = useCallback(() => onPreviousRef.current?.(), [])
  useEffect(() => {
    if (!video || !('mediaSession' in navigator)) return
    const session = navigator.mediaSession
    session.metadata = new MediaMetadata({
      title: subtitle ?? title,
      artist: subtitle ? title : '',
      artwork: artwork ? [{ src: artwork }] : [],
    })
    const handlers: Array<[MediaSessionAction, MediaSessionActionHandler | null]> = [
      ['play', () => void video.play().catch(() => {})],
      ['pause', () => video.pause()],
      ['seekbackward', (d) => seekBy(-(d.seekOffset ?? 10))],
      ['seekforward', (d) => seekBy(d.seekOffset ?? 10)],
      ['seekto', (d) => d.seekTime !== undefined && (video.currentTime = d.seekTime)],
      ['nexttrack', hasNext ? () => onNextRef.current?.('media') : null],
      ['previoustrack', hasPrevious ? () => onPreviousRef.current?.() : null],
    ]
    for (const [action, handler] of handlers) {
      try {
        session.setActionHandler(action, handler && ((d) => (noteInput(), handler(d))))
      } catch {
      }
    }
    return () => {
      for (const [action] of handlers) {
        try {
          session.setActionHandler(action, null)
        } catch {
        }
      }
      session.metadata = null
    }
  }, [video, title, subtitle, artwork, seekBy, hasNext, hasPrevious])

  useEffect(() => {
    if (!video || !('mediaSession' in navigator) || !navigator.mediaSession.setPositionState) return
    const update = () => {
      const d = video.duration
      if (!Number.isFinite(d) || d <= 0) return
      try {
        navigator.mediaSession.setPositionState({ duration: d, position: Math.min(video.currentTime, d), playbackRate: video.playbackRate || 1 })
      } catch {
      }
    }
    const events = ['loadedmetadata', 'durationchange', 'seeked', 'ratechange', 'play', 'pause']
    for (const e of events) video.addEventListener(e, update)
    return () => {
      for (const e of events) video.removeEventListener(e, update)
    }
  }, [video])

  useEffect(() => {
    if ('mediaSession' in navigator) navigator.mediaSession.playbackState = playing ? 'playing' : 'paused'
  }, [playing])

  const sleep = useSleep()
  useEffect(() => {
    if (sleep.kind === 'fired' && video && !video.paused) video.pause()
  }, [sleep, video])

  useEffect(() => {
    if (!playing || !('wakeLock' in navigator)) return
    let lock: WakeLockSentinel | null = null
    let gone = false
    const take = () => {
      if (document.visibilityState !== 'visible' || (lock && !lock.released)) return
      navigator.wakeLock.request('screen').then(
        (l) => (gone ? void l.release() : (lock = l)),
        () => {
        },
      )
    }
    take()
    document.addEventListener('visibilitychange', take)
    return () => {
      gone = true
      document.removeEventListener('visibilitychange', take)
      void lock?.release()
    }
  }, [playing])

  const seekTimer = useRef<number>(0)
  const markSeeking = useCallback((active: boolean) => {
    window.clearTimeout(seekTimer.current)
    if (!active) {
      setWaiting(false)
      return
    }
    seekTimer.current = window.setTimeout(() => setWaiting(true), 150)
  }, [])
  useEffect(() => () => window.clearTimeout(seekTimer.current), [])

  const lastTap = useRef<{ at: number; x: number }>({ at: 0, x: 0 })
  const touchedAt = useRef(0)
  const controlsShown = useRef(controlsVisible)
  controlsShown.current = controlsVisible
  const hiddenAtTouch = useRef(false)
  const [flash, setFlash] = useState<{ dir: 'back' | 'forward'; total: number } | null>(null)
  const run = useRef<{ dir: 'back' | 'forward'; total: number; at: number } | null>(null)
  const flashTimer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(flashTimer.current), [])
  const pendingTap = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(pendingTap.current), [])
  const onSurfaceTouch = useCallback(
    (e: React.PointerEvent<HTMLElement>) => {
      if (e.pointerType !== 'touch') return
      const now = performance.now()
      touchedAt.current = now
      const rect = e.currentTarget.getBoundingClientRect()
      const x = (e.clientX - rect.left) / rect.width
      const side = x < 0.35 || x > 0.65
      const dir = x < 0.35 ? 'back' : 'forward'
      const prev = lastTap.current
      lastTap.current = { at: now, x }
      const going = run.current
      const more = !!going && side && going.dir === dir && now - going.at < RUN_MS
      if (more || (now - prev.at < 300 && Math.abs(prev.x - x) < 0.2 && side)) {
        window.clearTimeout(pendingTap.current)
        seekBy(dir === 'back' ? -10 : 10)
        const total = more ? going.total + 10 : 10
        run.current = { dir, total, at: now }
        setFlash({ dir, total })
        window.clearTimeout(flashTimer.current)
        flashTimer.current = window.setTimeout(() => {
          setFlash(null)
          run.current = null
        }, RUN_MS)
        lastTap.current = { at: 0, x }
        return
      }
      if (hiddenAtTouch.current) return
      if (!side) {
        togglePlay()
        return
      }
      window.clearTimeout(pendingTap.current)
      pendingTap.current = window.setTimeout(togglePlay, 300)
    },
    [togglePlay, seekBy],
  )
  const onSurfaceClick = useCallback(() => {
    if (performance.now() - touchedAt.current < 700) return
    togglePlay()
  }, [togglePlay])

  const hideTimer = useRef<number | undefined>(undefined)
  const held = useRef(new Set<string>())
  const nudge = useCallback(() => {
    setControlsVisible(true)
    window.clearTimeout(hideTimer.current)
    hideTimer.current = window.setTimeout(() => {
      if (!video?.paused && held.current.size === 0) setControlsVisible(false)
    }, 2600)
  }, [video])
  const holdControls = useCallback(
    (why: string, on: boolean) => {
      if (on) held.current.add(why)
      else held.current.delete(why)
      if (!on) nudge()
    },
    [nudge],
  )
  const holdForCast = useCallback((on: boolean) => holdControls('cast', on), [holdControls])
  const holdForMenu = useCallback((on: boolean) => holdControls('menu', on), [holdControls])

  const partNow = skips.find((r) => time >= r.start && time < r.end)
  const offOp = !!skipOff?.op
  const offEd = !!skipOff?.ed
  const activeSkip = skips.find((r) => {
    if (time < r.start || time >= r.end) return false
    const k = skipKind(r.kind)
    return !(k === 'opening' && offOp) && !(k === 'ending' && offEd)
  })
  const offered = autoSkipped.skipped
  const canCallWrong = !!onSkipVote && offered?.range.source === 'aniskip' && !!offered.range.id
  const noteSkip = autoSkipped.note
  const skipActive = useCallback(() => {
    if (!video || !activeSkip) return
    video.currentTime = activeSkip.end
    noteSkip(activeSkip)
  }, [video, activeSkip, noteSkip])

  const stepRate = useCallback(
    (by: number) => {
      const at = SPEEDS.indexOf(rate)
      const next = SPEEDS[Math.min(SPEEDS.length - 1, Math.max(0, (at < 0 ? SPEEDS.indexOf(1) : at) + by))]
      changeRate(next)
      showNotice(`Speed ${next === 1 ? 'normal' : `${next}×`}`)
    },
    [rate, changeRate, showNotice],
  )
  const stepFrame = useCallback(
    (by: number) => {
      if (!video) return
      if (!video.paused) video.pause()
      video.currentTime = Math.max(0, video.currentTime + by / 23.976)
    },
    [video],
  )
  const cycleSubtitles = useCallback(() => {
    const tracks = subtitleTracks
    if (tracks.length === 0) return
    const at = tracks.findIndex((t) => t.index === track)
    const next = at + 1 < tracks.length ? tracks[at + 1] : null
    setTrack(next ? next.index : null)
    showNotice(next ? `Subtitles: ${subtitleLabel(tracks, next)}` : 'Subtitles off')
  }, [subtitleTracks, track, showNotice])
  const [infoOpen, setInfoOpen] = useState(false)
  const toggleInfo = useCallback(() => setInfoOpen((v) => !v), [])
  useKeymap()
  const shownTrack = subtitleTracks.find((t) => t.index === track)
  const subtitlesInfo = shownTrack
    ? [
        subtitleLabel(subtitleTracks, shownTrack),
        shownTrack.pictures ? 'pictures (PGS)' : subLookLabel(lookPreset),
        subDelay ? `${subDelay > 0 ? '+' : ''}${subDelay.toFixed(1)} s` : '',
      ]
        .filter(Boolean)
        .join(' · ')
    : 'Off'
  const anime4kInfo = !upscale?.enabled
    ? 'Off'
    : {
        off: 'Off',
        starting: 'Starting',
        on: `On · ${upscale.tier ?? 'full'} · mode ${upscale.mode ?? 'A'}`,
        unsupported: 'Needs WebGPU, which this browser lacks',
        failed: 'Stopped, the graphics device was lost',
      }[upscaleState]
  const holdFast = useCallback(
    (on: boolean) => {
      if (!video || fast.current === on) return
      fast.current = on
      video.playbackRate = on ? 2 : rate
      if (on) showNotice('2× while held', 60_000)
      else setNotice(null)
    },
    [video, rate, showNotice],
  )

  const [fullscreen, setFullscreen] = useState(false)
  useEffect(() => {
    const changed = () => setFullscreen(!!document.fullscreenElement)
    document.addEventListener('fullscreenchange', changed)
    return () => document.removeEventListener('fullscreenchange', changed)
  }, [])
  const wide = useMediaQuery('(min-width: 40rem)')
  const touch = useMediaQuery('(pointer: coarse)')
  const sideways = useCallback(() => {
    if (!window.matchMedia('(pointer: coarse)').matches || readKey(SIDEWAYS_KEY)) return
    writeKey(SIDEWAYS_KEY, '1')
    showNotice('Turn your phone sideways', 3000)
  }, [showNotice])
  const goFullscreen = useCallback(() => toggleFullscreen(shell.current, video, sideways), [video, sideways])
  const swipes = fullscreen || (!wide && !!onMinimize && !mini)
  const [fill, setFill] = useState(false)
  const scrubFrom = useRef<number | null>(null)
  const gestures = useTouchGestures(
    {
      hold: holdFast,
      scrub: (seconds, done) => {
        if (!video || !(video.duration > 1)) return
        scrubFrom.current ??= video.currentTime
        const to = Math.min(Math.max(0, scrubFrom.current + seconds), (video.duration || 0) - 1)
        if (done) {
          if (seconds !== 0) video.currentTime = to
          scrubFrom.current = null
          return
        }
        showNotice(`${seconds < 0 ? '−' : '+'}${Math.abs(Math.round(seconds))} s · ${clockTime(to)}`, 1500)
      },
      pinch: (spread) => {
        setFill(spread)
        showNotice(spread ? 'Filling the screen' : 'Whole picture')
      },
      swipe: (down) => {
        if (fullscreen) {
          if (down) goFullscreen()
        } else if (down) onMinimize?.()
        else goFullscreen()
      },
    },
    { swipes, secondsAcross: 120 },
  )
  const onSurfacePointerUp = (e: React.PointerEvent<HTMLElement>) => {
    gestures.handlers.onPointerUp(e)
    if (e.pointerType === 'touch' && gestures.used()) {
      touchedAt.current = performance.now()
      return
    }
    onSurfaceTouch(e)
  }
  const surface = {
    onPointerDown: gestures.handlers.onPointerDown,
    onPointerMove: gestures.handlers.onPointerMove,
    onPointerCancel: gestures.handlers.onPointerCancel,
    onPointerUp: onSurfacePointerUp,
    onClick: onSurfaceClick,
    onContextMenu: (e: React.MouseEvent) => gestures.touching() && e.preventDefault(),
    style: { touchAction: swipes ? 'none' : 'pan-y' },
  } as const

  const [marking, setMarking] = useState<(MarkFrom & { marks: unknown }) | null>(null)
  const markFrom = marking?.marks === skipMarks ? marking : null
  const setMarkFrom = useCallback(
    (next: MarkFrom | null) => setMarking(next && { ...next, marks: skipMarks }),
    [skipMarks],
  )
  const saveMark = useCallback(async () => {
    if (!video || !markFrom || !skipMarks) return ''
    const saved = await skipMarks.mark(markFrom.kind, markFrom.at, video.currentTime)
    setMarking(null)
    return markSaved(markFrom.kind, saved)
  }, [video, markFrom, skipMarks])
  const markingMenu = useMemo<Marking | undefined>(
    () => skipMarks && { from: markFrom, setFrom: setMarkFrom, save: saveMark, forget: skipMarks.forget },
    [skipMarks, markFrom, setMarkFrom, saveMark],
  )
  const markKey = useCallback(
    (edge: 'start' | 'end') => {
      if (!video || !Number.isFinite(video.duration)) return
      if (edge === 'start') {
        const kind = video.currentTime < video.duration / 2 ? 'op' : 'ed'
        setMarkFrom({ kind, at: video.currentTime })
        showNotice(`${partName(kind)} starts at ${clockTime(video.currentTime)} · ${keyHint('markEnd')} where it ends`, 4000)
      } else if (!markFrom) {
        showNotice(`${keyHint('markStart')} first, where the opening or ending starts`, 3000)
      } else {
        showNotice('Saving…', 10_000)
        saveMark().then(
          (said) => showNotice(said, 5000),
          (err: Error) => showNotice(`Couldn't save it: ${err.message}`, 5000),
        )
      }
    },
    [video, markFrom, setMarkFrom, saveMark, showNotice],
  )

  const [avOpen, setAvOpen] = useState(false)
  useEffect(() => {
    holdControls('av', avOpen)
  }, [avOpen, holdControls])
  useEffect(() => {
    if (mini) setAvOpen(false)
  }, [mini])
  const openSettings = useCallback(
    (open: boolean) => {
      setAvOpen(open)
      if (!open) return
      nudge()
      shell.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    },
    [nudge],
  )
  useImperativeHandle(controls, () => ({ fullscreen: goFullscreen, settings: openSettings }), [goFullscreen, openSettings])

  const [framedFor, setFramedFor] = useState<string>()
  const firstLoad = !!stream && framedFor !== stream.id

  const audioName = languageName(stream?.audio?.[audioTrack]?.language)
  const subtitleName = shownTrack ? (languageName(shownTrack.language) ?? shownTrack.title ?? 'Chosen') : undefined
  const subtitleFact = !stream ? undefined : subtitleName ?? (subtitleTracks.length ? 'off' : imageSubtitles ? undefined : 'none')
  const factsRef = useRef(onFacts)
  factsRef.current = onFacts
  useEffect(() => {
    factsRef.current?.({ audio: audioName, subtitles: subtitleFact })
  }, [audioName, subtitleFact])

  useKeyboard({
    off: !!mini,
    video,
    togglePlay,
    seekBy,
    nudge,
    shell: shell.current,
    extra: pip.window,
    showVolume,
    shiftSubDelay: track === null ? undefined : shiftSubDelay,
    skip: activeSkip ? skipActive : undefined,
    cycleVoice: (stream?.audio?.length ?? 0) > 0 ? cycleVoice : undefined,
    next: hasNext ? goNext : undefined,
    previous: hasPrevious ? goPrevious : undefined,
    stepRate,
    stepFrame,
    cycleSubtitles: subtitleTracks.length > 0 ? cycleSubtitles : undefined,
    toggleInfo,
    holdFast,
    mark: skipMarks ? markKey : undefined,
    sizeSubs: track === null ? undefined : stepSubSize,
    theatre: onTheatre,
  })

  const [stalledFor, setStalledFor] = useState(0)
  useEffect(() => setStalledFor(0), [stream?.id])

  const [health, setHealth] = useState<StreamHealth | null>(null)
  const unit = useSpeedUnit()
  const [queued, setQueued] = useState(false)
  useEffect(() => setQueued(false), [stream?.id])
  const stalledLong = stalledFor > 12
  useEffect(() => {
    const id = stream?.id
    if (!id || !stalledLong) {
      setHealth(null)
      return
    }
    let stop = false
    const poll = async () => {
      if (video?.paused || document.visibilityState !== 'visible') return
      return api
        .get<StreamHealth>(`/api/stream/${id}/health`)
        .then((h) => {
          if (!stop) setHealth(h)
        })
        .catch(() => {})
    }
    void poll()
    const timer = window.setInterval(() => void poll(), 5000)
    return () => {
      stop = true
      window.clearInterval(timer)
    }
  }, [stream?.id, stalledLong, video])

  const noticeText = notice === VOLUME_NOTICE ? (muted ? 'Muted' : `Volume ${Math.round(volume * 100)}%`) : (notice ?? '')
  const tryAgain = useRef<HTMLButtonElement>(null)
  const failed = !!(error || lostError)
  useEffect(() => {
    const doc = shell.current?.ownerDocument
    const at = doc?.activeElement
    if (failed && (!at || at === doc.body || shell.current?.contains(at))) tryAgain.current?.focus()
  }, [failed])

  const downloadInstead = async () => {
    const [animeId, episode] = (stream?.id ?? '').split('-').map(Number)
    if (!animeId || !episode) return
    try {
      await api.post('/api/download', { animeId, episode })
      refreshDownloads(qc)
      setQueued(true)
      setQueueError(null)
    } catch (err) {
      setQueueError(err instanceof Error ? err.message : 'Could not queue it')
    }
  }
  const [queueError, setQueueError] = useState<string | null>(null)
  useEffect(() => {
    if (!waiting && stream) {
      setStalledFor(0)
      return
    }
    const id = window.setInterval(() => setStalledFor((s) => (stream && video?.paused ? s : s + 1)), 1000)
    return () => window.clearInterval(id)
  }, [waiting, stream, video])

  const corners = pip.active || theatre ? '' : 'sm:rounded-xl'
  const audioMenu =
    !pip.active && !saved && stream?.audio && stream.audio.length > 1
      ? { tracks: stream.audio, value: audioTrack, onChange: switchAudio }
      : undefined
  const soundMenu =
    !pip.active && stream && !saved && (stream.audio?.length ?? 0) > 0 ? { value: soundState, onChange: switchSound } : undefined
  const startingNow = (!stream || firstLoad) && !error && !lostError && !casting
  const stepsShown: Starting | null = !startingNow
    ? null
    : !stream
      ? (starting ?? { step: 0, status: 'Finding a release.', seconds: 0 })
      : {
          step: 2,
          seconds: stalledFor,
          status:
            stalledFor > 12 && health && !health.complete
              ? health.peers > 0
                ? `Loading the first seconds: ${speed(health.mbps, unit)} from ${health.peers} ${health.peers === 1 ? 'person' : 'people'}.`
                : 'Loading the first seconds. No one sharing this release is sending right now.'
              : 'Loading the first seconds of the episode.',
        }
  const slowOut =
    (health?.slow && stalledFor > 20) || queueError ? (
      <>
        {health?.slow &&
          stalledFor > 20 &&
          (queued ? (
            <Link to="/downloads" className={cx(buttonClass('secondary', 'sm'), 'pointer-events-auto mt-3 pointer-coarse:min-h-11')}>
              Queued · open Downloads
            </Link>
          ) : (
            <button
              onClick={() => void downloadInstead()}
              className={cx(buttonClass('secondary', 'sm'), 'pointer-events-auto mt-3 pointer-coarse:min-h-11')}
            >
              Download it instead and watch later
            </button>
          ))}
        {queueError && <p className="mt-2 text-xs text-recap">{queueError}</p>}
      </>
    ) : null
  const minimizable = !!onMinimize && !wide && !fullscreen && !pip.active && !mini
  const avButton = (
    <AudioSubtitlesPanel
      open={avOpen}
      onOpen={setAvOpen}
      label={wide || pip.active}
      win={pip.window ?? undefined}
      floating={pip.active}
      audioChoice={pip.active ? undefined : settings?.audio}
      preferred={subLanguages}
      audio={audioMenu}
      tracks={stream ? subtitleTracks : undefined}
      value={track}
      onChange={setTrack}
      scale={subScale}
      onScale={changeSubScale}
      delay={subDelay}
      onDelay={setSubDelay}
      keptDelay={keptDelay}
      onKeepDelay={onKeepDelay}
      look={onSubLook && { value: lookPreset, onChange: onSubLook, raised: subRaised, onRaised: onSubRaised }}
      onLoadFile={(file) => void loadOwnFile(file)}
      jimaku={
        stream && jimakuSet && !saved
          ? {
              streamId: stream.id,
              onPick: (f) => addTrack({ index: JIMAKU_TRACK - f.index, title: `Jimaku · ${f.name}`, url: f.url, whole: true }),
            }
          : undefined
      }
    />
  )
  const settingsMenu = (
    <PlayerMenu
      rate={rate}
      onRate={changeRate}
      sleep={sleep}
      skips={skips}
      video={video}
      marking={markingMenu}
      sound={soundMenu}
      playback={pip.active ? undefined : settings?.playback}
      onOpen={holdForMenu}
      onInfo={toggleInfo}
      more={!wide}
    />
  )

  const body = (
    <div
      ref={shell}
      tabIndex={-1}
      role="region"
      aria-label="Video player"
      onMouseMove={nudge}
      onPointerDownCapture={(e) => {
        if (e.pointerType === 'touch') hiddenAtTouch.current = !controlsShown.current
      }}
      onTouchStart={nudge}
      onFocusCapture={nudge}
      className={cx(
        'group/player relative w-full bg-black outline-none select-none',
        pip.active ? 'h-full' : 'aspect-video',
        corners,
        !controlsVisible && 'cursor-none',
      )}
    >
      {upscale?.enabled && (
        <canvas
          ref={setCanvas}
          {...surface}
          className={cx(
            'absolute inset-0 size-full',
            fill ? 'object-cover' : 'object-contain',
            corners,
            showUpscaled ? 'opacity-100' : 'pointer-events-none opacity-0',
          )}
        />
      )}

      <video
        ref={setVideo}
        playsInline
        autoPlay={autoPlay && subtitlesSettled && !casting}
        className={cx('size-full', fill && 'object-cover', corners)}
        {...surface}
        onPlay={() => {
          setPlaying(true)
          if (getSleep().kind === 'fired') setSleep({ kind: 'off' })
          onResume?.('play')
          nudge()
        }}
        onPause={() => {
          setPlaying(false)
          setControlsVisible(true)
        }}
        onEmptied={(e) => setPlaying(!e.currentTarget.paused)}
        onWaiting={() => setWaiting(true)}
        onLoadedData={() => setFramedFor(stream?.id)}
        onLoadedMetadata={(e) => {
          if (!autoPlayRef.current && e.currentTarget.paused) setFramedFor(stream?.id)
        }}
        onPlaying={() => {
          setWaiting(false)
          setFramedFor(stream?.id)
        }}
        onCanPlay={() => setWaiting(false)}
        onSeeking={() => {
          markSeeking(true)
          onResume?.('seek')
        }}
        onSeeked={() => markSeeking(false)}
        onTimeUpdate={(e) => setTime(e.currentTarget.currentTime)}
        onDurationChange={(e) => setDuration(e.currentTarget.duration)}
        onVolumeChange={(e) => {
          setVolume(e.currentTarget.volume)
          setMuted(e.currentTarget.muted)
          savePersisted({ volume: e.currentTarget.volume, muted: e.currentTarget.muted })
        }}
      />

      {notice && (
        <div
          aria-hidden
          className="pointer-events-none absolute top-6 left-1/2 -translate-x-1/2 rounded-full bg-base-950/75 px-3 py-1 text-sm font-medium text-white tabular-nums backdrop-blur-sm"
        >
          {noticeText}
        </div>
      )}
      <p role="status" className="sr-only">
        {noticeText}
      </p>
      <p role="status" className="sr-only">
        {activeSkip && !casting ? `${skipLabel(activeSkip.kind)} · ${keyHint('skip')} skips it` : ''}
      </p>
      <p role="status" className="sr-only">
        {stalledLong ? 'Buffering' : ''}
      </p>

      {stepsShown && !mini ? (
        <StartingSteps
          starting={stepsShown}
          backdrop={artwork}
          onPick={onPickRelease}
          onCancel={onStartCancel}
          onRetry={onStartRetry}
          className={corners}
        >
          {slowOut}
        </StartingSteps>
      ) : (
        (waiting || !stream || holdingStart) &&
        !error &&
        !lostError && (
          <div className="pointer-events-none absolute inset-0 grid place-items-center">
            <div className="text-center">
              <Spinner className="mx-auto size-8" />
              {stream && holdingStart && !waiting && <p className="mt-3 text-sm text-base-300">Loading subtitles…</p>}
              {stream && stalledFor > 12 && (
                <p className="mt-3 max-w-xs text-xs text-base-300">
                  {!health || health.complete
                    ? 'Buffering…'
                    : health.peers > 0
                      ? `Buffering: ${speed(health.mbps, unit)} from ${health.peers} ${health.peers === 1 ? 'person' : 'people'}`
                      : 'Buffering: no one sharing this release is sending right now'}
                </p>
              )}
              {slowOut}
            </div>
          </div>
        )
      )}

      {(error || lostError) && (
        <div role="alert" className={cx('absolute inset-0 grid place-items-center bg-base-950/80 p-6 text-center', corners)}>
          <div>
            <p className="text-sm text-base-200">{error ?? lostError}</p>
            <div className="mt-3 flex flex-wrap justify-center gap-2">
              {!(error && final) && (
                <button
                  ref={tryAgain}
                  onClick={() => {
                    if (error) {
                      setResumeAt(video?.currentTime || startAt)
                      setWaiting(true)
                      resumeAfterRebuild(video, true)
                      setEpoch((e) => e + 1)
                      return
                    }
                    setLostError(null)
                    recover()
                  }}
                  className={cx(buttonClass('primary', 'sm'), 'pointer-events-auto pointer-coarse:min-h-11')}
                >
                  Retry
                </button>
              )}
              {error && onPickRelease && (
                <button onClick={onPickRelease} className={cx(buttonClass('secondary', 'sm'), 'pointer-events-auto pointer-coarse:min-h-11')}>
                  Try another release
                </button>
              )}
            </div>
          </div>
        </div>
      )}

      {flash && (
        <div
          aria-hidden
          className={cx(
            'pointer-events-none absolute inset-y-0 z-10 grid w-[38%] place-items-center bg-white/10',
            flash.dir === 'back' ? 'left-0 rounded-r-[50%]' : 'right-0 rounded-l-[50%]',
          )}
        >
          <span className="rounded-full bg-base-950/70 px-4 py-2 text-sm font-semibold text-white tabular-nums">
            {flash.dir === 'back' ? '−' : '+'}
            {flash.total} s
          </span>
        </div>
      )}

      {sleep.kind === 'fired' && !playing && !video?.ended && (
        <div className="pointer-events-none absolute top-4 left-1/2 -translate-x-1/2 rounded-full bg-base-950/80 px-3 py-1.5 text-xs text-white ring-1 ring-white/15">
          Paused by the sleep timer
        </div>
      )}

      {!mini && offered && !activeSkip && (
        <div className="absolute right-3 bottom-[3.75rem] z-10 flex items-center gap-1 rounded-xl bg-base-950/85 p-1 text-sm text-white ring-1 ring-white/20 backdrop-blur-sm sm:right-4 sm:bottom-24">
          <span className="px-2 text-white/80">Skipped {skipKind(offered.range.kind) ?? 'ahead'}</span>
          <button
            onClick={() => {
              if (Date.now() - offered.at < SKIPPED_GUARD) return
              if (video) video.currentTime = offered.range.start
              autoSkipped.dismiss()
            }}
            className="min-h-9 rounded-lg px-3 font-semibold hover:bg-white/15 pointer-coarse:min-h-11"
          >
            Undo
          </button>
          {canCallWrong && (
            <button
              onClick={() => {
                if (Date.now() - offered.at < SKIPPED_GUARD) return
                if (video) video.currentTime = offered.range.start
                onSkipVote!(offered.range.id!, false)
                autoSkipped.dismiss()
              }}
              className="min-h-9 rounded-lg px-3 text-white/80 hover:bg-white/15 pointer-coarse:min-h-11"
            >
              Wrong skip
            </button>
          )}
        </div>
      )}

      {!mini && activeSkip && !startingNow && (
        <button
          onClick={skipActive}
          className="absolute right-3 bottom-[3.75rem] z-10 flex min-h-11 items-center overflow-hidden rounded-xl bg-base-950/85 px-4 text-sm font-semibold text-white ring-1 ring-white/20 backdrop-blur-sm transition-transform hover:scale-105 sm:right-4 sm:bottom-24 sm:pointer-coarse:min-h-12"
        >
          Skip {skipKind(activeSkip.kind) ?? 'ahead'}
          <kbd className="ml-2 font-sans text-xs text-white/50 max-sm:hidden">{keyHint('skip')}</kbd>
          <span
            aria-hidden
            className="absolute inset-x-0 bottom-0 h-0.5 origin-left bg-white/70 transition-transform duration-300 ease-linear"
            style={{
              transform: `scaleX(${Math.max(0, Math.min(1, (activeSkip.end - time) / Math.max(1, activeSkip.end - activeSkip.start)))})`,
            }}
          />
        </button>
      )}

      <div
        className={cx(
          'pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 via-black/50 to-transparent px-3 pt-10 pb-2 transition-opacity duration-200 sm:px-4 sm:pb-3',
          pip.active || theatre ? '' : 'sm:rounded-b-xl',
          controlsVisible ? 'opacity-100' : 'opacity-0',
          (mini || (startingNow && !wide)) && 'hidden',
        )}
      >
        <div className={cx(controlsVisible && 'pointer-events-auto')}>
        <Scrubber
          time={time}
          duration={duration}
          skips={skips}
          streamId={stream?.id}
          video={video}
          onSeek={(t) => video && (video.currentTime = t)}
        />

        <div className="relative mt-1.5 flex items-center gap-1 text-white sm:mt-2 sm:gap-2">
          {(!touch || pip.active) && (
            <>
              <IconButton label={playing ? 'Pause' : 'Play'} hint={keyHint('play')} onClick={togglePlay}>
                {playing ? <PauseIcon /> : <PlayIcon />}
              </IconButton>
              <IconButton label="Back 10 seconds" hint={keyHint('back10')} onClick={() => seekBy(-10)}>
                <SeekIcon back />
              </IconButton>
              <IconButton label="Forward 10 seconds" hint={keyHint('forward10')} onClick={() => seekBy(10)}>
                <SeekIcon />
              </IconButton>
            </>
          )}

          {onPrevious && (
            <span className={cx('contents', !pip.active && 'max-sm:hidden')}>
              <IconButton label="Previous episode" hint={keyHint('previous')} onClick={onPrevious}>
                <span className="contents [&>svg]:rotate-180">
                  <NextIcon />
                </span>
              </IconButton>
            </span>
          )}
          {onNext && (
            <IconButton label={nextLabel ?? 'Next episode'} hint={keyHint('next')} onClick={() => onNext('button')}>
              <NextIcon />
            </IconButton>
          )}

          <span className="contents max-sm:hidden">
            <Volume
              volume={volume}
              muted={muted}
              onChange={(v) => {
                if (!video) return
                video.volume = v
                video.muted = v === 0
              }}
              onToggle={() => video && (video.muted = !video.muted)}
            />
          </span>

          <span className="ml-1 text-xs whitespace-nowrap tabular-nums text-white/80">
            {clockTime(time)} / {clockTime(duration)}
            {partNow && <span className="text-filler max-sm:hidden"> · {skipLabel(partNow.kind)}</span>}
          </span>

          <div className="ml-auto flex items-center gap-1">
            {(wide || pip.active) && avButton}
            {wide && soundMenu && <SoundPicker value={soundMenu.value} onChange={soundMenu.onChange} win={pip.window ?? undefined} />}

            {pip.active ? (
              <button
                onClick={() => void pip.toggle()}
                className="flex items-center gap-1.5 rounded-lg bg-white/15 px-2.5 py-1.5 text-xs font-medium text-white ring-1 ring-white/20 backdrop-blur-sm hover:bg-white/25"
              >
                <PiPIcon />
                Back to tab
              </button>
            ) : (
              <>
                {wide && settingsMenu}
                {!castImpossible() && !saved && (
                  <CastButton
                    casting={casting}
                    device={castDevice}
                    phase={castPhase}
                    sdkState={castSdk}
                    onCast={() => void chooseTV()}
                    onStop={stopCast}
                    onOpen={holdForCast}
                  />
                )}
                {airplay.available && !saved && (
                  <IconButton
                    label={airplay.active ? 'Playing on AirPlay' : 'AirPlay'}
                    onClick={() =>
                      stream?.airplay
                        ? airplay.pick()
                        : showNotice('AirPlay needs access from other devices: turn it on in Settings, Devices.', 6000)
                    }
                  >
                    <AirPlayIcon on={airplay.active} />
                  </IconButton>
                )}
                {pip.supported ? (
                  <IconButton label="Picture in picture" onClick={() => void pip.toggle()}>
                    <PiPIcon />
                  </IconButton>
                ) : (
                  videoPip.supported && (
                    <IconButton label="Picture in picture" onClick={() => void videoPip.toggle()}>
                      <PiPIcon />
                    </IconButton>
                  )
                )}
                <IconButton label={fullscreen ? 'Exit full screen' : 'Full screen'} hint={keyHint('fullscreen')} onClick={goFullscreen}>
                  <FullscreenIcon />
                </IconButton>
              </>
            )}
          </div>
        </div>
        </div>
      </div>

      <div
        className={cx(
          'pointer-events-none absolute inset-x-0 top-0 bg-gradient-to-b from-black/70 to-transparent transition-opacity duration-200',
          pip.active ? 'p-2' : 'px-2 pt-2 pb-6 sm:p-4',
          controlsVisible ? 'opacity-100' : 'opacity-0',
          mini && 'hidden',
        )}
      >
        <div className="flex items-start gap-2">
          {minimizable && (
            <button
              type="button"
              onClick={onMinimize}
              aria-label="Minimize"
              title="Minimize"
              className={cx(
                'grid size-11 shrink-0 place-items-center rounded-full text-white transition-colors hover:bg-white/15',
                controlsVisible && 'pointer-events-auto',
              )}
            >
              <ChevronDownIcon />
            </button>
          )}
          <div className={cx('min-w-0 flex-1', minimizable && 'pt-2.5')}>
            {pip.active ? (
              <p className="truncate text-xs font-medium text-white/90">{subtitle ?? title}</p>
            ) : (
              (wide || fullscreen) && (
                <>
                  <p className="truncate text-sm font-medium text-white">{title}</p>
                  {subtitle && <p className="truncate text-xs text-white/80">{subtitle}</p>}
                </>
              )
            )}
            {(upscaleState === 'unsupported' || upscaleState === 'failed') && (
              <p className="mt-1.5 inline-block rounded bg-black/60 px-2 py-0.5 text-xs text-amber-300">
                {upscaleState === 'unsupported'
                  ? "Anime4K needs WebGPU, which this browser doesn't have; playing without it."
                  : 'Anime4K stopped (the graphics device was lost); playing without it.'}
              </p>
            )}
          </div>
          {!wide && !pip.active && (
            <div className={cx('flex shrink-0 items-center gap-1', controlsVisible && 'pointer-events-auto')}>
              {avButton}
              {settingsMenu}
            </div>
          )}
        </div>
      </div>

      {touch && !mini && !pip.active && !casting && !startingNow && (
        <div
          className={cx(
            'pointer-events-none absolute inset-0 flex items-center justify-center gap-6 pb-12 transition-opacity duration-200 sm:gap-12 sm:pb-0',
            controlsVisible ? 'opacity-100' : 'opacity-0',
          )}
        >
          <RoundButton label="Back 10 seconds" onClick={() => seekBy(-10)} live={controlsVisible}>
            <SeekIcon back />
          </RoundButton>
          <RoundButton label={playing ? 'Pause' : 'Play'} onClick={togglePlay} live={controlsVisible} big>
            {playing ? <PauseIcon /> : <PlayIcon />}
          </RoundButton>
          <RoundButton label="Forward 10 seconds" onClick={() => seekBy(10)} live={controlsVisible}>
            <SeekIcon />
          </RoundButton>
        </div>
      )}

      {infoOpen && !pip.active && (
        <PlaybackInfo
          stream={stream}
          video={video}
          hls={hls}
          audioTrack={audioTrack}
          subtitles={subtitlesInfo}
          anime4k={anime4kInfo}
          onClose={() => setInfoOpen(false)}
        />
      )}

      {casting && (
        <CastingPanel
          device={castDevice}
          time={remote.time}
          duration={remote.duration || duration}
          paused={remote.paused}
          onPlayPause={castPlayPause}
          onSeek={castSeek}
          onStop={stopCast}
        />
      )}

      <div className={mini ? 'hidden' : 'contents'}>{overlay}</div>

      {mini && (
        <button
          type="button"
          onClick={mini.onExpand}
          aria-label="Expand the player"
          className="absolute inset-0 z-10 cursor-pointer focus-visible:ring-2 focus-visible:ring-accent-400 focus-visible:outline-none"
        />
      )}
    </div>
  )

  return (
    <div ref={anchor} className="w-full">
      {createPortal(body, host)}
    </div>
  )
}

function skipLabel(kind: string): string {
  const k = skipKind(kind)
  return k ? k[0].toUpperCase() + k.slice(1) : kind
}

function Scrubber({
  time,
  duration,
  skips,
  streamId,
  video,
  onSeek,
}: {
  time: number
  duration: number
  skips: SkipRange[]
  streamId?: string
  video: HTMLVideoElement | null
  onSeek: (t: number) => void
}) {
  const sheet = useThumbnails(streamId, video)

  const [at, setAt] = useState<number | null>(null)
  const [dragging, setDragging] = useState(false)
  const hoverTime = at === null ? 0 : at * duration

  const fraction = (e: React.PointerEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    return Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width))
  }

  const shown = dragging && at !== null ? at * duration : time
  const percent = duration > 0 ? (shown / duration) * 100 : 0
  const loaded = useLoadedRanges(streamId, video)

  return (
    <div
      role="slider"
      tabIndex={0}
      aria-label="Seek"
      aria-valuemin={0}
      aria-valuemax={Math.round(duration)}
      aria-valuenow={Math.round(shown)}
      aria-valuetext={[
        `${clockTime(shown)} of ${clockTime(duration)}`,
        ...skips.filter((r) => shown >= r.start && shown < r.end).map((r) => skipLabel(r.kind)),
      ].join(', ')}
      onKeyDown={(e) => {
        if (duration <= 0) return
        const to =
          e.key === 'Home' ? 0 : e.key === 'End' ? duration - 1
            : e.key === 'PageUp' ? time + 60 : e.key === 'PageDown' ? time - 60 : null
        if (to === null) return
        e.preventDefault()
        onSeek(Math.min(duration, Math.max(0, to)))
      }}
      className="group/scrub relative -my-4 h-11 cursor-pointer touch-none rounded focus-visible:ring-2 focus-visible:ring-accent-400 focus-visible:outline-hidden"
      onPointerDown={(e) => {
        if (e.button !== 0) return
        e.currentTarget.setPointerCapture(e.pointerId)
        setDragging(true)
        setAt(fraction(e))
      }}
      onPointerMove={(e) => setAt(fraction(e))}
      onPointerUp={(e) => {
        if (dragging && duration > 0) onSeek(fraction(e) * duration)
        setDragging(false)
        if (e.pointerType !== 'mouse') setAt(null)
      }}
      onPointerCancel={() => {
        setDragging(false)
        setAt(null)
      }}
      onPointerLeave={() => {
        if (!dragging) setAt(null)
      }}
    >
      <div data-scrub-track className="absolute top-1/2 h-1 w-full -translate-y-1/2 overflow-hidden rounded-full bg-white/20">
        {duration > 0 &&
          loaded.map(([from, to]) => (
            <div
              key={from}
              data-loaded
              className="absolute inset-y-0 bg-white/35"
              style={{ left: `${(from / duration) * 100}%`, width: `${((to - from) / duration) * 100}%` }}
            />
          ))}
        <div data-played className="relative h-full bg-accent-500" style={{ width: `${percent}%` }} />
      </div>

      {duration > 0 &&
        skips.map((range) => (
          <div
            key={`${range.kind}-${range.start}`}
            title={`${skipLabel(range.kind)} · ${clockTime(range.start)}–${clockTime(range.end)}`}
            className="absolute top-1/2 h-1 -translate-y-1/2 rounded-full bg-filler"
            style={{
              left: `${(range.start / duration) * 100}%`,
              width: `${Math.max(0.4, ((range.end - range.start) / duration) * 100)}%`,
            }}
          />
        ))}

      <div
        className={cx(
          'absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-white transition-opacity group-hover/scrub:opacity-100',
          dragging ? 'scale-125 opacity-100' : 'opacity-0',
        )}
        style={{ left: `${percent}%` }}
      />

      {at !== null && duration > 0 && (
        <ScrubPreview at={at} time={hoverTime} sheet={sheet} skips={skips} />
      )}
    </div>
  )
}

function useLoadedRanges(streamId: string | undefined, video: HTMLVideoElement | null): Array<[number, number]> {
  const [loaded, setLoaded] = useState<{ stream?: string; ranges: Array<[number, number]> }>({ ranges: [] })
  useEffect(() => {
    if (!streamId) return
    let stopped = false
    let timer: ReturnType<typeof setTimeout> | undefined
    const read = async () => {
      try {
        const res = await api.get<{ complete: boolean; ranges: Array<[number, number]> }>(`/api/stream/${streamId}/loaded`)
        if (stopped) return
        setLoaded({ stream: streamId, ranges: res.ranges })
        if (!res.complete) timer = setTimeout(() => void untilWatching(null, () => stopped).then(read), 4000)
      } catch {
        if (!stopped) timer = setTimeout(read, 10_000)
      }
    }
    void read()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [streamId, video])
  return loaded.stream === streamId ? loaded.ranges : []
}

function ScrubPreview({
  at,
  time,
  sheet,
  skips,
}: {
  at: number
  time: number
  sheet: Sheet | undefined
  skips: SkipRange[]
}) {
  const ready = sheet?.ready && sheet.interval > 0
  const inSkip = skips.find((r) => time >= r.start && time <= r.end)

  let tile: React.CSSProperties | undefined
  const index = ready && sheet ? Math.min(sheet.count - 1, Math.max(0, Math.floor(time / sheet.interval))) : 0
  if (ready && sheet && (!sheet.partial || sheet.have?.[index] === '1')) {
    const column = index % sheet.columns
    const row = Math.floor(index / sheet.columns)
    tile = {
      width: sheet.width,
      height: sheet.height,
      backgroundImage: `url(${sheet.url})`,
      backgroundSize: `${sheet.columns * sheet.width}px ${sheet.rows * sheet.height}px`,
      backgroundPosition: `-${column * sheet.width}px -${row * sheet.height}px`,
    }
  }

  return (
    <div
      className="pointer-events-none absolute bottom-8 -translate-x-1/2"
      style={{ left: `clamp(${(sheet?.width ?? 80) / 2}px, ${at * 100}%, calc(100% - ${(sheet?.width ?? 80) / 2}px))` }}
    >
      <div className="overflow-hidden rounded-md bg-base-950/90 shadow-lift ring-1 ring-white/15">
        {tile && <div style={tile} />}
        <p className="px-2 py-1 text-center text-xs font-medium tabular-nums text-white">
          {clockTime(time)}
          {inSkip && <span className="ml-1.5 text-filler">{skipLabel(inSkip.kind)}</span>}
        </p>
      </div>
    </div>
  )
}

function Volume({
  volume,
  muted,
  onChange,
  onToggle,
}: {
  volume: number
  muted: boolean
  onChange: (v: number) => void
  onToggle: () => void
}) {
  return (
    <div className="group/vol flex items-center">
      <IconButton label={muted ? 'Unmute' : 'Mute'} hint={keyHint('mute')} onClick={onToggle}>
        {muted || volume === 0 ? <MutedIcon /> : <VolumeIcon />}
      </IconButton>
      <input
        type="range"
        min={0}
        max={1}
        step={0.05}
        value={muted ? 0 : volume}
        onChange={(e) => onChange(Number(e.target.value))}
        aria-label="Volume"
        className="w-0 accent-[var(--color-accent-500)] opacity-0 transition-all duration-200 group-hover/vol:w-20 group-hover/vol:opacity-100 group-focus-within/vol:w-20 group-focus-within/vol:opacity-100 pointer-coarse:w-20 pointer-coarse:opacity-100"
      />
    </div>
  )
}

function subtitleLabel(tracks: StreamInfo['subtitles'], t: StreamInfo['subtitles'][number]): string {
  const language = languageName(t.language)
  if (!language) return t.title || `Track ${t.index + 1}`
  if (t.title && tracks.filter((o) => languageName(o.language) === language).length > 1) return `${language} · ${t.title}`
  return language
}

function AudioSubtitlesPanel({
  open,
  onOpen,
  label,
  win,
  floating,
  audioChoice,
  preferred,
  audio,
  tracks,
  value,
  onChange,
  scale,
  onScale,
  delay,
  onDelay,
  keptDelay,
  onKeepDelay,
  look,
  onLoadFile,
  jimaku,
}: {
  open: boolean
  onOpen: (open: boolean) => void
  label: boolean
  win?: Window
  floating: boolean
  audioChoice?: ReactNode
  preferred?: string[]
  audio?: { tracks: AudioTrack[]; value: number; onChange: (index: number) => void }
  tracks?: StreamInfo['subtitles']
  value: number | null
  onChange: (index: number | null) => void
  scale: number
  onScale: (scale: number) => void
  delay: number
  onDelay: (delay: number) => void
  keptDelay?: number
  onKeepDelay?: (delay: number) => void
  look?: { value: SubLook; onChange: (look: SubLook) => void; raised?: boolean; onRaised?: (raised: boolean) => void }
  onLoadFile: (file: File) => void
  jimaku?: { streamId: string; onPick: (file: JimakuFile) => void }
}) {
  const close = useCallback(() => onOpen(false), [onOpen])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const panel = useRef<HTMLDivElement>(null)
  useModalFocus(panel, open)
  const narrow = !useMediaQuery('(min-width: 40rem)') && !floating
  const room = useRoomAbove(ref, open && !narrow, win)
  const picker = useRef<HTMLInputElement>(null)
  const scroller = useRef<HTMLDivElement>(null)
  const jimakuFiles = useQuery({
    queryKey: ['jimaku-files', jimaku?.streamId],
    queryFn: () => api.get<{ files: JimakuFile[] }>(`/api/stream/${jimaku!.streamId}/jimaku`),
    enabled: open && !!jimaku,
    staleTime: 10 * 60_000,
    retry: false,
  })
  const here = narrow ? 'This phone' : 'This device'
  const active = tracks?.find((t) => t.index === value)
  const name = (t: StreamInfo['subtitles'][number]) => subtitleLabel(tracks ?? [], t)
  const kept = !!onKeepDelay && delay !== 0 && delay === keptDelay
  const sub = 'px-4 pt-2 pb-0.5 text-xs font-medium text-base-300'
  const [roomLeft, setRoomLeft] = useState(0)
  useLayoutEffect(() => {
    if (!open) return
    const view = win ?? window
    const measure = () => setRoomLeft(ref.current?.getBoundingClientRect().right ?? 0)
    measure()
    view.addEventListener('resize', measure)
    const shell = ref.current?.closest('[role="region"]')
    const observer = shell ? new ResizeObserver(measure) : null
    if (shell) observer?.observe(shell)
    return () => {
      view.removeEventListener('resize', measure)
      observer?.disconnect()
    }
  }, [open, win, ref])
  const columns = !narrow && !floating && !!(audioChoice || audio) && roomLeft >= 576
  const [view, setView] = useState<'main' | 'style'>('main')
  const go = (next: 'main' | 'style') => {
    setView(next)
    scroller.current?.scrollTo({ top: 0 })
  }
  const [moreFor, setMoreFor] = useState<string | null>(null)
  const [wasOpen, setWasOpen] = useState(open)
  if (open !== wasOpen) {
    setWasOpen(open)
    if (!open) {
      setView('main')
      setMoreFor(null)
    }
  }
  const base = (code?: string) => languageName(code?.split(/[-_]/)[0])
  const wanted = new Set((preferred?.length ? preferred : ['en']).map(base))
  const foreign = (tracks ?? []).filter((t) => {
    const language = base(t.language)
    return !!language && !wanted.has(language)
  })
  const foreignKey = foreign.map((t) => t.index).join(',')
  const more = !!foreignKey && moreFor === foreignKey
  const others = foreign.filter((t) => t.index !== value)
  const folded = others.length > 1 && !more
  const shown = (tracks ?? []).filter((t) => !folded || !others.includes(t))
  const styleValue = [
    SUB_SIZES.find((s) => s.value === String(scale))?.name,
    look && !active?.pictures ? subLookLabel(look.value) : undefined,
    delay !== 0 ? delayLabel(delay) : undefined,
  ]
    .filter(Boolean)
    .join(' · ')

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => onOpen(!open)}
        aria-label="Audio & subtitles"
        title={`Audio & subtitles (${keyHint('subtitles')} changes the subtitles)`}
        className={cx(
          'tap relative flex items-center gap-1.5 text-xs font-medium text-white/90 transition-colors hover:bg-white/15',
          label ? 'h-9 rounded-md px-2.5' : 'h-11 rounded-full px-3',
        )}
      >
        <CaptionIcon />
        <span className={cx(!label && 'max-[359px]:sr-only')}>Audio &amp; subtitles</span>
      </button>

      {open && narrow && <div aria-hidden className="fixed inset-0 z-50 bg-black/50" onClick={close} />}
      {open && (
        <div
          ref={panel}
          role="dialog"
          aria-label="Audio & subtitles"
          style={narrow ? undefined : { maxHeight: room }}
          className={cx(
            'z-50 flex animate-rise flex-col overflow-hidden bg-base-750 text-left shadow-xl shadow-black/60 ring-1 ring-white/[0.06]',
            narrow
              ? 'fixed inset-x-0 bottom-0 max-h-[60dvh] rounded-t-sheet pb-[max(0.5rem,env(safe-area-inset-bottom))]'
              : cx(
                  'absolute right-0 bottom-full mb-2 max-w-[calc(100vw-1rem)] origin-bottom-right rounded-xl',
                  columns ? 'w-[560px]' : 'w-[340px]',
                ),
          )}
        >
          {narrow && <div aria-hidden className="mx-auto mt-2 h-1 w-10 shrink-0 rounded-full bg-base-600" />}
          <div className="flex shrink-0 items-center justify-between gap-3 py-1 pr-1.5 pl-4">
            {view === 'style' ? (
              <button
                type="button"
                onClick={() => go('main')}
                aria-label="Back to audio and subtitles"
                className="-ml-2 flex min-h-11 items-center gap-2 rounded-lg px-2 text-sm font-semibold text-white transition-colors hover:bg-white/10"
              >
                <span aria-hidden className="text-base-400">
                  ‹
                </span>
                Size, look and timing
              </button>
            ) : (
              <h2 className="text-sm font-semibold text-white">Audio &amp; subtitles</h2>
            )}
            <button
              type="button"
              onClick={close}
              className="min-h-11 rounded-lg px-3 text-sm font-semibold text-accent-300 transition-colors hover:bg-white/10"
            >
              Done
            </button>
          </div>
          <div ref={scroller} className="min-h-0 flex-1 overflow-y-auto overscroll-contain scrollbar-thin">
            {view === 'main' && (
              <div className={cx(columns && 'grid grid-cols-2 divide-x divide-white/[0.06] [&>section]:border-t-0')}>
                {(audioChoice || audio) && (
                  <PanelGroup title="Audio" note={audioChoice ? 'This show' : undefined}>
                    {audioChoice}
                    {audio && (
                      <>
                        <p className={sub}>Tracks in this release · this episode</p>
                        <Choices label="Audio track">
                          {audio.tracks.map((_, i) => (
                            <ChoiceRow
                              key={i}
                              label={audioLabel(audio.tracks, i)}
                              selected={i === audio.value}
                              onClick={() => audio.onChange(i)}
                            />
                          ))}
                        </Choices>
                      </>
                    )}
                  </PanelGroup>
                )}

                <PanelGroup title="Subtitles" note={tracks ? 'This episode' : undefined}>
                  {tracks ? (
                    <Choices label="Subtitle track">
                      <ChoiceRow label="Off" selected={value === null} onClick={() => onChange(null)} />
                      {shown.map((t) => (
                        <ChoiceRow key={t.index} label={name(t)} selected={t.index === value} onClick={() => onChange(t.index)} />
                      ))}
                    </Choices>
                  ) : (
                    <p className="px-4 py-2 text-xs text-base-400">The episode's tracks show here once it opens.</p>
                  )}
                  {others.length > 1 && (
                    <button
                      type="button"
                      onClick={() => setMoreFor(more ? null : foreignKey)}
                      aria-expanded={more}
                      className="mx-2 flex min-h-10 w-[calc(100%-1rem)] items-center gap-3 rounded-lg px-2.5 py-1.5 text-left text-sm text-base-200 transition-colors hover:bg-white/[0.05] pointer-coarse:min-h-12"
                    >
                      <span className="min-w-0 flex-1">
                        {more ? 'Fewer languages' : `More languages · ${others.length}`}
                        {!more && (
                          <span className="mt-0.5 block truncate text-xs text-base-400">
                            {[...new Set(others.map((t) => languageName(t.language)))].join(', ')}
                          </span>
                        )}
                      </span>
                      <span aria-hidden className={cx('text-base-500 transition-transform', more && '-rotate-90')}>
                        ›
                      </span>
                    </button>
                  )}
                  {jimaku && (
                    <>
                      <p className={sub}>Jimaku · Japanese</p>
                      {jimakuFiles.isPending ? (
                        <p className="px-4 py-2 text-xs text-base-400">Looking…</p>
                      ) : jimakuFiles.isError ? (
                        <p className="px-4 py-2 text-xs text-recap">{(jimakuFiles.error as Error).message}</p>
                      ) : jimakuFiles.data.files.length === 0 ? (
                        <p className="px-4 py-2 text-xs text-base-400">None for this episode</p>
                      ) : (
                        <Choices label="Jimaku subtitles">
                          {jimakuFiles.data.files.map((f) => (
                            <ChoiceRow
                              key={f.index}
                              label={<span className="break-all">{f.name}</span>}
                              selected={JIMAKU_TRACK - f.index === value}
                              onClick={() => jimaku.onPick(f)}
                            />
                          ))}
                        </Choices>
                      )}
                    </>
                  )}
                  {tracks && (
                    <button
                      type="button"
                      onClick={() => picker.current?.click()}
                      className="mx-2 mt-1 flex min-h-10 items-center rounded-lg px-2.5 text-left text-xs font-medium text-accent-300 transition-colors hover:bg-white/[0.05] pointer-coarse:min-h-11"
                    >
                      Load a subtitle file…
                    </button>
                  )}
                  <input
                    ref={picker}
                    type="file"
                    accept=".ass,.ssa,.srt,.vtt"
                    hidden
                    onChange={(e) => {
                      const file = e.currentTarget.files?.[0]
                      e.currentTarget.value = ''
                      if (file) onLoadFile(file)
                    }}
                  />
                </PanelGroup>
              </div>
            )}
            {view === 'main' && (
              <div className="border-t border-white/[0.06]">
                <ActionRow label="Size, look and timing" value={styleValue} onClick={() => go('style')} />
              </div>
            )}

            {view === 'style' && (
              <div className={cx(columns && look && 'grid grid-cols-2 divide-x divide-white/[0.06] [&>section]:border-t-0')}>
                {look && (
                  <PanelGroup title="Look" note="All devices">
                    {active?.pictures ? (
                      <p className="px-4 py-2 text-xs text-base-400">Picture subtitles keep the look they were made with.</p>
                    ) : (
                      <>
                        <div
                          aria-hidden
                          className="mx-4 mb-2 flex h-16 items-end justify-center rounded-lg bg-[linear-gradient(135deg,#384868,#6e5546)] pb-2 font-semibold"
                          style={{ fontSize: `${0.875 * scale}rem`, ...(look.raised ? { paddingBottom: '1.4rem' } : {}) }}
                        >
                          <span style={previewStyle(look.value)}>Where to next?</span>
                        </div>
                        <Choices label="Subtitle look">
                          {SUB_LOOKS.map((l) => (
                            <ChoiceRow key={l.value} label={l.label} selected={look.value === l.value} onClick={() => look.onChange(l.value)} />
                          ))}
                        </Choices>
                      </>
                    )}
                  </PanelGroup>
                )}
                <PanelGroup title="Size and timing">
                  <SegmentRow
                    label="Size"
                    caption={here}
                    value={String(scale)}
                    options={SUB_SIZES}
                    onChange={(v) => onScale(Number(v))}
                  />
                  {look?.onRaised && !active?.pictures && (
                    <SegmentRow
                      label="Position"
                      caption="All devices · raised clears burned-in text"
                      value={look.raised ? 'raised' : 'low'}
                      options={[
                        { value: 'low', label: 'Low' },
                        { value: 'raised', label: 'Raised' },
                      ]}
                      onChange={(v) => look.onRaised?.(v === 'raised')}
                    />
                  )}
                  <Stepper
                    label="Delay"
                    caption={kept ? 'Kept for this show and group' : 'This episode'}
                    value={delayLabel(delay)}
                    onLess={() => onDelay(Math.round((delay - DELAY_STEP) * 10) / 10)}
                    onMore={() => onDelay(Math.round((delay + DELAY_STEP) * 10) / 10)}
                    onReset={() => onDelay(0)}
                  />
                  {onKeepDelay && delay !== (keptDelay ?? 0) && (
                    <button
                      type="button"
                      onClick={() => onKeepDelay(delay)}
                      className="flex min-h-11 w-full items-center px-4 text-left text-xs font-medium text-accent-300 transition-colors hover:bg-white/[0.06]"
                    >
                      {delay === 0 ? 'Stop keeping a delay for this show' : 'Keep this delay for this show'}
                    </button>
                  )}
                </PanelGroup>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

function Stepper({
  label,
  caption,
  value,
  onLess,
  onMore,
  onReset,
}: {
  label: string
  caption?: string
  value: string
  onLess: () => void
  onMore: () => void
  onReset: () => void
}) {
  const step = 'grid size-9 place-items-center rounded-md text-base-200 hover:bg-white/10 hover:text-white pointer-coarse:size-11'
  return (
    <div className="flex min-h-12 items-center gap-1 px-4 py-1 text-sm">
      <span className="min-w-0 flex-1 text-base-100">
        {label}
        {caption && <span className="mt-0.5 block text-xs text-base-400">{caption}</span>}
      </span>
      <button type="button" aria-label={`${label} down`} onClick={onLess} className={step}>
        −
      </button>
      <button
        type="button"
        aria-label={`Reset ${label.toLowerCase()}`}
        title="Reset"
        onClick={onReset}
        className="h-9 w-14 rounded-md text-center text-xs tabular-nums text-base-200 hover:bg-white/10 pointer-coarse:h-11"
      >
        {value}
      </button>
      <button type="button" aria-label={`${label} up`} onClick={onMore} className={step}>
        +
      </button>
    </div>
  )
}

function audioLabel(tracks: AudioTrack[], i: number): string {
  const t = tracks[i]
  if (!t) return 'Audio'
  const language = languageName(t.language)
  if (!language) return t.title || `Track ${i + 1}`
  if (t.title && tracks.filter((o) => languageName(o.language) === language).length > 1) return `${language} · ${t.title}`
  return language
}

const LEVEL_LABELS: Record<VoiceLevel, string> = { off: 'Off', medium: 'Medium', high: 'High' }

function soundQuery(patch: Partial<SoundOptions>): string {
  const q = new URLSearchParams()
  for (const [key, value] of Object.entries(patch)) if (value !== undefined) q.set(key, String(value))
  const s = q.toString()
  return s ? `?${s}` : ''
}

function soundSummary(s: SoundOptions): string {
  const on = []
  if (s.voice !== 'off') on.push(`Voice ${LEVEL_LABELS[s.voice].toLowerCase()}`)
  if (s.night) on.push('Night')
  if (s.headphones) on.push('Headphones')
  if (s.mono) on.push('Mono')
  if (s.boost !== 'off') on.push(`Boost ${LEVEL_LABELS[s.boost].toLowerCase()}`)
  return on.length ? on.join(', ') : 'Default'
}

function SoundPicker({
  value,
  onChange,
  win,
}: {
  value: SoundOptions
  onChange: (patch: Partial<SoundOptions>) => void
  win?: Window
}) {
  const { open, close, toggle, trigger, menu } = useMenuButton()
  const ref = useDismiss<HTMLDivElement>(close, open)
  const room = useRoomAbove(ref, open, win)
  const summary = soundSummary(value)

  return (
    <div className="relative" ref={ref}>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={toggle}
        title="Sound: voice boost (V), night mode, headphones, mono, volume boost"
        aria-label={`Sound: ${summary}`}
        className={cx(
          'flex h-9 items-center gap-1.5 rounded-md px-2.5 text-xs transition-colors hover:bg-white/15',
          summary === 'Default' ? 'text-white/90' : 'text-accent-300',
        )}
      >
        <VoiceIcon />
        <span className="max-w-32 truncate">{summary === 'Default' ? 'Sound' : summary}</span>
      </button>

      {open && (
        <div
          ref={menu}
          role="menu"
          aria-label="Sound"
          onKeyDown={walkMenu}
          style={{ maxHeight: room }}
          className="absolute right-0 bottom-full z-50 mb-2 w-80 max-w-[calc(100vw-1rem)] origin-bottom-right animate-rise overflow-y-auto rounded-xl bg-base-750 py-1 shadow-xl shadow-black/60 ring-1 ring-white/[0.06] scrollbar-thin"
        >
          <p className="px-3.5 pt-2 pb-1 text-sm font-semibold text-white">Sound</p>
          <SoundRows value={value} onChange={onChange} />
        </div>
      )}
    </div>
  )
}

function SoundRows({ value, onChange }: { value: SoundOptions; onChange: (patch: Partial<SoundOptions>) => void }) {
  return (
    <>
      <LevelRow label="Voice boost" caption="Clearer dialogue" options={LEVEL_OPTIONS} value={value.voice} onChange={(voice) => onChange({ voice })} />
      <LevelRow label="Volume boost" caption="Louder overall" options={LEVEL_OPTIONS} value={value.boost} onChange={(boost) => onChange({ boost })} />
      <div className="my-1 border-t border-white/[0.06]" />
      <MenuToggle label="Night mode" caption="Evens out loud and quiet" on={value.night} onChange={(night) => onChange({ night })} />
      <MenuToggle
        label="Headphones"
        caption="Softer stereo for headphones"
        on={value.headphones}
        onChange={(headphones) => onChange({ headphones })}
      />
      <MenuToggle label="Mono" caption="Both sides in both ears" on={value.mono} onChange={(mono) => onChange({ mono })} />
    </>
  )
}

const LEVEL_OPTIONS = VOICE_LEVELS.map((level) => ({ value: level, label: LEVEL_LABELS[level] }))

function LevelRow<T extends string>({
  label,
  caption,
  options,
  value,
  onChange,
}: {
  label: string
  caption?: string
  options: ReadonlyArray<{ value: T; label: string }>
  value: T
  onChange: (value: T) => void
}) {
  return (
    <div role="group" aria-label={label} className="flex items-center justify-between gap-3 px-3.5 py-2">
      <span className="min-w-0 text-sm text-base-100">
        {label}
        {caption && <span className="block text-xs text-base-500">{caption}</span>}
      </span>
      <span className="flex shrink-0 gap-0.5 rounded-lg bg-base-900 p-0.5">
        {options.map((o) => (
          <button
            key={o.value}
            role="menuitemradio"
            aria-checked={o.value === value}
            onClick={() => onChange(o.value)}
            className={cx(
              'h-7 rounded-md px-2 text-xs font-medium transition-colors pointer-coarse:h-10',
              o.value === value ? 'bg-base-700 text-white' : 'text-base-300 hover:text-white',
            )}
          >
            {o.label}
          </button>
        ))}
      </span>
    </div>
  )
}

function VoiceIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path
        d="M12 3a3 3 0 0 0-3 3v6a3 3 0 0 0 6 0V6a3 3 0 0 0-3-3Zm-6 9a6 6 0 0 0 12 0M12 18v3"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

const SKIP_SOURCES: Record<string, string> = {
  marked: 'Yours', chapters: 'Chapters', aniskip: 'AniSkip', found: 'Found by Yoru',
}

const SLEEP_MINUTES = [15, 30, 45, 60]

type MenuView = 'main' | 'speed' | 'sleep' | 'skips' | 'sound' | 'quality' | 'skipping'

const skipShort = (mode: SkipMode) => SKIP_MODES.find((c) => c.value === mode)?.label ?? mode

function skipSummary(p: PlaybackSettings): string {
  const op = skipShort(p.openings.value)
  const ed = skipShort(p.endings.value)
  return op === ed ? op : `Openings ${op.toLowerCase()}, endings ${ed.toLowerCase()}`
}

function PlayerMenu({
  rate,
  onRate,
  sleep,
  skips,
  video,
  marking,
  sound,
  playback,
  onOpen,
  onInfo,
  more = false,
}: {
  rate: number
  onRate: (rate: number) => void
  sleep: Sleep
  skips: SkipRange[]
  video: HTMLVideoElement | null
  marking?: Marking
  sound?: { value: SoundOptions; onChange: (patch: Partial<SoundOptions>) => void }
  playback?: PlaybackSettings
  onOpen: (open: boolean) => void
  onInfo: () => void
  more?: boolean
}) {
  const { open, close: shut, toggle, trigger, menu } = useMenuButton()
  const [view, setView] = useState<MenuView>('main')
  const net = useNetwork()
  const close = useCallback(() => {
    shut()
    setView('main')
  }, [shut])
  useEffect(() => {
    const doc = menu.current?.ownerDocument
    if (open && doc && (doc.activeElement === doc.body || !doc.activeElement)) menuItems(menu.current)[0]?.focus()
  }, [open, view, menu])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const { wide, backdrop, style: sheetStyle, className: sheetClass } = usePopSheet(ref, open, close)
  useEffect(() => {
    onOpen(open)
    return () => onOpen(false)
  }, [open, onOpen])
  const from = marking?.from ?? null
  const coarse = useMediaQuery('(pointer: coarse)')
  const now = useNow(15_000)
  const left = sleep.kind === 'at' ? Math.max(1, Math.round((sleep.at - now) / 60_000)) : 0
  const sleepValue = sleep.kind === 'at' ? `${left} min left` : sleep.kind === 'episode' ? 'End of episode' : 'Off'
  const busy = sleep.kind === 'at' || sleep.kind === 'episode' || !!from
  const name = more ? 'More' : 'Player settings'
  const label =
    sleep.kind === 'at'
      ? `${name} · sleep timer: ${left} min left`
      : sleep.kind === 'episode'
        ? `${name} · sleep timer: end of this episode`
        : name
  const pickSleep = (next: Sleep) => {
    setSleep(next)
    close()
  }

  return (
    <div className="relative" ref={ref}>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        title={label}
        onClick={(e) => (open ? close() : toggle(e))}
        className={cx(
          'tap flex items-center gap-1 text-xs tabular-nums transition-colors hover:bg-white/15',
          more ? 'h-11 min-w-11 justify-center rounded-full px-2.5' : 'h-9 rounded-md px-2',
          busy ? 'text-accent-300' : 'text-white/90',
        )}
      >
        {more ? <MoreIcon /> : <GearIcon />}
        {sleep.kind === 'at' && <span>{left}m</span>}
      </button>

      {backdrop}
      {open && (
        <div
          ref={menu}
          role="menu"
          aria-label="Player settings"
          onKeyDown={walkMenu}
          style={sheetStyle}
          className={sheetClass('w-72', 'py-1')}
        >
          {view === 'main' && (
            <>
              {!wide && sound && <MenuItem label="Sound" value={soundSummary(sound.value)} onClick={() => setView('sound')} next />}
              <MenuItem label="Speed" value={rate === 1 ? 'Normal' : `${rate}×`} onClick={() => setView('speed')} next />
              <MenuItem
                label="Quality"
                value={QUALITIES.find((q) => q.value === net.quality)?.label ?? 'Auto'}
                onClick={() => setView('quality')}
                next
              />
              {playback?.upscale && (
                <MenuItem
                  label="Sharper picture"
                  value={playback.upscale.value}
                  onClick={() => {
                    close()
                    playback.upscale?.onOpen()
                  }}
                  next
                />
              )}
              <div aria-hidden className="my-1 border-t border-white/[0.06]" />
              {playback && <MenuItem label="Skipping" value={skipSummary(playback)} onClick={() => setView('skipping')} next />}
              {playback && (
                <MenuToggle
                  label="Play next by itself"
                  caption={playback.autoNext.caption}
                  on={playback.autoNext.on}
                  onChange={playback.autoNext.onChange}
                />
              )}
              {playback && (
                <MenuToggle
                  label="Start playing when opened"
                  caption={playback.autoplay.caption}
                  on={playback.autoplay.on}
                  onChange={playback.autoplay.onChange}
                />
              )}
              <MenuItem label="Sleep timer" value={sleepValue} onClick={() => setView('sleep')} next />
              {marking && !playback && (
                <MenuItem
                  label="Skip times"
                  value={from ? 'Marking' : skips.length ? `${skips.length} known` : 'None known'}
                  onClick={() => setView('skips')}
                  next
                />
              )}
              <div aria-hidden className="my-1 border-t border-white/[0.06]" />
              <MenuItem label="Playback info" value={coarse ? '' : keyHint('info')} onClick={() => (onInfo(), close())} />
              <div className="pointer-coarse:hidden">
                <MenuItem label="Keyboard shortcuts" value="?" onClick={() => (window.dispatchEvent(new Event(SHOW_SHORTCUTS)), close())} />
              </div>
            </>
          )}

          {view === 'speed' && (
            <>
              <MenuBack label="Speed" onClick={() => setView('main')} />
              {SPEEDS.map((s) => (
                <MenuRow key={s} selected={s === rate} onClick={() => (onRate(s), close())}>
                  {s === 1 ? 'Normal' : `${s}×`}
                </MenuRow>
              ))}
            </>
          )}

          {view === 'quality' && (
            <>
              <MenuBack label="Quality" onClick={() => setView('main')} />
              {QUALITIES.map((q) => (
                <MenuRow key={q.value} selected={q.value === net.quality} onClick={() => (setDeviceQuality(q.value), close())}>
                  {q.label}
                </MenuRow>
              ))}
              <p className="px-3 py-1.5 text-xs text-base-400">
                This device only. Auto changes nothing at home; away, it lowers a connection that can't keep up.
              </p>
            </>
          )}

          {view === 'sleep' && (
            <>
              <MenuBack label="Sleep timer" onClick={() => setView('main')} />
              <MenuRow selected={sleepValue === 'Off'} onClick={() => pickSleep({ kind: 'off' })}>
                Off
              </MenuRow>
              {SLEEP_MINUTES.map((m) => (
                <MenuRow key={m} selected={false} onClick={() => pickSleep({ kind: 'at', at: Date.now() + m * 60_000 })}>
                  {m} minutes
                </MenuRow>
              ))}
              <MenuRow selected={sleep.kind === 'episode'} onClick={() => pickSleep({ kind: 'episode' })}>
                End of this episode
              </MenuRow>
              {sleep.kind === 'at' && <p className="px-3 py-1.5 text-xs text-base-400">Pauses in {left} min, on any episode.</p>}
            </>
          )}

          {view === 'sound' && sound && (
            <>
              <MenuBack label="Sound" onClick={() => setView('main')} />
              <SoundRows value={sound.value} onChange={sound.onChange} />
            </>
          )}

          {view === 'skipping' && playback && (
            <>
              <MenuBack label="Skipping" onClick={() => setView('main')} />
              <LevelRow
                label="Openings"
                caption={playback.openings.caption}
                options={SKIP_MODES}
                value={playback.openings.value}
                onChange={playback.openings.onChange}
              />
              <LevelRow
                label="Endings"
                caption={playback.endings.caption}
                options={SKIP_MODES}
                value={playback.endings.value}
                onChange={playback.endings.onChange}
              />
              <p className="px-3.5 pt-0.5 pb-2 text-xs text-base-500">Button shows Skip while it plays. Auto skips by itself, with Undo.</p>
              <div className="mt-1 border-t border-white/[0.06] pt-1">
                <MenuToggle label="Skip filler episodes" caption="This show" on={playback.filler.on} onChange={playback.filler.onChange} />
                {marking && (
                  <MenuItem
                    label="Skip times"
                    value={from ? 'Marking' : skips.length ? `${skips.length} known` : 'None known'}
                    onClick={() => setView('skips')}
                    next
                  />
                )}
              </div>
            </>
          )}

          {view === 'skips' && marking && (
            <>
              <MenuBack label="Skip times" onClick={() => setView(playback ? 'skipping' : 'main')} />
              <SkipTimesPanel skips={skips} video={video} marking={marking} />
            </>
          )}
        </div>
      )}
    </div>
  )
}

const codecName = (codec?: string) => (codec === 'h264' ? 'H.264' : codec?.toUpperCase())

function PlaybackInfo({
  stream,
  video,
  hls,
  audioTrack,
  subtitles,
  anime4k,
  onClose,
}: {
  stream?: StreamInfo
  video: HTMLVideoElement | null
  hls: { current: { bandwidthEstimate: number } | null }
  audioTrack: number
  subtitles: string
  anime4k: string
  onClose: () => void
}) {
  useNow(1000)
  const unit = useSpeedUnit()
  const panel = useRef<HTMLDivElement>(null)
  const [health, setHealth] = useState<StreamHealth | null>(null)
  const [copied, setCopied] = useState<string | null>(null)
  const id = stream?.id
  useEffect(() => {
    if (!id) return
    let stop = false
    const poll = () =>
      document.visibilityState === 'visible' &&
      api
        .get<StreamHealth>(`/api/stream/${id}/health`)
        .then((h) => !stop && setHealth(h))
        .catch(() => {})
    void poll()
    const timer = window.setInterval(() => void poll(), 3000)
    return () => {
      stop = true
      window.clearInterval(timer)
    }
  }, [id])

  const plan = stream?.plan
  const v = stream?.video
  const audio = stream?.audio?.[audioTrack]
  const quality = video?.getVideoPlaybackQuality?.()
  let ahead = 0
  if (video) {
    for (let i = 0; i < video.buffered.length; i++) {
      if (video.buffered.start(i) <= video.currentTime + 0.5 && video.currentTime <= video.buffered.end(i)) {
        ahead = video.buffered.end(i) - video.currentTime
      }
    }
  }
  const rows: [string, string][] = []
  if (plan) {
    const method = plan.videoCopy ? (plan.audioCopy ? 'Copied as it is' : 'Sound converted only') : 'Converting'
    const why = plan.reason?.replace(/^re-encoding /, '')
    const detail = plan.videoCopy && plan.audioCopy ? (plan.forDevice ? 'this device decodes it' : '') : why
    rows.push(['Method', detail ? `${method} · ${detail}` : method])
    if (!plan.videoCopy && plan.videoCodec) {
      rows.push(['Encoder', `${ENCODERS[plan.videoCodec] ?? plan.videoCodec} (${plan.videoCodec})`])
    }
  }
  if (v) {
    const range = v.dvProfile ? `Dolby Vision ${v.dvProfile}` : v.transfer === 'smpte2084' ? 'HDR10' : v.transfer === 'arib-std-b67' ? 'HLG' : ''
    const picture = [
      codecName(v.codec), v.profile, v.width && v.height ? `${v.width}×${v.height}` : '', v.bitDepth ? `${v.bitDepth}-bit` : '', range,
      ['tt', 'bb', 'tb', 'bt'].includes(v.fieldOrder ?? '') ? 'interlaced' : '',
    ]
    const sound = audio ? [codecName(audio.codec), audio.channels ? `${audio.channels} ch` : ''] : []
    rows.push(['Source', [...picture, ...sound].filter(Boolean).join(' · ')])
  }
  if (plan) {
    const sentVideo = plan.videoCopy ? codecName(v?.codec) : 'H.264'
    const layout = plan.channels === 2 ? 'stereo' : plan.channels ? `${plan.channels} ch` : ''
    const sentAudio = plan.audioCopy ? codecName(audio?.codec) : `${codecName(plan.audioCodec ?? 'aac')} ${layout}`.trim()
    rows.push(['Sent', [sentVideo, sentAudio].filter(Boolean).join(' · ')])
    const changes = [
      plan.toneMap && 'HDR tone-mapped', plan.deinterlace && 'deinterlaced', plan.soften && 'flashing softened',
      plan.maxRate && `capped at ${plan.height}p, ${(plan.maxRate / 1e6).toFixed(1)} Mb/s`,
    ].filter(Boolean)
    if (changes.length) rows.push(['Picture', changes.join(' · ')])
  }
  if (health?.speed) rows.push(['Converter', `${health.speed.toFixed(1)}× real time`])
  rows.push(['Buffered', `${Math.max(0, Math.round(ahead))} s ahead`])
  if (quality) rows.push(['Dropped frames', `${quality.droppedVideoFrames} of ${quality.totalVideoFrames}`])
  const estimate = hls.current?.bandwidthEstimate
  if (estimate) rows.push(['Network', `${speed(estimate / 1e6, unit)} estimated`])
  if (health) {
    rows.push([
      'Download',
      health.complete
        ? 'Whole file on disk'
        : `${Math.round(health.percent ?? 0)}% on disk · ${speed(health.mbps, unit)} from ${health.peers} peer${health.peers === 1 ? '' : 's'}`,
    ])
  }
  rows.push(['Anime4K', anime4k])
  rows.push(['Subtitles', subtitles])

  const copy = async () => {
    const text = rows.map(([name, details]) => `${name}: ${details}`).join('\n')
    setCopied((await copyText(text, panel.current ?? undefined)) ? 'Copied' : "Couldn't copy")
    window.setTimeout(() => setCopied(null), 2000)
  }

  return (
    <div
      ref={panel}
      role="region"
      aria-label="Playback info"
      className="pointer-events-auto absolute top-20 left-4 z-30 max-h-[calc(100%-6rem)] w-80 max-w-[calc(100%-2rem)] overflow-y-auto rounded-lg bg-base-950/90 p-3 text-xs text-base-200 ring-1 ring-white/15 backdrop-blur-sm scrollbar-thin"
    >
      <div className="mb-2 flex items-center justify-between gap-2">
        <p className="text-sm font-medium text-white">Playback info</p>
        <div className="flex items-center gap-1">
          <button
            onClick={() => void copy()}
            className="rounded-md px-2 py-1 text-base-300 hover:bg-white/15 hover:text-white pointer-coarse:min-h-11"
          >
            {copied ?? 'Copy details'}
          </button>
          <button
            onClick={onClose}
            aria-label="Close playback info"
            className="grid size-7 place-items-center rounded-md text-base-300 hover:bg-white/15 hover:text-white pointer-coarse:size-11"
          >
            ✕
          </button>
        </div>
      </div>
      <dl className="space-y-1">
        {rows.map(([name, details]) => (
          <div key={name} className="flex gap-2">
            <dt className="w-28 shrink-0 text-base-400">{name}</dt>
            <dd className="min-w-0 break-words tabular-nums">{details}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

const menuRow =
  'mx-1 flex w-[calc(100%-0.5rem)] items-center gap-3 rounded-lg px-2.5 py-2 text-left text-sm transition-colors hover:bg-white/[0.06] pointer-coarse:min-h-11'

function MenuItem({ label, value, onClick, next = false }: { label: string; value: string; onClick: () => void; next?: boolean }) {
  return (
    <button role="menuitem" onClick={onClick} className={cx(menuRow, 'text-base-100')}>
      <span className="shrink-0">{label}</span>
      <span className="ml-auto min-w-0 truncate text-xs text-base-400">{value}</span>
      {next && (
        <span aria-hidden className="-ml-1.5 text-base-500">
          ›
        </span>
      )}
    </button>
  )
}

function MenuToggle({
  label,
  caption,
  on,
  onChange,
}: {
  label: string
  caption?: string
  on: boolean
  onChange: (on: boolean) => void
}) {
  return (
    <button role="menuitemcheckbox" aria-checked={on} onClick={() => onChange(!on)} className={cx(menuRow, 'justify-between text-base-100')}>
      <span className="min-w-0">
        <span className="block">{label}</span>
        {caption && <span className="block text-xs text-base-500">{caption}</span>}
      </span>
      <span aria-hidden className={cx('relative h-5 w-9 shrink-0 rounded-full transition-colors', on ? 'bg-accent-500' : 'bg-base-700')}>
        <span className={cx('absolute top-0.5 left-0.5 size-4 rounded-full bg-white transition-transform', on && 'translate-x-4')} />
      </span>
    </button>
  )
}

function MenuBack({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <>
      <button role="menuitem" onClick={onClick} aria-label={`Back from ${label}`} className={cx(menuRow, 'gap-2 font-semibold text-white')}>
        <span aria-hidden className="text-base-400">
          ‹
        </span>
        {label}
      </button>
      <div aria-hidden className="my-1 border-t border-white/[0.06]" />
    </>
  )
}

type MarkFrom = { kind: 'op' | 'ed'; at: number }

interface Marking {
  from: MarkFrom | null
  setFrom: (from: MarkFrom | null) => void
  save: () => Promise<string>
  forget: () => void
}

const partName = (kind: string) => (kind === 'op' ? 'Opening' : 'Ending')

function markSaved(kind: string, saved: SkipMarkResult): string {
  const lined = { chapters: ", lined up with the file's chapters", sound: ', lined up with the music', '': '' }
  return `${partName(kind)} saved · ${clockTime(saved.start)}–${clockTime(saved.end)}${lined[saved.snapped] ?? ''}`
}

function SkipTimesPanel({ skips, video, marking }: { skips: SkipRange[]; video: HTMLVideoElement | null; marking: Marking }) {
  const [said, setSaid] = useState<{ text: string; error?: boolean } | null>(null)
  const ownMarks = skips.some((r) => r.source === 'marked')
  const { from, setFrom } = marking
  const markStart = (kind: 'op' | 'ed') => {
    if (!video) return
    setFrom({ kind, at: video.currentTime })
    setSaid(null)
  }
  const markEnd = () => {
    setSaid({ text: 'Saving…' })
    return marking.save().then(
      (text) => setSaid(text ? { text } : null),
      (e: Error) => setSaid({ text: e.message, error: true }),
    )
  }
  const name = (kind: string) => partName(kind).toLowerCase()

  return (
    <>
      {skips.length === 0 && <p className="px-3 py-1.5 text-xs text-base-400">None known for this episode.</p>}
      {skips.map((r) => (
        <div key={`${r.kind}-${r.start}`} className="flex items-baseline justify-between gap-3 px-3 py-1.5 text-sm text-base-200">
          <span className="tabular-nums">
            {skipLabel(r.kind)} {clockTime(r.start)}–{clockTime(r.end)}
          </span>
          {r.source && <span className="text-xs text-base-500">{SKIP_SOURCES[r.source] ?? r.source}</span>}
        </div>
      ))}
      <div className="mt-1 border-t border-white/[0.06] pt-1">
        {from ? (
          <>
            <p className="px-3 py-1.5 text-xs text-base-400">
              The {name(from.kind)} starts at {clockTime(from.at)}. Play to where it ends, then:
            </p>
            <MenuRow selected={false} onClick={markEnd}>
              Mark where it ends
            </MenuRow>
            <MenuRow selected={false} onClick={() => setFrom(null)}>
              Cancel
            </MenuRow>
          </>
        ) : (
          <>
            <MenuRow selected={false} onClick={() => markStart('op')}>
              Mark where the opening starts
            </MenuRow>
            <MenuRow selected={false} onClick={() => markStart('ed')}>
              Mark where the ending starts
            </MenuRow>
            {ownMarks && (
              <MenuRow selected={false} onClick={marking.forget}>
                Forget my marks
              </MenuRow>
            )}
          </>
        )}
        {said && <p className={cx('px-3 py-1 text-xs', said.error ? 'text-recap' : 'text-base-300')}>{said.text}</p>}
        <p className="px-3 py-1 text-xs text-base-500 pointer-coarse:hidden">
          Or press {keyHint('markStart')} and {keyHint('markEnd')} as it plays. Marks move onto the music's edges.
        </p>
      </div>
    </>
  )
}

function GearIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0" aria-hidden>
      <circle cx="12" cy="12" r="3" {...stroke} />
      <path
        d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1.08-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1.08 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"
        {...stroke}
      />
    </svg>
  )
}

function MenuRow({
  selected,
  onClick,
  children,
}: {
  selected: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      role="menuitemradio"
      aria-checked={selected}
      onClick={onClick}
      className={cx(menuRow, 'py-1.5', selected ? 'bg-white/[0.08] font-medium text-white' : 'text-base-200')}
    >
      <span className="min-w-0 flex-1 truncate">{children}</span>
      <svg viewBox="0 0 24 24" aria-hidden className={cx('size-4 shrink-0 text-accent-300', !selected && 'invisible')}>
        <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    </button>
  )
}

function CaptionIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0" aria-hidden>
      <rect x="3" y="5" width="18" height="14" rx="2" {...stroke} />
      <path d="M8 11.5a1.6 1.6 0 1 0 0 1.6M15 11.5a1.6 1.6 0 1 0 0 1.6" {...stroke} />
    </svg>
  )
}

const REPEATING = new Set<KeyAction>(['back5', 'forward5', 'back10', 'volumeUp', 'volumeDown', 'subEarlier', 'subLater'])

function useKeyboard({
  video,
  togglePlay,
  seekBy,
  nudge,
  shell,
  extra,
  showVolume,
  shiftSubDelay,
  skip,
  cycleVoice,
  next,
  previous,
  stepRate,
  stepFrame,
  cycleSubtitles,
  toggleInfo,
  holdFast,
  mark,
  sizeSubs,
  theatre,
  off,
}: {
  sizeSubs?: (by: number) => void
  theatre?: () => void
  off?: boolean
  video: HTMLVideoElement | null
  togglePlay: () => void
  seekBy: (n: number) => void
  nudge: () => void
  shell: HTMLElement | null
  extra: Window | null
  showVolume: () => void
  shiftSubDelay?: (by: number) => void
  skip?: () => void
  cycleVoice?: () => void
  next?: () => void
  previous?: () => void
  stepRate: (by: number) => void
  stepFrame: (by: number) => void
  cycleSubtitles?: () => void
  toggleInfo: () => void
  holdFast: (on: boolean) => void
  mark?: (edge: 'start' | 'end') => void
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (off) return
      const target = e.target as HTMLElement | null
      if (target && ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)) return
      if (target?.closest('[role=dialog], [role=menu], [role=listbox], [data-portal-menu]')) return
      if (e.key === ' ' && target?.closest('button, a, [role=switch], [role=tab]') && !shell?.contains(target)) return
      if ((e.ctrlKey || e.metaKey || e.altKey) && !e.getModifierState('AltGraph')) return
      if (keyScope() === 'player' && !(target && shell?.contains(target))) return

      const action = actionFor(e)
      if (!action) {
        if (/^[0-9]$/.test(e.key) && video && Number.isFinite(video.duration)) {
          video.currentTime = (video.duration * Number(e.key)) / 10
          nudge()
        }
        return
      }
      if (e.key.startsWith('Arrow') || e.key === ' ') e.preventDefault()
      if (e.repeat) {
        if (action === 'forward10') holdFast(true)
        if (!REPEATING.has(action)) return
      }

      switch (action) {
        case 'play':
          togglePlay()
          break
        case 'back5':
          seekBy(-5)
          break
        case 'forward5':
          seekBy(5)
          break
        case 'back10':
          seekBy(-10)
          break
        case 'forward10':
          seekBy(10)
          break
        case 'slower':
        case 'faster':
          stepRate(action === 'slower' ? -1 : 1)
          break
        case 'frameBack':
        case 'frameForward':
          stepFrame(action === 'frameBack' ? -1 : 1)
          break
        case 'volumeUp':
        case 'volumeDown':
          if (video) video.volume = Math.min(1, Math.max(0, video.volume + (action === 'volumeUp' ? 0.1 : -0.1)))
          showVolume()
          break
        case 'mute':
          if (video) video.muted = !video.muted
          showVolume()
          break
        case 'fullscreen':
          toggleFullscreen(shell, video)
          break
        case 'subEarlier':
        case 'subLater':
          if (!shiftSubDelay) return
          shiftSubDelay(action === 'subEarlier' ? -DELAY_STEP : DELAY_STEP)
          break
        case 'subtitles':
          if (!cycleSubtitles) return
          cycleSubtitles()
          break
        case 'skip':
          if (!skip) return
          skip()
          break
        case 'voice':
          if (!cycleVoice) return
          cycleVoice()
          break
        case 'next':
          if (!next) return
          next()
          break
        case 'previous':
          if (!previous) return
          previous()
          break
        case 'info':
          toggleInfo()
          break
        case 'markStart':
        case 'markEnd':
          if (!mark) return
          mark(action === 'markStart' ? 'start' : 'end')
          break
        case 'subBigger':
        case 'subSmaller':
          if (!sizeSubs) return
          sizeSubs(action === 'subBigger' ? 1 : -1)
          break
        case 'focus':
          if (!theatre) return
          theatre()
          break
      }
      nudge()
    }
    function onKeyUp(e: KeyboardEvent) {
      if (actionFor(e) === 'forward10') holdFast(false)
    }
    const release = () => holdFast(false)
    window.addEventListener('keydown', onKey)
    window.addEventListener('keyup', onKeyUp)
    window.addEventListener('blur', release)
    extra?.addEventListener('keydown', onKey)
    extra?.addEventListener('keyup', onKeyUp)
    extra?.addEventListener('blur', release)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('keyup', onKeyUp)
      window.removeEventListener('blur', release)
      extra?.removeEventListener('keydown', onKey)
      extra?.removeEventListener('keyup', onKeyUp)
      extra?.removeEventListener('blur', release)
    }
  }, [video, togglePlay, seekBy, nudge, shell, extra, showVolume, shiftSubDelay, skip, cycleVoice, next, previous,
    stepRate, stepFrame, cycleSubtitles, toggleInfo, holdFast, mark, sizeSubs, theatre, off])
}

function IconButton({
  label,
  hint,
  onClick,
  children,
}: {
  label: string
  hint?: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      title={hint ? `${label} (${hint})` : label}
      className="tap relative grid size-9 place-items-center rounded-md text-white/90 transition-colors hover:bg-white/15 hover:text-white"
    >
      {children}
    </button>
  )
}

function RoundButton({
  label,
  onClick,
  live,
  big = false,
  children,
}: {
  label: string
  onClick: () => void
  live: boolean
  big?: boolean
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      tabIndex={live ? undefined : -1}
      className={cx(
        'grid place-items-center rounded-full bg-base-950/60 text-white ring-1 ring-white/15 backdrop-blur-sm transition-transform active:scale-95',
        big ? 'size-16 [&>svg]:size-8' : 'size-14 [&>svg]:size-7',
        live && 'pointer-events-auto',
      )}
    >
      {children}
    </button>
  )
}

function ChevronDownIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-6" aria-hidden>
      <path d="m6 9 6 6 6-6" {...stroke} strokeWidth={2.2} />
    </svg>
  )
}

function MoreIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <circle cx="12" cy="5.5" r="1.7" fill="currentColor" />
      <circle cx="12" cy="12" r="1.7" fill="currentColor" />
      <circle cx="12" cy="18.5" r="1.7" fill="currentColor" />
    </svg>
  )
}

const stroke = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
}

function PauseIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <path d="M8 5h3v14H8zM13 5h3v14h-3z" fill="currentColor" />
    </svg>
  )
}

function SeekIcon({ back }: { back?: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className={cx('size-5', back && 'scale-x-[-1]')} aria-hidden>
      <path d="M4 12a8 8 0 1 1 2.5 5.8" {...stroke} />
      <path d="M4 7v5h5" {...stroke} />
    </svg>
  )
}

function NextIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <path d="M6 5.5v13l9-6.5-9-6.5Z" fill="currentColor" />
      <path d="M17 5v14" {...stroke} strokeWidth={2} />
    </svg>
  )
}

function VolumeIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <path d="M4 9v6h3.5L12 19V5L7.5 9H4Z" fill="currentColor" />
      <path d="M16 9.5a3.5 3.5 0 0 1 0 5M18.5 7a7 7 0 0 1 0 10" {...stroke} />
    </svg>
  )
}

function MutedIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <path d="M4 9v6h3.5L12 19V5L7.5 9H4Z" fill="currentColor" />
      <path d="m16 9.5 5 5M21 9.5l-5 5" {...stroke} />
    </svg>
  )
}

function PiPIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <rect x="3" y="5" width="18" height="14" rx="2" {...stroke} />
      <rect x="12" y="11" width="7" height="6" rx="1" fill="currentColor" />
    </svg>
  )
}

function FullscreenIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <path d="M4 9V5h4M20 9V5h-4M4 15v4h4M20 15v4h-4" {...stroke} />
    </svg>
  )
}

function CastIcon({ on }: { on: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <path d="M3 9V7a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2h-6" {...stroke} />
      <path d="M3 13a6 6 0 0 1 6 6M3 16.5A2.5 2.5 0 0 1 5.5 19" {...stroke} strokeLinecap="round" />
      <circle cx="3.5" cy="19" r="0.5" fill="currentColor" />
      {on && <rect x="7" y="8" width="11" height="8" rx="1" fill="currentColor" opacity="0.6" />}
    </svg>
  )
}

function AirPlayIcon({ on }: { on: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
      <path d="M7 17H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2h-2" {...stroke} />
      <path d="M12 15l5 6H7z" fill="currentColor" />
      {on && <rect x="6" y="7" width="12" height="6" rx="1" fill="currentColor" opacity="0.6" />}
    </svg>
  )
}

function usePopSheet(anchor: RefObject<HTMLElement | null>, open: boolean, close: () => void) {
  const wide = useMediaQuery('(min-width: 40rem)')
  const room = useRoomAbove(anchor, open && wide)
  return {
    wide,
    backdrop: open && !wide ? <div aria-hidden className="fixed inset-0 z-50 bg-black/50" onClick={close} /> : null,
    style: { maxHeight: wide ? room : '70vh' },
    className: (width: string, pad: string) =>
      cx(
        'z-50 animate-rise overflow-y-auto bg-base-750 shadow-xl shadow-black/60 ring-1 ring-white/[0.06] scrollbar-thin',
        pad,
        wide
          ? cx('absolute right-0 bottom-full mb-2 origin-bottom-right rounded-lg', width)
          : 'fixed inset-x-0 bottom-0 rounded-t-xl pb-[max(0.5rem,env(safe-area-inset-bottom))]',
      ),
  }
}

type Need = { label: string; met: boolean | null; detail?: ReactNode }

function CastButton({
  casting,
  device,
  phase,
  sdkState,
  onCast,
  onStop,
  onOpen,
}: {
  casting: boolean
  device: string
  phase: CastPhase
  sdkState: CastSdk
  onCast: () => void
  onStop: () => void
  onOpen: (open: boolean) => void
}) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const { backdrop, style: sheetStyle, className: sheetClass } = usePopSheet(ref, open, close)
  useEffect(() => {
    onOpen(open)
    return () => onOpen(false)
  }, [open, onOpen])
  const access = useAccess()
  const openUp = useMutation({
    mutationFn: () => api.post('/api/access/network', { lan: true }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['access'] }),
  })

  const secure = window.isSecureContext
  const reachable = access.data?.reachable ?? null
  const ready = secure && sdkState === 'ok' && reachable === true && phase !== 'none'
  const https = access.data?.urls?.find((u) => u.startsWith('https://'))
  const httpsHere = () => {
    const u = new URL(https!)
    u.pathname = location.pathname
    new URLSearchParams(location.search).forEach((v, k) => u.searchParams.set(k, v))
    return u.toString()
  }

  const needs: Need[] = [
    {
      label: 'A browser that can cast',
      met: !secure || sdkState === 'loading' ? null : sdkState === 'ok',
      detail: !secure
        ? undefined
        : sdkState === 'off'
          ? isBrave()
            ? "Brave has casting turned off: turn on Media Router in Brave's Settings, Extensions, then restart Brave."
            : "This browser can't cast. Chrome can, on a computer or an Android phone."
          : sdkState === 'failed'
            ? "Casting didn't load. Check this device is online, then reload the page."
            : undefined,
    },
    {
      label: 'A secure address',
      met: secure,
      detail: secure ? undefined : https ? (
        <a href={httpsHere()} className="text-accent-300 underline underline-offset-2">Open this page at the HTTPS address</a>
      ) : (
        'Casting needs HTTPS. Set it up on the PC running Yoru, in Settings, Devices.'
      ),
    },
    {
      label: 'Access from other devices',
      met: reachable,
      detail: reachable !== false ? undefined : access.data?.canSwitch ? (
        <span className="flex flex-wrap items-center gap-2">
          The TV plays from Yoru over your network, which only this PC can reach now.
          <button
            type="button"
            onClick={() => openUp.mutate()}
            disabled={openUp.isPending}
            className={buttonClass('secondary', 'sm')}
          >
            {openUp.isPending ? 'Turning on…' : 'Turn on'}
          </button>
          {openUp.isError && <span className="text-recap">{(openUp.error as Error).message}</span>}
        </span>
      ) : (
        'The TV plays from Yoru over your network: turn this on in Settings, Devices.'
      ),
    },
    {
      label: 'A TV on this Wi-Fi',
      met: sdkState !== 'ok' ? null : phase !== 'none',
      detail:
        sdkState === 'loading' ? (
          'Looking…'
        ) : sdkState === 'ok' && phase === 'none' ? (
          <span className="flex flex-wrap items-center gap-2">
            None found yet: a Chromecast or Google TV, switched on and on the same Wi-Fi as this device.
            <button
              type="button"
              onClick={() => {
                setOpen(false)
                onCast()
              }}
              className={buttonClass('secondary', 'sm')}
            >
              Look for TVs
            </button>
          </span>
        ) : undefined,
    },
  ]

  return (
    <div className="sm:relative" ref={ref}>
      <IconButton
        label={casting ? `Stop casting to ${device || 'the TV'}` : 'Cast to a TV'}
        onClick={() => (casting ? onStop() : ready ? onCast() : setOpen((v) => !v))}
      >
        <CastIcon on={casting} />
      </IconButton>
      {backdrop}
      {open && (
        <div
          role="dialog"
          aria-label="Cast to a TV"
          style={sheetStyle}
          className={sheetClass('w-80 max-w-[calc(100vw-2rem)]', 'p-3 text-sm')}
        >
          <p className="font-medium text-white">Cast to a TV</p>
          <p className="mt-0.5 text-xs text-base-400">
            {ready ? 'Everything is ready.' : 'Casting needs all of these:'}
          </p>
          <ul className="mt-2 space-y-2">
            {needs.map((n) => (
              <li key={n.label} className="flex gap-2">
                <NeedMark met={n.met} />
                <div className="min-w-0">
                  <p className={n.met === false ? 'text-white' : 'text-base-300'}>{n.label}</p>
                  {n.detail && <div className="mt-0.5 text-xs text-base-400">{n.detail}</div>}
                </div>
              </li>
            ))}
          </ul>
          {ready && (
            <button
              type="button"
              onClick={() => { close(); onCast() }}
              className={cx(buttonClass('primary', 'sm'), 'mt-3 w-full')}
            >
              Choose a TV
            </button>
          )}
        </div>
      )}
    </div>
  )
}

function NeedMark({ met }: { met: boolean | null }) {
  return (
    <span
      role="img"
      aria-label={met ? 'Done' : met === false ? 'Missing' : 'Not known yet'}
      className={cx(
        'mt-0.5 grid size-5 shrink-0 place-items-center rounded-full text-xs leading-none',
        met ? 'bg-emerald-400 text-base-950' : met === false ? 'ring-1 ring-recap text-recap' : 'ring-1 ring-base-600',
      )}
    >
      {met ? '✓' : met === false ? '!' : ''}
    </span>
  )
}

function CastingPanel({
  device,
  time,
  duration,
  paused,
  onPlayPause,
  onSeek,
  onStop,
}: {
  device: string
  time: number
  duration: number
  paused: boolean
  onPlayPause: () => void
  onSeek: (to: number) => void
  onStop: () => void
}) {
  return (
    <div className="absolute inset-0 z-20 flex flex-col items-center justify-center gap-4 bg-black/85 p-6 text-center">
      <CastIcon on />
      <p className="text-sm text-white/80">
        Playing on <span className="font-semibold text-white">{device || 'your TV'}</span>
      </p>
      <button
        onClick={onPlayPause}
        aria-label={paused ? 'Play on the TV' : 'Pause on the TV'}
        className="grid size-14 place-items-center rounded-full bg-white/15 text-white ring-1 ring-white/25 hover:bg-white/25"
      >
        {paused ? <PlayIcon /> : <span className="text-xl font-bold">❚❚</span>}
      </button>
      <div className="flex w-full max-w-md items-center gap-3 text-xs tabular-nums text-white/80">
        <span>{clockTime(time)}</span>
        <input
          type="range"
          min={0}
          max={Math.max(duration, 1)}
          step={1}
          value={Math.min(time, duration)}
          onChange={(e) => onSeek(Number(e.target.value))}
          aria-label="Seek on the TV"
          className="h-11 flex-1 accent-accent-500"
        />
        <span>{clockTime(duration)}</span>
      </div>
      <button onClick={onStop} className={buttonClass('secondary', 'md')}>
        Stop casting
      </button>
    </div>
  )
}
