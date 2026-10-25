import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react'
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
import { menuItems, useDismiss, useMediaQuery, useMenuButton, useRoomAbove, walkMenu } from '../lib/hooks'
import { buttonClass } from '../lib/ui'
import { copyText } from '../lib/downloads'
import { actionFor, keyHint, keyScope, useKeymap, type KeyAction } from '../lib/keymap'
import { getSleep, noteInput, setSleep, useSleep, type Sleep } from '../lib/watching'
import { useAnime4K, type UpscaleTier } from './anime4k'
import { useTouchGestures } from './gestures'
import { castImpossible, isBrave, useCast, type CastPhase, type CastSdk } from './cast'
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
import { castStyle, SUB_LOOKS, subLookLabel, type SubLook } from './subLook'

// An iPhone has no element fullscreen, only the video's own.
function toggleFullscreen(shell: HTMLElement | null, video: HTMLVideoElement | null) {
  if (document.fullscreenElement) {
    void document.exitFullscreen().catch(() => {})
  } else if (shell?.requestFullscreen) {
    // A wide picture turns the phone sideways; a browser that can't lock (a desktop's) says no, which is fine.
    void shell
      .requestFullscreen()
      .then(() => {
        const o = screen.orientation as ScreenOrientation & { lock?: (to: string) => Promise<void> }
        if (video && video.videoWidth > video.videoHeight) return o?.lock?.('landscape')
      })
      .catch(() => {})
  } else {
    ;(video as (HTMLVideoElement & { webkitEnterFullscreen?: () => void }) | null)?.webkitEnterFullscreen?.()
  }
}

// A source swap loads the element paused with no pause event; what was playing resumes once it can.
function resumeAfterRebuild(video: HTMLVideoElement | null, wasPlaying: boolean) {
  if (!video || !wasPlaying) return
  video.addEventListener('canplay', () => void video.play().catch(() => {}), { once: true })
}

// The settings' ISO 639-1 codes, and what a file's track tags say instead.
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
// A signs-and-songs track by default looks like broken subtitles, not the wrong track.
const SIGNS = /\b(signs?|songs?|s&s|forced)\b/i

/** The track to show: first preferred language, dialogue over signs; the file's default flag comes last. */
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

/** With a dub the viewer understands, dialogue subtitles repeat it: signs and songs only, or none. */
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

// Volume, mute, speed and subtitle size carry over; the element alone starts at full volume.
const PERSIST_KEY = 'yoru.player'
interface Persisted {
  volume?: number
  muted?: boolean
  rate?: number
  subScale?: number
}

const VOLUME_NOTICE = 'volume'
const SUB_SCALES = [0.75, 0.9, 1, 1.15, 1.3, 1.5]
const DELAY_STEP = 0.1
// The longest an episode's first frame waits for its subtitles, ms.
const SUBTITLE_WAIT = 5000
// Tracks from outside the file take indexes no stream has: a loaded file, and Jimaku's n at JIMAKU_TRACK - n.
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
    // Storage may be unavailable; the defaults are fine.
  }
}

export interface PlayerProps {
  stream?: StreamInfo
  /** What the episode is waiting on before its stream opens. */
  starting?: { title: string; detail: string }
  startAt: number
  skips: SkipRange[]
  autoSkip: AutoSkip
  /** AniSkip hears whether its times were right; absent unless sharing with it is on. */
  onSkipVote?: (id: string, up: boolean) => void
  /** Keeps the viewer's own opening or ending for this episode, or forgets them. */
  skipMarks?: { mark: (kind: 'op' | 'ed', start: number, end: number) => Promise<SkipMarkResult>; forget: () => void }
  autoPlay: boolean
  /** Subtitle languages in preference order, from settings. */
  subLanguages?: string[]
  /** What shows with a dub in one of those languages: signs and songs only, nothing, or as with sub. */
  withDub?: 'signs' | 'off' | 'same'
  /** How dialogue is drawn, from settings; onSubLook changes it there. */
  subLook?: SubLook
  onSubLook?: (look: SubLook) => void
  /** A subtitle delay kept for this show's releases from the playing one's group. */
  keptDelay?: number
  /** Keeps a delay for the rest of the show; absent when the release names no group. */
  onKeepDelay?: (delay: number) => void
  /** onSlow is set only under Auto: a tier picked by hand is left alone. */
  upscale?: { enabled: boolean; mode: string; tier?: UpscaleTier; onSlow?: () => void }
  /** The sound settings, and where a change made here is remembered. */
  sound?: { options: SoundOptions; onChange: (next: SoundOptions) => void }
  title: string
  subtitle?: string
  /** The show's cover, for the system's media controls. */
  artwork?: string
  /** played is media time that actually played since the last report. */
  /** final: the episode's last report (its end, Next, leaving the page), worth waiting on. */
  onProgress: (position: number, duration: number, played: number, final: boolean, paused: boolean) => void
  onEnded: () => void
  /** by: which control asked, for the log. */
  onNext?: (by: 'button' | 'key' | 'media') => void
  /** The episode before, absent on the first. */
  onPrevious?: () => void
  /** Playing or seeking again, which after the end means the viewer went back. */
  onResume?: (by: 'play' | 'seek') => void
  /** Sat at the very end without ending, so no countdown comes; told once per stream. */
  onEndStuck?: (detail: Record<string, unknown>) => void
  /** The server no longer knows the stream; reopen it under the same URLs. */
  onSessionLost?: () => Promise<unknown>
  /** This device can't decode what was copied for it; true when it reopens converted. */
  onUndecodable?: () => boolean
  /** The stream keeps stalling for want of bandwidth; present only where a lower quality may be taken. */
  onSlowLink?: () => void
  /** Opens the release picker, offered when this release won't play. */
  onPickRelease?: () => void
  /** Drawn inside the player, so it survives fullscreen and picture in picture. */
  overlay?: ReactNode
  /** Casting draws the chosen subtitles into the picture instead of sending them as text. */
  castBurn?: boolean
}

export function Player({
  stream,
  starting,
  startAt,
  skips,
  autoSkip,
  onSkipVote,
  skipMarks,
  autoPlay,
  subLanguages,
  withDub = 'signs',
  keptDelay,
  onKeepDelay,
  subLook: lookPreset = '',
  onSubLook,
  upscale,
  sound,
  title,
  subtitle,
  artwork,
  onProgress,
  onEnded,
  onNext,
  onPrevious,
  onResume,
  onEndStuck,
  onSessionLost,
  onUndecodable,
  onSlowLink,
  onPickRelease,
  overlay,
  castBurn = false,
}: PlayerProps) {
  const [video, setVideo] = useState<HTMLVideoElement | null>(null)
  const [canvas, setCanvas] = useState<HTMLCanvasElement | null>(null)
  const qc = useQueryClient()
  const shell = useRef<HTMLDivElement>(null)
  // The player renders into this through a portal, so picture in picture can
  // move it to the floating window with its event handlers still attached.
  const [host] = useState(() => document.createElement('div'))
  const anchor = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    anchor.current?.append(host)
    return () => host.remove()
  }, [host])

  const [playing, setPlaying] = useState(false)
  const [waiting, setWaiting] = useState(true)
  const [time, setTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [volume, setVolume] = useState(1)
  const [muted, setMuted] = useState(false)
  const [rate, setRate] = useState(() => loadPersisted().rate ?? 1)
  // The 10-second key held: 2× until let go, the chosen speed kept for after.
  const fast = useRef(false)
  const [controlsVisible, setControlsVisible] = useState(true)
  // A key changed something with nothing else on screen; say where it landed.
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
  // Per episode: a delay fixes one file's timing, unless one was kept for the show's releases from this group.
  const [subDelay, setSubDelay] = useState(0)
  useEffect(() => {
    setSubDelay(keptDelay ?? 0)
    if (keptDelay) showNotice(`Subtitle delay ${delayLabel(keptDelay)} (kept for this show)`, 2500)
  }, [stream?.id, keptDelay, showNotice])
  // Said, not left to look like a release with none: the browser can't draw subtitles that are pictures.
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

  // Restore the last episode's settings; a new source resets the rate, so it's reapplied each load.
  useEffect(() => {
    if (!video) return
    const saved = loadPersisted()
    if (typeof saved.volume === 'number') video.volume = Math.min(1, Math.max(0, saved.volume))
    if (typeof saved.muted === 'boolean') video.muted = saved.muted
    // A held L's 2× outlasts the seek and buffering it set off.
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

  // After an audio switch or recovery, the epoch forces a fresh HLS instance so old segments aren't mixed in.
  const [audioTrack, setAudioTrack] = useState(0)
  const [soundState, setSoundState] = useState<SoundOptions>(sound?.options ?? DEFAULT_SOUND)
  const [epoch, setEpoch] = useState(0)
  const [resumeAt, setResumeAt] = useState(startAt)
  // The session could not be reopened after the reaper took it.
  const [lostError, setLostError] = useState<string | null>(null)

  // Reset during render: the HLS effect runs first and would attach at the last episode's position.
  const [attached, setAttached] = useState(stream?.id)
  if (stream?.id !== attached) {
    setAttached(stream?.id)
    setAudioTrack(stream?.audioTrack ?? 0)
    setSoundState(stream?.sound ? soundFrom(stream.sound) : (sound?.options ?? DEFAULT_SOUND))
    setEpoch(0)
    setResumeAt(startAt)
    // Or the scrubber reads the last episode's length and a skip button for its ranges sits over the new one.
    setTime(0)
    setDuration(0)
    // Or a new episode sits on a paused frame while it buffers.
    setWaiting(true)
    setLostError(null)
  }

  const playlist = stream?.playlist
    ? `${stream.playlist}${epoch ? `?a=${epoch}` : ''}`
    : undefined

  // A switch's answer arriving after the episode changed belongs to the old one.
  const liveId = useRef(stream?.id)
  liveId.current = stream?.id
  // Set on any pick, even track 0, or a rebuilt session's sub/dub pick undoes it.
  const audioChosen = useRef(false)
  const switchAudio = useCallback(
    (index: number) => {
      const id = stream?.id
      if (!id || index === audioTrack) return
      audioChosen.current = true
      const before = audioTrack
      const playing = !!video && !video.paused && !video.ended
      setAudioTrack(index)
      // Rebuilt on the server's yes: a refused switch keeps the old track, and the menu says so.
      void api.post(`/api/stream/${id}/audio?track=${index}`).then(
        () => {
          if (liveId.current !== id) return
          // Loading the new source leaves the element paused without a pause event.
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

  // Same shape as a track switch: the pass restarts where the viewer is, and the change is remembered.
  const switchSound = useCallback(
    (patch: Partial<SoundOptions>) => {
      const id = stream?.id
      const next = { ...soundState, ...patch }
      if (!id || sameSound(next, soundState)) return
      const before = soundState
      const playing = !!video && !video.paused && !video.ended
      setSoundState(next)
      // Rebuilt and remembered on the server's yes, as a track switch is.
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

  // Filled below once hls exists; keepalive and fatal 404 can notice together, so one reopen at a time.
  const recoverRef = useRef<() => void>(() => {})
  const recovering = useRef(false)
  const recover = useCallback(() => recoverRef.current(), [])

  const undecodable = useCallback(() => {
    if (!onUndecodable?.()) return false
    showNotice("This device couldn't play the original, so it's converted instead", 6000)
    return true
  }, [onUndecodable, showNotice])
  const { error, final, hls, streamEnded } = useHlsSource(video, playlist, resumeAt, recover, undecodable)
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
        // The rebuilt session picks the track from the sub/dub preference, so a
        // mid-episode switch has to be asked for again or the menu lies.
        if (audioChosen.current && stream?.id) {
          void api.post(`/api/stream/${stream.id}/audio?track=${track}`).catch(() => {})
        }
        if (!sameSound(snd, DEFAULT_SOUND) && stream?.id) {
          void api.post(`/api/stream/${stream.id}/sound${soundQuery(snd)}`).catch(() => {})
        }
        setResumeAt(video?.currentTime ?? startAt)
        setWaiting(true)
        setEpoch((e) => e + 1)
        // The rebuild resets the element to paused; only autoplay restarts it.
        resumeAfterRebuild(video, wasPlaying)
      })
      // Rebuilding against a session that failed to reopen only loops.
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
  // Tracks from outside the file, for this episode only: one loaded from this device, and Jimaku's.
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
  // A loaded file stays in memory until its blob: URL is revoked: when replaced, its episode gone, unmounted.
  const blobs = useRef(new Set<string>())
  useEffect(() => {
    const live = new Set(extraTracks.map((t) => t.url).filter((url) => url.startsWith('blob:')))
    for (const url of blobs.current) if (!live.has(url)) URL.revokeObjectURL(url)
    blobs.current = live
  }, [extraTracks])
  useEffect(() => () => blobs.current.forEach((url) => URL.revokeObjectURL(url)), [])
  // Picture in picture moves the player to another document, so the subtitle canvas is rebuilt there.
  const subLook = useMemo(() => ({ scale: subScale, delay: subDelay, preset: lookPreset }), [subScale, subDelay, lookPreset])
  const subtitlesReady = useSubtitles(video, subtitleStream, track, fonts, pip.active, subLook, (problem) =>
    showNotice(problem, 6000),
  )
  // The system's floating window and an iPhone's fullscreen show only text tracks.
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
  const jimakuSet = useQuery({ queryKey: ['jimaku'], queryFn: () => api.get<{ set: boolean }>('/api/jimaku') }).data?.set
  // A skip of AniSkip's left standing is a vote that it was right.
  const autoSkipped = useAutoSkip(video, skips, autoSkip, (r) => {
    if (r.source === 'aniskip' && r.id) onSkipVote?.(r.id, true)
  })

  const upscaleState = useAnime4K({
    video,
    canvas,
    // Not while floating: the upscaler's canvas is bound to the document it was configured in.
    enabled: !!upscale?.enabled && !pip.active,
    mode: upscale?.mode ?? 'A',
    tier: upscale?.tier ?? 'full',
    onSlow: upscale?.onSlow,
    // The playlist, not the session id, which stays when another release is picked.
    source: playlist,
  })
  // Only worth showing while it holds the current frame; seeking or a stopped
  // renderer leaves it stale, and the plain video underneath should show.
  const painted = useFreshFrames(video, upscaleState === 'on')
  const showUpscaled = upscaleState === 'on' && painted

  // Keyed on the tracks, not the session id, which a recovery and a hand-picked release both keep.
  const trackList = (stream?.subtitles ?? [])
    .map((s) => `${s.index}:${s.language ?? ''}:${s.title ?? ''}`)
    .join('|')
  // Chosen again when the audio changes language: a switch to the dub takes the dialogue subtitles away.
  const audioLanguage = stream?.audio?.[audioTrack]?.language ?? ''
  // The track list the choice below was made for: until then, "no track" only means not chosen yet.
  const [chosenFor, setChosenFor] = useState<string>()
  useEffect(() => {
    // Text before pictures of the same language: pictures wait for the whole download.
    const tracks = [...(stream?.subtitles ?? [])].sort((a, b) => Number(!!a.pictures) - Number(!!b.pictures))
    setTrack(tracks.length ? (chooseWithDub(tracks, subLanguages ?? ['en'], audioLanguage, withDub)?.index ?? null) : null)
    setChosenFor(trackList)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [trackList, audioLanguage, withDub])

  // An episode can open on dialogue, so its first frame waits for the subtitles to draw; never for long.
  const [subtitleWaitOver, setSubtitleWaitOver] = useState<string>()
  useEffect(() => {
    if (!stream?.id) return
    const id = stream.id
    const timer = window.setTimeout(() => setSubtitleWaitOver(id), SUBTITLE_WAIT)
    return () => clearTimeout(timer)
  }, [stream?.id])
  // Latched per stream: a track switched later never pauses what is playing.
  const [subtitlesFor, setSubtitlesFor] = useState<string>()
  const subtitlesForRef = useRef(subtitlesFor)
  subtitlesForRef.current = subtitlesFor
  const subtitlesDrawable = chosenFor === trackList && subtitlesReady
  useEffect(() => {
    if (stream?.id && (subtitlesDrawable || subtitleWaitOver === stream.id)) setSubtitlesFor(stream.id)
  }, [stream?.id, subtitlesDrawable, subtitleWaitOver])
  const subtitlesSettled = !!stream?.id && subtitlesFor === stream.id
  // Played by hand meanwhile: the wait is over, and a pause after that is the viewer's to keep.
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

  // The reused element's autoplay fires once; a ref, so flipping it mid-episode can't undo a pause.
  const autoPlayRef = useRef(autoPlay)
  autoPlayRef.current = autoPlay
  const holdingStart = autoPlay && !!stream?.id && !subtitlesSettled
  useEffect(() => {
    if (!video || !stream?.id || !autoPlayRef.current || !subtitlesSettled || playedByHand.current === stream.id) return
    const start = () => void video.play().catch(() => {})
    // Straight away; waiting on canplay left the next episode still for seconds.
    start()
    // Asked again only if it couldn't play yet: a canplay left armed fired on the next seek, playing a pause.
    if (video.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA) return
    video.addEventListener('canplay', start, { once: true })
    return () => video.removeEventListener('canplay', start)
  }, [video, stream?.id, subtitlesSettled])

  const reportRef = useRef(onProgress)
  reportRef.current = onProgress
  const endedRef = useRef(onEnded)
  endedRef.current = onEnded

  // Reports on a timer with the time really played (a big step is a seek); per stream, keeping the last
  // known position, since the source is torn down by the time Next or leaving sends it.
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
    const id = window.setInterval(() => {
      if (!video.paused && duration > 0) report(position)
    }, 10_000)

    // On pause and on play alike, so the Discord card turns over at once.
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
      if (duration > 0) report(position, true)
    }
  }, [video, stream?.id])

  // Casting: the TV plays from Yoru itself through a link signed for this session; the video here waits, and
  // carries on where the TV was when casting stops.
  const { phase: castPhase, sdkState: castSdk, device: castDevice, remote, playing: castPlaying, start: startCast,
    stop: stopCast, playPause: castPlayPause, seek: castSeek } = useCast()
  // This stream on the TV, not just a session: another episode, or another page's, leaves this one alone.
  const casting = castPhase === 'connected' && !!stream?.id && castPlaying === stream.id
  const castTo = useCallback(async () => {
    if (!stream?.id || !video) return
    // A failed try's words must not sit over the cast that works.
    setNotice(null)
    try {
      // Drawn in, the TV gets the track shown here, with this episode's delay; pictures it can't draw itself.
      const chosen = stream.subtitles.find((t) => t.index === track)
      const burn = chosen && (castBurn || chosen.pictures) ? `?burn=${track}&delay=${subDelay}` : ''
      const link = await api.post<{
        url: string
        subtitles: Array<{ index: number; language?: string; title?: string; url: string }>
        note?: string
      }>(`/api/stream/${stream.id}/cast${burn}`)
      if (link.note) showNotice(link.note, 6000)
      // A file from this device or Jimaku's lives in this page; the TV can't fetch it.
      else if (track != null && !chosen) showNotice("The TV shows the episode's own subtitles; loaded ones stay here", 6000)
      const shown = track == null ? -1 : link.subtitles.findIndex((s) => s.index === track)
      video.pause()
      await startCast({
        key: stream.id, url: link.url, title: title ?? '', subtitle, artwork, startAt: video.currentTime,
        subtitles: link.subtitles, subtitleTrack: shown >= 0 ? shown : undefined,
        textStyle: castStyle(lookPreset, subScale),
      })
    } catch (e) {
      // Closing Chrome's device list rejects with "cancel": nothing went wrong.
      const message = e instanceof Error ? e.message : String(e)
      if (!/cancel/i.test(message)) showNotice(`Couldn't cast: ${message}`, 6000)
    }
  }, [stream, video, track, title, subtitle, artwork, startCast, showNotice, castBurn, subDelay, lookPreset, subScale])
  useEffect(() => {
    if (casting) video?.pause()
  }, [casting, video])
  // The TV's position goes to the list as the video's would, the time really played counted the same way.
  // Only while it plays this stream: what it shows after moving on is another episode's.
  const remoteRef = useRef(remote)
  if (casting) remoteRef.current = remote
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
      // Carried on here where the TV was, unless this player has moved to another episode meanwhile.
      if (video && r.time > 0 && liveId.current === sid) video.currentTime = r.time
      if (r.duration > 0) reporter(r.time, r.duration, played, false, true)
    }
  }, [casting, stream?.id, video])

  // Still for 5 s in the last segment (6 s, 7 with a sliver; a seek that never lands counts), not ended: no
  // countdown will come, so the log is told, and a stall at the very end is ended here as a real end would be.
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
      // The last segment in and played, under 2 s left: a browser that never ends it (a shorter picture
      // track) is ended here; a last segment still on its way is waited for.
      const assumed = streamEnded.current && d - t < 2 && bufferedEnd - t < 0.5
      // planned is the server's length: a player duration far past it means hls.js shifted its timeline.
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

  // The system's media controls (media keys, a headset button, the lock screen) show the episode and drive it.
  const onNextRef = useRef(onNext)
  onNextRef.current = onNext
  const hasNext = !!onNext
  // Steady, so the keyboard's listener isn't rebuilt each render; N presses the same button.
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
        // A headset button or media key is someone there, as a key on the page is.
        session.setActionHandler(action, handler && ((d) => (noteInput(), handler(d))))
      } catch {
        // An action this browser doesn't know.
      }
    }
    return () => {
      for (const [action] of handlers) {
        try {
          session.setActionHandler(action, null)
        } catch {
          // As above.
        }
      }
      session.metadata = null
    }
  }, [video, title, subtitle, artwork, seekBy, hasNext, hasPrevious])

  // The lock screen's scrubber follows the episode; it can't place itself on a stream it never saw load.
  useEffect(() => {
    if (!video || !('mediaSession' in navigator) || !navigator.mediaSession.setPositionState) return
    const update = () => {
      const d = video.duration
      if (!Number.isFinite(d) || d <= 0) return
      try {
        navigator.mediaSession.setPositionState({ duration: d, position: Math.min(video.currentTime, d), playbackRate: video.playbackRate || 1 })
      } catch {
        // A position past a duration still settling.
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

  // The sleep timer gone off pauses, which lets the screen sleep too: the wake lock below goes with playing.
  const sleep = useSleep()
  useEffect(() => {
    if (sleep.kind === 'fired' && video && !video.paused) video.pause()
  }, [sleep, video])

  // The screen stays on while an episode plays, muted too, which the browser's own rule leaves to dim.
  useEffect(() => {
    if (!playing || !('wakeLock' in navigator)) return
    let lock: WakeLockSentinel | null = null
    let gone = false
    const take = () => {
      if (document.visibilityState !== 'visible' || (lock && !lock.released)) return
      navigator.wakeLock.request('screen').then(
        (l) => (gone ? void l.release() : (lock = l)),
        () => {
          // Refused: battery saver, or a page not over HTTPS.
        },
      )
    }
    take()
    // A hidden page loses its lock; coming back takes it again.
    document.addEventListener('visibilitychange', take)
    return () => {
      gone = true
      document.removeEventListener('visibilitychange', take)
      void lock?.release()
    }
  }, [playing])

  // Only a slow seek earns a spinner; otherwise every arrow press flashes one.
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

  // On touch, a double tap on either third of the picture seeks ten seconds
  // that way; a single tap still toggles play. Mouse clicks are unchanged.
  const lastTap = useRef<{ at: number; x: number }>({ at: 0, x: 0 })
  const touchedAt = useRef(0)
  // Read as the finger lands, before the touch shows the controls.
  const controlsShown = useRef(controlsVisible)
  controlsShown.current = controlsVisible
  const hiddenAtTouch = useRef(false)
  const [flash, setFlash] = useState<'back' | 'forward' | null>(null)
  // A tap on a side third waits for a second, or every double-tap seek pauses.
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
      const prev = lastTap.current
      lastTap.current = { at: now, x }
      if (now - prev.at < 300 && Math.abs(prev.x - x) < 0.2 && side) {
        window.clearTimeout(pendingTap.current)
        const dir = x < 0.35 ? 'back' : 'forward'
        seekBy(dir === 'back' ? -10 : 10)
        setFlash(dir)
        window.setTimeout(() => setFlash(null), 500)
        lastTap.current = { at: 0, x }
        return
      }
      // A tap on a bare picture brings the controls up without pausing, as phone players do.
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
  // The click that follows a touch is the same gesture, already handled.
  const onSurfaceClick = useCallback(() => {
    if (performance.now() - touchedAt.current < 700) return
    togglePlay()
  }, [togglePlay])

  // Hide the controls while playing, but never while paused: a paused player with no controls looks broken.
  const hideTimer = useRef<number | undefined>(undefined)
  // What keeps the bar up while playing: a panel being read (Cast's needs, a menu).
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

  const activeSkip = skips.find((r) => time >= r.start && time < r.end)
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
  // A frame of the usual 23.976 fps; stepping plays nothing, so the episode pauses first.
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
  // Rendered again when the shortcuts list changes a key, so the tooltips say the new one.
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
  // Two fingers spread: the picture fills the screen, its edges cropped, as a phone's own player does.
  const [fill, setFill] = useState(false)
  const scrubFrom = useRef<number | null>(null)
  const gestures = useTouchGestures(
    {
      hold: holdFast,
      scrub: (seconds, done) => {
        // No length yet (still loading, or a rebuilt session): nowhere to drag to.
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
        if (down) toggleFullscreen(shell.current, video)
      },
    },
    // A sideways drag across the whole picture moves two minutes.
    { fullscreen, secondsAcross: 120 },
  )
  const onSurfacePointerUp = (e: React.PointerEvent<HTMLElement>) => {
    gestures.handlers.onPointerUp(e)
    if (e.pointerType === 'touch' && gestures.used()) {
      // The click a touch brings after it is the same gesture, done.
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
    // A held finger is 2×, not the browser's menu for the video.
    onContextMenu: (e: React.MouseEvent) => gestures.touching() && e.preventDefault(),
    // Vertical drags still scroll the page; in fullscreen there is none, and a drag down leaves it.
    style: { touchAction: fullscreen ? 'none' : 'pan-y' },
  } as const

  // Outlives the menu, shut while playing to the part's end; tied to the marks, so a new episode starts clean.
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
  // [ in the first half is the opening's start, in the second the ending's; ] saves the part it began.
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
        // Lining it up with the music can take a few seconds on a slow swarm.
        showNotice('Saving…', 10_000)
        saveMark().then(
          (said) => showNotice(said, 5000),
          (err: Error) => showNotice(`Couldn't save it: ${err.message}`, 5000),
        )
      }
    },
    [video, markFrom, setMarkFrom, saveMark, showNotice],
  )

  useKeyboard({
    video,
    togglePlay,
    seekBy,
    nudge,
    shell: shell.current,
    extra: pip.window,
    showVolume,
    shiftSubDelay: track === null ? undefined : shiftSubDelay,
    skip: activeSkip ? skipActive : undefined,
    // Only where the control shows: a file with no audio has nothing to lift.
    cycleVoice: (stream?.audio?.length ?? 0) > 0 ? cycleVoice : undefined,
    next: hasNext ? goNext : undefined,
    previous: hasPrevious ? goPrevious : undefined,
    stepRate,
    stepFrame,
    cycleSubtitles: subtitleTracks.length > 0 ? cycleSubtitles : undefined,
    toggleInfo,
    holdFast,
    mark: skipMarks ? markKey : undefined,
  })

  // How long the picture has been stuck, so a slow swarm can say so rather than looking like a hang.
  const [stalledFor, setStalledFor] = useState(0)
  // Carried over, it declares the next episode too slow before it has loaded.
  useEffect(() => setStalledFor(0), [stream?.id])

  // Numbers for the stall message: a swarm too slow for the bitrate is a download, not a wait.
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
  // Focus to Try again when the error shows, unless it would leave a box being typed in elsewhere.
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
    // Paused is not stuck: counting on would start the health poll, and that keeps the session alive.
    const id = window.setInterval(() => setStalledFor((s) => (stream && video?.paused ? s : s + 1)), 1000)
    return () => window.clearInterval(id)
  }, [waiting, stream, video])

  // Floating, the player fills its window; fullscreen is CSS, since `:fullscreen` never matches there.
  const corners = pip.active ? '' : 'sm:rounded-xl'
  // Buttons on the bar, rows in the settings menu on a phone; the floating window has neither.
  const audioMenu =
    !pip.active && stream?.audio && stream.audio.length > 1
      ? { tracks: stream.audio, value: audioTrack, onChange: switchAudio }
      : undefined
  const soundMenu =
    !pip.active && stream && (stream.audio?.length ?? 0) > 0 ? { value: soundState, onChange: switchSound } : undefined

  const body = (
    <div
      ref={shell}
      // Focusable by a click, so keys kept to the player (see the shortcuts list) answer once it is chosen.
      tabIndex={-1}
      role="region"
      aria-label="Video player"
      onMouseMove={nudge}
      onPointerDownCapture={(e) => {
        if (e.pointerType === 'touch') hiddenAtTouch.current = !controlsShown.current
      }}
      onTouchStart={nudge}
      // Tabbing onto a hidden control shows the bar, as a mouse would.
      onFocusCapture={nudge}
      className={cx(
        // Never clipped or isolated: clipping a composited video paints it black,
        // and a stacking context put the subtitle menu behind the header.
        'group/player relative w-full bg-black outline-none select-none',
        // Filling the user-shaped floating window, or the 16:9 box leaves bands.
        pip.active ? 'h-full' : 'aspect-video',
        corners,
        !controlsVisible && 'cursor-none',
      )}
    >
      {/* Before the video, as the subtitle canvas goes right after it and later things paint over. */}
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

      {/* Never hidden: the canvas above paints only on presented frames. */}
      <video
        ref={setVideo}
        playsInline
        autoPlay={autoPlay && subtitlesSettled}
        className={cx('size-full', fill && 'object-cover', corners)}
        {...surface}
        onPlay={() => {
          setPlaying(true)
          if (getSleep().kind === 'fired') setSleep({ kind: 'off' })
          onResume?.('play')
          // Any resume re-arms the hide timer, as a keyboard one moves no pointer.
          nudge()
        }}
        onPause={() => {
          setPlaying(false)
          setControlsVisible(true)
        }}
        // A new source resets to paused without a pause event; the button must not keep saying Pause over it.
        onEmptied={(e) => setPlaying(!e.currentTarget.paused)}
        onWaiting={() => setWaiting(true)}
        onPlaying={() => setWaiting(false)}
        onCanPlay={() => setWaiting(false)}
        // Paused seeks repaint only once the new position decodes.
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
      {/* Said by a screen reader as they change; mounted throughout, or the first change goes unheard. */}
      <p role="status" className="sr-only">
        {noticeText}
      </p>
      <p role="status" className="sr-only">
        {activeSkip && !casting ? `${skipLabel(activeSkip.kind)} · ${keyHint('skip')} skips it` : ''}
      </p>
      <p role="status" className="sr-only">
        {stalledLong ? 'Buffering' : ''}
      </p>

      {(waiting || !stream || holdingStart) && !error && !lostError && (
        <div className="pointer-events-none absolute inset-0 grid place-items-center">
          <div className="text-center">
            <Spinner className="mx-auto size-8" />
            {stream && holdingStart && !waiting && <p className="mt-3 text-sm text-base-300">Loading subtitles…</p>}
            {/* No stream yet: still resolving, so the stall copy below doesn't apply. */}
            {!stream && (
              <>
                <p className="mt-3 text-sm text-base-300">{starting?.title ?? 'Finding a release…'}</p>
                {starting && (
                  <p className="mx-auto mt-1 max-w-md px-4 text-xs break-words text-base-500 tabular-nums">
                    {starting.detail}
                  </p>
                )}
              </>
            )}
            {/* A spinner looks the same buffering a second or with a dead swarm, so this says which. */}
            {stream && stalledFor > 12 && (
              <p className="mt-3 max-w-xs text-xs text-base-400">
                {/* What is arriving, never if it's enough: a slow line buffers and plays on, as anywhere. */}
                {!health || health.complete
                  ? 'Buffering…'
                  : health.peers > 0
                    ? `Buffering… ${speed(health.mbps, unit)} from ${health.peers} ${health.peers === 1 ? 'peer' : 'peers'}`
                    : 'Buffering… no peers connected right now'}
              </p>
            )}
            {health?.slow && stalledFor > 20 &&
              (queued ? (
                <Link
                  to="/downloads"
                  className={cx(buttonClass('secondary', 'sm'), 'pointer-events-auto mt-3')}
                >
                  Queued · open Downloads
                </Link>
              ) : (
                <button
                  onClick={() => void downloadInstead()}
                  className={cx(buttonClass('secondary', 'sm'), 'pointer-events-auto mt-3')}
                >
                  Download it instead and watch later
                </button>
              ))}
            {queueError && <p className="mt-2 text-xs text-recap">{queueError}</p>}
          </div>
        </div>
      )}

      {(error || lostError) && (
        <div role="alert" className={cx('absolute inset-0 grid place-items-center bg-base-950/80 p-6 text-center', corners)}>
          <div>
            <p className="text-sm text-base-200">{error ?? lostError}</p>
            <div className="mt-3 flex flex-wrap justify-center gap-2">
              {/* The same release again only fails again when its conversion is what failed. */}
              {!(error && final) && (
                <button
                  ref={tryAgain}
                  onClick={() => {
                    if (error) {
                      // A fresh source from where it stopped, playing once it can; the hook clears the error.
                      setResumeAt(video?.currentTime || startAt)
                      setWaiting(true)
                      resumeAfterRebuild(video, true)
                      setEpoch((e) => e + 1)
                      return
                    }
                    setLostError(null)
                    recover()
                  }}
                  className={cx(buttonClass('secondary', 'sm'), 'pointer-events-auto')}
                >
                  Try again
                </button>
              )}
              {error && onPickRelease && (
                <button onClick={onPickRelease} className={cx(buttonClass('secondary', 'sm'), 'pointer-events-auto')}>
                  Choose another release
                </button>
              )}
            </div>
          </div>
        </div>
      )}

      {flash && (
        <div
          className={cx(
            'pointer-events-none absolute top-1/2 -translate-y-1/2 rounded-full bg-base-950/70 px-4 py-2 text-sm font-medium text-white',
            flash === 'back' ? 'left-[12%]' : 'right-[12%]',
          )}
        >
          {flash === 'back' ? '−10s' : '+10s'}
        </div>
      )}

      {sleep.kind === 'fired' && !playing && !video?.ended && (
        <div className="pointer-events-none absolute top-4 left-1/2 -translate-x-1/2 rounded-full bg-base-950/80 px-3 py-1.5 text-xs text-white ring-1 ring-white/15">
          Paused by the sleep timer
        </div>
      )}

      {/* Where Skip sits: an auto-skip offered back for a moment, and AniSkip's times open to a "wrong". */}
      {offered && !activeSkip && (!offered.byHand || canCallWrong) && (
        <div className="absolute right-4 bottom-24 flex items-center gap-1 rounded-md bg-base-950/85 p-1 text-sm text-white ring-1 ring-white/20 backdrop-blur-sm">
          <span className="px-2 text-white/70">Skipped {skipKind(offered.range.kind) ?? 'ahead'}</span>
          {!offered.byHand && (
            <button
              onClick={() => {
                if (Date.now() - offered.at < SKIPPED_GUARD) return
                if (video) video.currentTime = offered.range.start
                autoSkipped.dismiss()
              }}
              className="rounded px-3 py-1 font-medium hover:bg-white/15 pointer-coarse:min-h-11"
            >
              Watch it
            </button>
          )}
          {canCallWrong && (
            <button
              onClick={() => {
                if (Date.now() - offered.at < SKIPPED_GUARD) return
                if (video) video.currentTime = offered.range.start
                onSkipVote!(offered.range.id!, false)
                autoSkipped.dismiss()
              }}
              className="rounded px-3 py-1 text-white/80 hover:bg-white/15 pointer-coarse:min-h-11"
            >
              Wrong skip
            </button>
          )}
        </div>
      )}

      {activeSkip && (
        <button
          onClick={skipActive}
          className="absolute right-4 bottom-24 rounded-md bg-base-950/85 px-4 py-2 text-sm font-medium text-white ring-1 ring-white/20 backdrop-blur-sm transition-transform hover:scale-105 pointer-coarse:min-h-11"
        >
          Skip {skipKind(activeSkip.kind) ?? 'ahead'}
          <kbd className="ml-2 font-sans text-xs text-white/50 max-sm:hidden">S</kbd>
        </button>
      )}

      <div
        className={cx(
          // The gradient passes taps to the picture (half of it on a phone); only its controls take input.
          'pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/90 via-black/50 to-transparent px-3 pt-10 pb-3 transition-opacity duration-200 sm:px-4',
          pip.active ? '' : 'sm:rounded-b-xl',
          controlsVisible ? 'opacity-100' : 'opacity-0',
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

        {/* Relative: on a phone a wide panel (Cast's) opens across the bar, not off its button's edge. */}
        <div className="relative mt-2 flex items-center gap-2 text-white">
          <IconButton label={playing ? 'Pause' : 'Play'} hint={keyHint('play')} onClick={togglePlay}>
            {playing ? <PauseIcon /> : <PlayIcon />}
          </IconButton>

          {/* Desktop only: on a phone double tap seeks and the hardware buttons set volume. */}
          <span className={cx('contents', !pip.active && 'max-sm:hidden')}>
          <IconButton label="Back 10 seconds" hint={keyHint('back10')} onClick={() => seekBy(-10)}>
            <SeekIcon back />
          </IconButton>
          <IconButton label="Forward 10 seconds" hint={keyHint('forward10')} onClick={() => seekBy(10)}>
            <SeekIcon />
          </IconButton>
          </span>

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
            <IconButton label="Next episode" hint={keyHint('next')} onClick={() => onNext('button')}>
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
            {clockTime(time)}
            <span className="max-sm:hidden"> / {clockTime(duration)}</span>
            {/* The part playing, which the bar shows only as amber. */}
            {activeSkip && <span className="text-filler max-sm:hidden"> · {skipLabel(activeSkip.kind)}</span>}
          </span>

          <div className="ml-auto flex items-center gap-1">
            {/* Not in the floating window, and on a phone in the settings menu instead. */}
            <span className="contents max-sm:hidden">
              {audioMenu && <AudioPicker tracks={audioMenu.tracks} value={audioMenu.value} onChange={audioMenu.onChange} />}
              {soundMenu && <SoundPicker value={soundMenu.value} onChange={soundMenu.onChange} />}
            </span>

            {/* Even with no track of its own: a file from this device, or Jimaku's, can be loaded. */}
            {stream && (
              <SubtitlePicker
                tracks={subtitleTracks}
                value={track}
                onChange={setTrack}
                scale={subScale}
                onScale={changeSubScale}
                delay={subDelay}
                onDelay={setSubDelay}
                keptDelay={keptDelay}
                onKeepDelay={onKeepDelay}
                look={onSubLook && { value: lookPreset, onChange: onSubLook }}
                onLoadFile={(file) => void loadOwnFile(file)}
                jimaku={
                  jimakuSet
                    ? {
                        streamId: stream.id,
                        onPick: (f) =>
                          addTrack({ index: JIMAKU_TRACK - f.index, title: `Jimaku · ${f.name}`, url: f.url, whole: true }),
                      }
                    : undefined
                }
              />
            )}

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
                <PlayerMenu
                  rate={rate}
                  onRate={changeRate}
                  sleep={sleep}
                  skips={skips}
                  video={video}
                  marking={markingMenu}
                  audio={audioMenu}
                  sound={soundMenu}
                  onOpen={holdForMenu}
                  onInfo={toggleInfo}
                />
                {!castImpossible() && (
                  <CastButton
                    casting={casting}
                    device={castDevice}
                    phase={castPhase}
                    sdkState={castSdk}
                    onCast={() => void castTo()}
                    onStop={stopCast}
                    onOpen={holdForCast}
                  />
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
                <IconButton
                  label="Fullscreen"
                  hint={keyHint('fullscreen')}
                  onClick={() => {
                    toggleFullscreen(shell.current, video)
                  }}
                >
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
          pip.active ? 'p-2' : 'p-4',
          controlsVisible ? 'opacity-100' : 'opacity-0',
        )}
      >
        {/* One line in the floating window, which is small and already titled. */}
        {pip.active ? (
          <p className="truncate text-xs font-medium text-white/90">{subtitle ?? title}</p>
        ) : (
          <>
            <p className="text-sm font-medium text-white">{title}</p>
            {subtitle && <p className="text-xs text-white/70">{subtitle}</p>}
          </>
        )}
        {/* The toolbar says Anime4K is on; this says when it is not working. */}
        {(upscaleState === 'unsupported' || upscaleState === 'failed') && (
          <p className="mt-1.5 inline-block rounded bg-black/60 px-2 py-0.5 text-[11px] text-amber-300">
            {upscaleState === 'unsupported'
              ? "Anime4K needs WebGPU, which this browser doesn't have; playing without it."
              : 'Anime4K stopped (the graphics device was lost); playing without it.'}
          </p>
        )}
      </div>

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

      {overlay}
    </div>
  )

  return (
    <div ref={anchor} className="w-full">
      {createPortal(body, host)}
    </div>
  )
}

/** What an opening or ending is called, for the marker's tooltip. */
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

  // Where the pointer is, as a fraction. Null when it is not over the bar.
  const [at, setAt] = useState<number | null>(null)
  // Pressed and moving: the bar follows the finger and the seek lands on
  // release, the way every video site does it. A tap is a drag of no distance.
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
      // A slider for the keyboard and screen readers: the arrows seek through the player's own keys; Home, End
      // and the page keys jump from here.
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
      // touch-none, or the page scrolls instead of scrubbing; taller than the bar, for fingers.
      className="group/scrub relative -my-1 h-6 cursor-pointer touch-none rounded focus-visible:ring-2 focus-visible:ring-accent-400 focus-visible:outline-hidden"
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
        {/* What is loaded and will play without waiting, lighter than the rest of the bar. */}
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

      {/* Openings and endings in amber, so what is about to be skipped shows before the button. */}
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

// The stretches that will play without waiting on the network, as [from, to] seconds: the lighter part of
// the bar. Asked of the server; the browser's own buffer is a small window around the playhead, no more.
function useLoadedRanges(streamId: string | undefined, video: HTMLVideoElement | null): Array<[number, number]> {
  // Tagged with their stream, so another stream's ranges never show and nothing has to be cleared.
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
        // Asked again while downloading and on screen, paused too: asking keeps nothing alive.
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

/** The frame under the pointer, cut from one sprite sheet (no requests), with its time; until then the time. */
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
  // While the file downloads, only the parts on disk have a picture; the rest show the time alone.
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
      className="pointer-events-none absolute bottom-5 -translate-x-1/2"
      // Clamped so the preview cannot hang off either end of the player.
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
        // Always out on touch: a tablet has no hover to reveal it, and a tap on the speaker only mutes.
        className="w-0 accent-[var(--color-accent-500)] opacity-0 transition-all duration-200 group-hover/vol:w-20 group-hover/vol:opacity-100 group-focus-within/vol:w-20 group-focus-within/vol:opacity-100 pointer-coarse:w-20 pointer-coarse:opacity-100"
      />
    </div>
  )
}

/** A plain menu, as the OS paints a native select white over the dark player. */
// Language first; the release's track name only when it tells two apart (Erai-raws calls all "CR").
function subtitleLabel(tracks: StreamInfo['subtitles'], t: StreamInfo['subtitles'][number]): string {
  const language = languageName(t.language)
  if (!language) return t.title || `Track ${t.index + 1}`
  if (t.title && tracks.filter((o) => languageName(o.language) === language).length > 1) return `${language} · ${t.title}`
  return language
}

function SubtitlePicker({
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
  tracks: StreamInfo['subtitles']
  value: number | null
  onChange: (index: number | null) => void
  scale: number
  onScale: (scale: number) => void
  delay: number
  onDelay: (delay: number) => void
  /** The delay remembered for this show's releases from this group, if any. */
  keptDelay?: number
  /** Keeps the delay for the rest of the show, from the same group; absent when nothing can say the group. */
  onKeepDelay?: (delay: number) => void
  look?: { value: SubLook; onChange: (look: SubLook) => void }
  onLoadFile: (file: File) => void
  /** Jimaku's files for this episode, offered once a key is set. */
  jimaku?: { streamId: string; onPick: (file: JimakuFile) => void }
}) {
  const { open, close, toggle, trigger, menu } = useMenuButton()
  const ref = useDismiss<HTMLDivElement>(close, open)
  const room = useRoomAbove(ref, open)
  const picker = useRef<HTMLInputElement>(null)
  const jimakuFiles = useQuery({
    queryKey: ['jimaku-files', jimaku?.streamId],
    queryFn: () => api.get<{ files: JimakuFile[] }>(`/api/stream/${jimaku!.streamId}/jimaku`),
    enabled: open && !!jimaku,
    staleTime: 10 * 60_000,
    retry: false,
  })

  const label = (t: StreamInfo['subtitles'][number]) => subtitleLabel(tracks, t)
  const active = tracks.find((t) => t.index === value)

  return (
    <div className="relative" ref={ref}>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={toggle}
        title="Subtitles"
        aria-label={`Subtitles: ${active ? label(active) : 'off'}`}
        className="flex h-9 items-center gap-1.5 rounded-md px-2.5 text-xs text-white/90 transition-colors hover:bg-white/15"
      >
        <CaptionIcon />
        {/* On a phone the icon alone: the bar has no room for words, and the menu names the track. */}
        <span className="max-w-24 truncate max-sm:hidden">{active ? label(active) : 'Off'}</span>
      </button>

      {open && (
        <div
          ref={menu}
          role="menu"
          aria-label="Subtitles"
          onKeyDown={walkMenu}
          style={{ maxHeight: room }}
          className="absolute right-0 bottom-full z-50 mb-2 w-52 origin-bottom-right animate-rise overflow-y-auto rounded-lg border border-base-700 bg-base-850 py-1 shadow-xl shadow-black/60 scrollbar-thin"
        >
          {/* Only the tracks scroll, so size and delay stay in view (unless the screen is too short). */}
          <div className="max-h-52 overflow-y-auto scrollbar-thin">
            <MenuRow selected={value === null} onClick={() => { onChange(null); close() }}>
              Off
            </MenuRow>
            {tracks.map((t) => (
              <MenuRow
                key={t.index}
                selected={t.index === value}
                onClick={() => { onChange(t.index); close() }}
              >
                {label(t)}
              </MenuRow>
            ))}
            {jimaku && (
              <>
                <p className="px-3 pt-2 pb-1 text-[10px] font-semibold tracking-wider text-base-500 uppercase">Jimaku · Japanese</p>
                {jimakuFiles.isPending ? (
                  <p className="px-3 py-1.5 text-xs text-base-400">Looking…</p>
                ) : jimakuFiles.isError ? (
                  <p className="px-3 py-1.5 text-xs text-recap">{(jimakuFiles.error as Error).message}</p>
                ) : jimakuFiles.data.files.length === 0 ? (
                  <p className="px-3 py-1.5 text-xs text-base-400">None for this episode</p>
                ) : (
                  jimakuFiles.data.files.map((f) => (
                    <MenuRow key={f.index} selected={JIMAKU_TRACK - f.index === value} onClick={() => { jimaku.onPick(f); close() }}>
                      <span className="break-all">{f.name}</span>
                    </MenuRow>
                  ))
                )}
              </>
            )}
          </div>
          <button
            type="button"
            role="menuitem"
            onClick={() => picker.current?.click()}
            className="w-full border-t border-base-700 px-3 py-1.5 text-left text-sm text-base-200 hover:bg-base-750 pointer-coarse:py-3"
          >
            Load a subtitle file…
          </button>
          <input
            ref={picker}
            type="file"
            accept=".ass,.ssa,.srt,.vtt"
            hidden
            onChange={(e) => {
              const file = e.currentTarget.files?.[0]
              e.currentTarget.value = ''
              if (file) {
                onLoadFile(file)
                close()
              }
            }}
          />
          {value !== null && (
            <div className="mt-1 border-t border-base-700 pt-1">
              <Stepper
                label="Size"
                value={`${Math.round(scale * 100)}%`}
                onLess={() => onScale(SUB_SCALES[Math.max(0, SUB_SCALES.indexOf(scale) - 1)] ?? 1)}
                onMore={() => onScale(SUB_SCALES[Math.min(SUB_SCALES.length - 1, SUB_SCALES.indexOf(scale) + 1)] ?? 1)}
                onReset={() => onScale(1)}
              />
              <Stepper
                label="Delay"
                value={delayLabel(delay)}
                onLess={() => onDelay(Math.round((delay - DELAY_STEP) * 10) / 10)}
                onMore={() => onDelay(Math.round((delay + DELAY_STEP) * 10) / 10)}
                onReset={() => onDelay(0)}
              />
              {/* A batch off by the same amount every episode: fixed once, not on each. */}
              {onKeepDelay && delay !== (keptDelay ?? 0) && (
                <button
                  type="button"
                  role="menuitem"
                  onClick={() => onKeepDelay(delay)}
                  className="w-full px-3 py-1.5 text-left text-xs text-accent-300 hover:bg-base-750 pointer-coarse:py-3"
                >
                  {delay === 0 ? 'Stop keeping a delay for this show' : 'Keep this delay for this show'}
                </button>
              )}
              {onKeepDelay && delay !== 0 && delay === keptDelay && (
                <p className="px-3 py-1.5 text-xs text-base-500">Kept for this show and group</p>
              )}
              {/* Pictures are drawn as made; only text can be made over. */}
              {look && !active?.pictures && (
                <>
                  <p className="px-3 pt-2 pb-1 text-[10px] font-semibold tracking-wider text-base-500 uppercase">Look</p>
                  {SUB_LOOKS.map((l) => (
                    <MenuRow key={l.value} selected={look.value === l.value} onClick={() => look.onChange(l.value)}>
                      {l.label}
                    </MenuRow>
                  ))}
                </>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/** A − value + row; the value resets it. */
function Stepper({
  label,
  value,
  onLess,
  onMore,
  onReset,
}: {
  label: string
  value: string
  onLess: () => void
  onMore: () => void
  onReset: () => void
}) {
  const step = 'grid size-6 pointer-coarse:size-10 place-items-center rounded text-base-200 hover:bg-base-750 hover:text-white'
  return (
    <div className="flex items-center gap-1 px-3 py-1 text-sm">
      <span className="flex-1 text-base-400">{label}</span>
      <button type="button" role="menuitem" aria-label={`${label} down`} onClick={onLess} className={step}>
        −
      </button>
      <button
        type="button"
        role="menuitem"
        aria-label={`Reset ${label.toLowerCase()}`}
        title="Reset"
        onClick={onReset}
        className="w-12 rounded text-center text-xs tabular-nums text-base-200 hover:bg-base-750"
      >
        {value}
      </button>
      <button type="button" role="menuitem" aria-label={`${label} up`} onClick={onMore} className={step}>
        +
      </button>
    </div>
  )
}

function AudioPicker({
  tracks,
  value,
  onChange,
}: {
  tracks: AudioTrack[]
  value: number
  onChange: (index: number) => void
}) {
  const { open, close, toggle, trigger, menu } = useMenuButton()
  const ref = useDismiss<HTMLDivElement>(close, open)

  return (
    <div className="relative" ref={ref}>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={toggle}
        title="Audio"
        aria-label={`Audio track: ${audioLabel(tracks, value)}`}
        className="flex h-9 items-center gap-1.5 rounded-md px-2.5 text-xs text-white/90 transition-colors hover:bg-white/15"
      >
        <SpeakerIcon />
        <span className="max-w-24 truncate">{audioLabel(tracks, value)}</span>
      </button>

      {open && (
        <div
          ref={menu}
          role="menu"
          aria-label="Audio track"
          onKeyDown={walkMenu}
          className="absolute right-0 bottom-full z-50 mb-2 max-h-64 w-44 origin-bottom-right animate-rise overflow-y-auto rounded-lg border border-base-700 bg-base-850 py-1 shadow-xl shadow-black/60 scrollbar-thin"
        >
          {tracks.map((_, i) => (
            <MenuRow key={i} selected={i === value} onClick={() => { onChange(i); close() }}>
              {audioLabel(tracks, i)}
            </MenuRow>
          ))}
        </div>
      )}
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

/** The query for a sound change: only the fields given, so the rest stay as they are on the server. */
function soundQuery(patch: Partial<SoundOptions>): string {
  const q = new URLSearchParams()
  for (const [key, value] of Object.entries(patch)) if (value !== undefined) q.set(key, String(value))
  const s = q.toString()
  return s ? `?${s}` : ''
}

/** What is on, in a few words, for the button. */
function soundSummary(s: SoundOptions): string {
  const on = []
  if (s.voice !== 'off') on.push(`Voice ${LEVEL_LABELS[s.voice].toLowerCase()}`)
  if (s.night) on.push('Night')
  if (s.headphones) on.push('Headphones')
  if (s.mono) on.push('Mono')
  if (s.boost !== 'off') on.push(`Boost ${LEVEL_LABELS[s.boost].toLowerCase()}`)
  return on.length ? on.join(', ') : 'Default'
}

function SoundPicker({ value, onChange }: { value: SoundOptions; onChange: (patch: Partial<SoundOptions>) => void }) {
  const { open, close, toggle, trigger, menu } = useMenuButton()
  const ref = useDismiss<HTMLDivElement>(close, open)
  const room = useRoomAbove(ref, open)
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
          className="absolute right-0 bottom-full z-50 mb-2 w-52 origin-bottom-right animate-rise overflow-y-auto rounded-lg border border-base-700 bg-base-850 py-1 shadow-xl shadow-black/60 scrollbar-thin"
        >
          <SoundRows value={value} onChange={onChange} />
        </div>
      )}
    </div>
  )
}

// Enhancing (what the ear picks out) and boosting (how loud the whole thing is) are different asks,
// so they sit apart, as on the services that offer both.
function SoundRows({ value, onChange }: { value: SoundOptions; onChange: (patch: Partial<SoundOptions>) => void }) {
  const heading = 'px-3 pt-2 pb-1 text-[10px] font-semibold tracking-wider text-base-500 uppercase'
  return (
    <>
      <p className={heading}>Voice boost</p>
      {VOICE_LEVELS.map((level) => (
        <MenuRow key={level} selected={level === value.voice} onClick={() => onChange({ voice: level })}>
          {LEVEL_LABELS[level]}
        </MenuRow>
      ))}
      <p className={heading}>Enhance</p>
      <MenuRow selected={value.night} onClick={() => onChange({ night: !value.night })}>Night mode</MenuRow>
      <MenuRow selected={value.headphones} onClick={() => onChange({ headphones: !value.headphones })}>Headphones</MenuRow>
      <MenuRow selected={value.mono} onClick={() => onChange({ mono: !value.mono })}>Mono</MenuRow>
      <p className={heading}>Volume boost</p>
      {VOICE_LEVELS.map((level) => (
        <MenuRow key={level} selected={level === value.boost} onClick={() => onChange({ boost: level })}>
          {LEVEL_LABELS[level]}
        </MenuRow>
      ))}
    </>
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

type MenuView = 'main' | 'speed' | 'sleep' | 'skips' | 'audio' | 'sound' | 'quality'

// The player's settings listed with their values, each opening its own options in place.
function PlayerMenu({
  rate,
  onRate,
  sleep,
  skips,
  video,
  marking,
  audio,
  sound,
  onOpen,
  onInfo,
}: {
  rate: number
  onRate: (rate: number) => void
  sleep: Sleep
  skips: SkipRange[]
  video: HTMLVideoElement | null
  /** Absent where marks can't be kept. */
  marking?: Marking
  audio?: { tracks: AudioTrack[]; value: number; onChange: (index: number) => void }
  sound?: { value: SoundOptions; onChange: (patch: Partial<SoundOptions>) => void }
  onOpen: (open: boolean) => void
  onInfo: () => void
}) {
  const { open, close: shut, toggle, trigger, menu } = useMenuButton()
  const [view, setView] = useState<MenuView>('main')
  const net = useNetwork()
  const close = useCallback(() => {
    shut()
    setView('main')
  }, [shut])
  // A list swapped for another takes focus with it, or a keyboard is left on nothing.
  useEffect(() => {
    const doc = menu.current?.ownerDocument
    if (open && doc && (doc.activeElement === doc.body || !doc.activeElement)) menuItems(menu.current)[0]?.focus()
  }, [open, view, menu])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const room = useRoomAbove(ref, open)
  // A phone's player is too short for the list above its bar: there it is a sheet along the screen's bottom.
  const wide = useMediaQuery('(min-width: 40rem)')
  useEffect(() => {
    onOpen(open)
    return () => onOpen(false)
  }, [open, onOpen])
  const from = marking?.from ?? null
  // A phone has no keys to name.
  const coarse = useMediaQuery('(pointer: coarse)')
  // Rounded: the clock can be a few seconds behind.
  const now = useNow(15_000)
  const left = sleep.kind === 'at' ? Math.max(1, Math.round((sleep.at - now) / 60_000)) : 0
  const sleepValue = sleep.kind === 'at' ? `${left} min left` : sleep.kind === 'episode' ? 'End of episode' : 'Off'
  const busy = sleep.kind === 'at' || sleep.kind === 'episode' || !!from
  const label =
    sleep.kind === 'at'
      ? `Player settings · sleep timer: ${left} min left`
      : sleep.kind === 'episode'
        ? 'Player settings · sleep timer: end of this episode'
        : 'Player settings'
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
          'tap flex h-9 items-center gap-1 rounded-md px-2 text-xs tabular-nums transition-colors hover:bg-white/15',
          busy ? 'text-accent-300' : 'text-white/90',
        )}
      >
        <GearIcon />
        {sleep.kind === 'at' && <span>{left}m</span>}
      </button>

      {open && !wide && <div aria-hidden className="fixed inset-0 z-50 bg-black/50" onClick={close} />}
      {open && (
        <div
          ref={menu}
          role="menu"
          aria-label="Player settings"
          onKeyDown={walkMenu}
          style={{ maxHeight: wide ? room : '70vh' }}
          className={cx(
            'z-50 animate-rise overflow-y-auto border border-base-700 bg-base-850 py-1 shadow-xl shadow-black/60 scrollbar-thin',
            wide
              ? 'absolute right-0 bottom-full mb-2 w-64 origin-bottom-right rounded-lg'
              : 'fixed inset-x-0 bottom-0 rounded-t-xl pb-[max(0.5rem,env(safe-area-inset-bottom))]',
          )}
        >
          {view === 'main' && (
            <>
              {/* Wider, the bar has their own buttons. */}
              {!wide && audio && (
                <MenuItem label="Audio track" value={audioLabel(audio.tracks, audio.value)} onClick={() => setView('audio')} />
              )}
              {!wide && sound && <MenuItem label="Sound" value={soundSummary(sound.value)} onClick={() => setView('sound')} />}
              <MenuItem label="Speed" value={rate === 1 ? 'Normal' : `${rate}×`} onClick={() => setView('speed')} />
              <MenuItem label="Quality" value={QUALITIES.find((q) => q.value === net.quality)?.label ?? 'Auto'} onClick={() => setView('quality')} />
              <MenuItem label="Sleep timer" value={sleepValue} onClick={() => setView('sleep')} />
              {marking && (
                <MenuItem
                  label="Skip times"
                  value={from ? 'Marking' : skips.length ? `${skips.length} known` : 'None known'}
                  onClick={() => setView('skips')}
                />
              )}
              <MenuItem label="Playback info" value={coarse ? '' : keyHint('info')} onClick={() => (onInfo(), close())} />
              {/* Keys are for a keyboard; a phone has none. */}
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

          {view === 'audio' && audio && (
            <>
              <MenuBack label="Audio track" onClick={() => setView('main')} />
              {audio.tracks.map((_, i) => (
                <MenuRow key={i} selected={i === audio.value} onClick={() => (audio.onChange(i), close())}>
                  {audioLabel(audio.tracks, i)}
                </MenuRow>
              ))}
            </>
          )}

          {view === 'sound' && sound && (
            <>
              <MenuBack label="Sound" onClick={() => setView('main')} />
              <SoundRows value={sound.value} onChange={sound.onChange} />
            </>
          )}

          {view === 'skips' && marking && (
            <>
              <MenuBack label="Skip times" onClick={() => setView('main')} />
              <SkipTimesPanel skips={skips} video={video} marking={marking} />
            </>
          )}
        </div>
      )}
    </div>
  )
}

const codecName = (codec?: string) => (codec === 'h264' ? 'H.264' : codec?.toUpperCase())

// What the player and the server are doing with this episode, for a curious viewer or a bug report.
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
  // Each second for what the browser reports, every few for the server's side.
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
    // The server's reason reads "re-encoding video hevc 10-bit and audio ac3".
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
    // Inside the panel: in fullscreen only the player is on screen, and the fallback needs a visible field.
    setCopied((await copyText(text, panel.current ?? undefined)) ? 'Copied' : "Couldn't copy")
    window.setTimeout(() => setCopied(null), 2000)
  }

  return (
    <div
      ref={panel}
      role="region"
      aria-label="Playback info"
      className="pointer-events-auto absolute top-20 left-4 z-30 max-h-[calc(100%-6rem)] w-80 max-w-[calc(100%-2rem)] overflow-y-auto rounded-lg bg-base-950/90 p-3 text-xs text-base-200 ring-1 ring-white/15 backdrop-blur-sm"
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

// A row of the menu's list: what it sets, and its value now.
function MenuItem({ label, value, onClick }: { label: string; value: string; onClick: () => void }) {
  return (
    <button
      role="menuitem"
      onClick={onClick}
      className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-sm text-base-100 transition-colors hover:bg-base-750 pointer-coarse:min-h-11"
    >
      <span>{label}</span>
      <span className="truncate text-xs text-base-400">{value}</span>
    </button>
  )
}

function MenuBack({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      role="menuitem"
      onClick={onClick}
      aria-label={`Back from ${label}`}
      className="mb-1 flex w-full items-center gap-2 border-b border-base-750 px-3 py-2 text-left text-sm font-medium text-base-100 transition-colors hover:bg-base-750 pointer-coarse:min-h-11"
    >
      <span aria-hidden className="text-base-400">‹</span>
      {label}
    </button>
  )
}

type MarkFrom = { kind: 'op' | 'ed'; at: number }

/** A mark begun in the menu or with its key, finished in either. */
interface Marking {
  from: MarkFrom | null
  setFrom: (from: MarkFrom | null) => void
  /** Saves from where it began to now, and says what was kept. */
  save: () => Promise<string>
  forget: () => void
}

const partName = (kind: string) => (kind === 'op' ? 'Opening' : 'Ending')

function markSaved(kind: string, saved: SkipMarkResult): string {
  const lined = { chapters: ", lined up with the file's chapters", sound: ', lined up with the music', '': '' }
  return `${partName(kind)} saved · ${clockTime(saved.start)}–${clockTime(saved.end)}${lined[saved.snapped] ?? ''}`
}

// The episode's skip times and where each came from, and marking an opening or ending at the current moment.
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
      <div className="mt-1 border-t border-base-750 pt-1">
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

function SpeakerIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0" aria-hidden>
      <path d="M4 9.5v5h3.5L12 18.5v-13L7.5 9.5H4z" {...stroke} />
      <path d="M15.5 9a4 4 0 0 1 0 6M18 6.5a7.5 7.5 0 0 1 0 11" {...stroke} />
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
      className={cx(
        'block w-full truncate px-3 py-1.5 text-left text-sm transition-colors pointer-coarse:py-3',
        selected ? 'text-accent-400' : 'text-base-200 hover:bg-base-750',
      )}
    >
      {children}
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

// Held, a toggle would flip 30 times a second and Next skip episodes; seeks, volume and delay repeat.
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
}: {
  video: HTMLVideoElement | null
  togglePlay: () => void
  seekBy: (n: number) => void
  nudge: () => void
  shell: HTMLElement | null
  /** The picture-in-picture window, whose keys never reach this one. */
  extra: Window | null
  showVolume: () => void
  /** Absent while subtitles are off. */
  shiftSubDelay?: (by: number) => void
  /** Skips the opening or ending on screen; absent when there is none. */
  skip?: () => void
  /** Off, medium, high, round again; absent before the stream is open. */
  cycleVoice?: () => void
  /** The Next episode button's, absent while it doesn't show. */
  next?: () => void
  previous?: () => void
  stepRate: (by: number) => void
  stepFrame: (by: number) => void
  /** Absent when the episode has no subtitles. */
  cycleSubtitles?: () => void
  toggleInfo: () => void
  /** The 10-second key held: 2× until it's let go. */
  holdFast: (on: boolean) => void
  /** Absent where marks can't be kept. */
  mark?: (edge: 'start' | 'end') => void
}) {
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null
      if (target && ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)) return
      // An open dialog or menu owns the keyboard, or Space wouldn't press its buttons.
      if (target?.closest('[role=dialog], [role=menu], [role=listbox], [data-portal-menu]')) return
      // Space on a button outside the player is that button's own.
      if (e.key === ' ' && target?.closest('button, a, [role=switch], [role=tab]') && !shell?.contains(target)) return
      // Ctrl+F and the like are the browser's; AltGr, read as Ctrl+Alt, types [ on many layouts.
      if ((e.ctrlKey || e.metaKey || e.altKey) && !e.getModifierState('AltGraph')) return
      // Asked of this device: single keys only while the player has focus, leaving a screen reader its own.
      if (keyScope() === 'player' && !(target && shell?.contains(target))) return

      const action = actionFor(e)
      if (!action) {
        // 0 to 9 jump to that tenth of the episode, as on YouTube; typed with Shift on AZERTY.
        if (/^[0-9]$/.test(e.key) && video && Number.isFinite(video.duration)) {
          video.currentTime = (video.duration * Number(e.key)) / 10
          nudge()
        }
        return
      }
      // The page must not scroll while the arrows seek or change volume.
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
          // The same as the button, so only while it shows.
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
      }
      // Only for a key that did something.
      nudge()
    }
    function onKeyUp(e: KeyboardEvent) {
      if (actionFor(e) === 'forward10') holdFast(false)
    }
    // A key let go in another window sends no keyup here.
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
    stepRate, stepFrame, cycleSubtitles, toggleInfo, holdFast, mark])
}

function IconButton({
  label,
  hint,
  onClick,
  children,
}: {
  label: string
  /** The key that does the same, named in the tooltip: hovering a button is how its key gets learned. */
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

type Need = { label: string; met: boolean | null; detail?: ReactNode }

// Always in the bar: when casting can't start, it lists what casting needs, each met or not, with the fix.
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
  const room = useRoomAbove(ref, open)
  useEffect(() => {
    onOpen(open)
    // Unmounted open (another episode's bar), the controls must not stay held.
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
  // The same page at the HTTPS address, its token signing this device in there.
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
      // Asked only on a secure page, so before that it isn't known.
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
        sdkState === 'loading'
          ? 'Looking…'
          : sdkState === 'ok' && phase === 'none'
            ? 'None found yet: a Chromecast or Google TV, switched on and on the same Wi-Fi as this device.'
            : undefined,
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
      {open && (
        <div
          role="dialog"
          aria-label="Cast to a TV"
          style={{ maxHeight: room }}
          className="absolute right-0 bottom-full z-50 mb-2 w-80 max-w-[calc(100vw-2rem)] origin-bottom-right animate-rise overflow-y-auto rounded-lg border border-base-700 bg-base-850 p-3 text-sm shadow-xl shadow-black/60 scrollbar-thin"
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
        'mt-0.5 grid size-4 shrink-0 place-items-center rounded-full text-[10px] leading-none',
        met ? 'bg-accent-500 text-white' : met === false ? 'ring-1 ring-recap text-recap' : 'ring-1 ring-base-600',
      )}
    >
      {met ? '✓' : met === false ? '!' : ''}
    </span>
  )
}

// While the TV plays, the video area becomes its remote: where it plays, play or pause, a seek bar, stop.
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
