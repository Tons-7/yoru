import { useCallback, useEffect, useRef, useState } from 'react'
import { appleTouchDevice } from '../lib/install'

// Google Cast: the Web Sender SDK sends the episode to a Chromecast, whose Default Media Receiver plays
// Yoru's own HLS through a link signed for the session. Only on an HTTPS or localhost page.

const SDK = 'https://www.gstatic.com/cv/js/sender/v1/cast_sender.js?loadCastFramework=1'

// The slice of the SDK used here.
interface CastSession {
  getCastDevice(): { friendlyName: string }
  loadMedia(request: unknown): Promise<unknown>
  // The older session, which takes a whole queue at once.
  getSessionObj(): {
    queueLoad(request: unknown, done: () => void, failed: (err?: { code?: string; description?: string }) => void): void
  }
}
interface CastContext {
  setOptions(options: { receiverApplicationId: string; autoJoinPolicy: string }): void
  getCastState(): string
  getCurrentSession(): CastSession | null
  requestSession(): Promise<unknown>
  endCurrentSession(stopCasting: boolean): void
  addEventListener(type: string, handler: () => void): void
  removeEventListener(type: string, handler: () => void): void
}
interface RemotePlayer {
  currentTime: number
  duration: number
  isPaused: boolean
  isConnected: boolean
  mediaInfo?: { contentId?: string } | null
}
interface RemotePlayerController {
  playOrPause(): void
  seek(): void
  addEventListener(type: string, handler: () => void): void
  removeEventListener(type: string, handler: () => void): void
}
interface CastFramework {
  CastContext: { getInstance(): CastContext }
  CastContextEventType: { CAST_STATE_CHANGED: string }
  RemotePlayer: new () => RemotePlayer
  RemotePlayerController: new (player: RemotePlayer) => RemotePlayerController
  RemotePlayerEventType: { ANY_CHANGE: string }
}
type Ctor = new (...args: never[]) => Record<string, unknown>
interface ChromeCast {
  AutoJoinPolicy: { ORIGIN_SCOPED: string }
  Image: new (url: string) => unknown
  media: {
    DEFAULT_MEDIA_RECEIVER_APP_ID: string
    MediaInfo: new (url: string, contentType: string) => Record<string, unknown>
    GenericMediaMetadata: Ctor
    LoadRequest: new (info: unknown) => Record<string, unknown>
    QueueItem: new (info: unknown) => Record<string, unknown>
    QueueLoadRequest: new (items: unknown[]) => Record<string, unknown>
    Track: new (id: number, type: string) => Record<string, unknown>
    TextTrackStyle: Ctor
    TrackType: { TEXT: string }
    TextTrackType: { SUBTITLES: string }
    StreamType: { BUFFERED: string }
    HlsSegmentFormat: { FMP4: string }
    HlsVideoSegmentFormat: { FMP4: string }
  }
}

const sdk = () =>
  window as unknown as { cast?: { framework: CastFramework }; chrome?: { cast?: ChromeCast }; __onGCastApiAvailable?: (ok: boolean) => void }

/** An iPhone or iPad: no browser there can cast, so nothing about it is offered. */
export const castImpossible = appleTouchDevice

/** Brave keeps casting behind a switch of its own, which the needs list names. */
export const isBrave = () => 'brave' in navigator

/** Casting needs a secure page; whether this browser can cast is the SDK's answer, not its name. */
export function castSupported(): boolean {
  return window.isSecureContext && !castImpossible()
}

/** 'off': this browser can't cast, or has it turned off. 'failed': the SDK didn't arrive (offline, blocked). */
export type CastSdk = 'loading' | 'ok' | 'off' | 'failed'

let loading: Promise<CastSdk> | null = null

function loadSdk(): Promise<CastSdk> {
  loading ??= new Promise((resolve) => {
    sdk().__onGCastApiAvailable = (ok) => {
      const { cast, chrome } = sdk()
      if (ok && cast && chrome?.cast) {
        cast.framework.CastContext.getInstance().setOptions({
          receiverApplicationId: chrome.cast.media.DEFAULT_MEDIA_RECEIVER_APP_ID,
          autoJoinPolicy: chrome.cast.AutoJoinPolicy.ORIGIN_SCOPED,
        })
      }
      resolve(ok && cast && chrome?.cast ? 'ok' : 'off')
    }
    const script = document.createElement('script')
    script.src = SDK
    script.async = true
    script.onerror = () => {
      // Tried again on the next player, as the connection may be back.
      loading = null
      resolve('failed')
    }
    document.head.append(script)
  })
  return loading
}

/** An episode the TV plays after the one cast, on its own: the server's queue. */
export interface QueuedMedia {
  /** Which episode this is ("anime-episode"), so a page can tell its own on the TV from another's. */
  key: string
  url: string
  title: string
  subtitle?: string
  artwork?: string
  subtitles: Array<{ url: string; language?: string; title?: string }>
  /** Position in subtitles of the track to show, if any. */
  subtitleTrack?: number
}

export interface CastMedia extends QueuedMedia {
  startAt: number
  /** How the TV draws them: TextTrackStyle's fields (see subLook.ts). */
  textStyle?: Record<string, unknown>
  /** What plays after, with Auto next on; each opens on the server once the TV gets to it. */
  queue?: QueuedMedia[]
}

/** An episode's key, the same for every device's session of it ("-dev…", "-cast", drawn-in copies). */
export const episodeKey = (sessionId: string) => sessionId.replace(/-[a-z].*$/, '')

export type CastPhase = 'none' | 'idle' | 'connecting' | 'connected'

// What this page last sent to the TV, the queue after it included. A session outlives episodes and is joined
// on any page of Yoru, so a connection alone doesn't say the TV plays the episode open here.
let sent: { items: Array<{ key: string; url: string }>; seen: boolean } | null = null

/** The cast device's state, and the TV's player once connected. 'none': no device found, or no SDK. */
export function useCast() {
  const [phase, setPhase] = useState<CastPhase>('none')
  // Not asked on an insecure page: the SDK can't work there, and the needs list says why.
  const [sdkState, setSdkState] = useState<CastSdk>(() => (castSupported() ? 'loading' : 'off'))
  const [device, setDevice] = useState('')
  // key: the episode these times are of, '' while the TV plays none of this page's.
  const [remote, setRemote] = useState({ time: 0, duration: 0, paused: true, key: '' })
  // The key of the stream the TV plays, if this page sent it; '' for anything else.
  const [playing, setPlaying] = useState('')
  const player = useRef<RemotePlayer | null>(null)
  const controller = useRef<RemotePlayerController | null>(null)

  useEffect(() => {
    if (!castSupported()) return
    let stop = () => {}
    let cancelled = false
    // A browser the SDK doesn't know may never answer: shown as can't cast, and corrected if it answers late.
    const silent = window.setTimeout(() => setSdkState((s) => (s === 'loading' ? 'off' : s)), 10_000)
    void loadSdk().then((state) => {
      const fw = sdk().cast?.framework
      window.clearTimeout(silent)
      if (cancelled) return
      setSdkState(state)
      if (state !== 'ok' || !fw) return
      const ctx = fw.CastContext.getInstance()
      const p = new fw.RemotePlayer()
      const c = new fw.RemotePlayerController(p)
      player.current = p
      controller.current = c
      // Ours while the TV reports one of our links, and until it first does (still loading it); not once it
      // moved on to another sender's.
      const ours = () => {
        if (!sent) return ''
        const on = sent.items.find((i) => i.url === p.mediaInfo?.contentId)
        if (on) {
          sent.seen = true
          return on.key
        }
        return sent.seen ? '' : sent.items[0].key
      }
      const onState = () => {
        const state = ctx.getCastState()
        if (state !== 'CONNECTED') sent = null
        setPhase(state === 'CONNECTED' ? 'connected' : state === 'CONNECTING' ? 'connecting' : state === 'NOT_CONNECTED' ? 'idle' : 'none')
        setDevice(ctx.getCurrentSession()?.getCastDevice().friendlyName ?? '')
        setPlaying(ours())
      }
      // The length each link first showed: an episode's stays put, so another is the next's, its time come
      // before the TV said it moved on.
      const lengths = new Map<string, number>()
      const onChange = () => {
        const url = p.mediaInfo?.contentId ?? ''
        const on = sent?.items.find((i) => i.url === url)?.key ?? ''
        if (on && p.duration > 0 && !lengths.has(url)) lengths.set(url, p.duration)
        const length = lengths.get(url) ?? 0
        const moved = length > 0 && p.duration > 0 && Math.abs(p.duration - length) >= 0.5
        setRemote({ time: p.currentTime, duration: p.duration, paused: p.isPaused, key: moved ? '' : on })
        setPlaying(ours())
      }
      ctx.addEventListener(fw.CastContextEventType.CAST_STATE_CHANGED, onState)
      c.addEventListener(fw.RemotePlayerEventType.ANY_CHANGE, onChange)
      onState()
      stop = () => {
        ctx.removeEventListener(fw.CastContextEventType.CAST_STATE_CHANGED, onState)
        c.removeEventListener(fw.RemotePlayerEventType.ANY_CHANGE, onChange)
      }
    })
    return () => {
      cancelled = true
      window.clearTimeout(silent)
      stop()
    }
  }, [])

  /** Asks for a device when none is connected (Chrome's own picker), then plays the media there. */
  const start = useCallback(async (media: CastMedia) => {
    const { cast, chrome } = sdk()
    if (!cast || !chrome?.cast) throw new Error('Casting is not available in this browser')
    const ctx = cast.framework.CastContext.getInstance()
    if (!ctx.getCurrentSession()) await ctx.requestSession()
    const session = ctx.getCurrentSession()
    if (!session) return
    const m = chrome.cast.media
    const Image = chrome.cast.Image
    const describe = (item: QueuedMedia) => {
      const info = new m.MediaInfo(item.url, 'application/x-mpegURL')
      info.streamType = m.StreamType.BUFFERED
      // Yoru's segments are fragmented MP4; the receiver assumes TS unless told.
      info.hlsSegmentFormat = m.HlsSegmentFormat.FMP4
      info.hlsVideoSegmentFormat = m.HlsVideoSegmentFormat.FMP4
      const meta = new m.GenericMediaMetadata()
      meta.title = item.title
      meta.subtitle = item.subtitle
      if (item.artwork) meta.images = [new Image(item.artwork)]
      info.metadata = meta
      info.tracks = item.subtitles.map((s, i) => {
        const track = new m.Track(i + 1, m.TrackType.TEXT)
        track.trackContentId = s.url
        track.trackContentType = 'text/vtt'
        track.subtype = m.TextTrackType.SUBTITLES
        track.name = s.title || s.language || `Subtitles ${i + 1}`
        track.language = s.language || 'und'
        return track
      })
      if (media.textStyle) info.textTrackStyle = Object.assign(new m.TextTrackStyle(), media.textStyle)
      return info
    }
    const all = [media, ...(media.queue ?? [])]
    // Before the load: the TV may report the new media before it settles.
    const before = sent
    sent = { items: all.map((i) => ({ key: i.key, url: i.url })), seen: false }
    try {
      if (all.length > 1) {
        // Each plays on its own and loads a little before the last ends: the TV goes on with this page shut.
        const items = all.map((item, i) => {
          const q = new m.QueueItem(describe(item))
          q.autoplay = true
          q.preloadTime = 30
          q.startTime = i === 0 ? media.startAt : 0
          if (item.subtitleTrack != null) q.activeTrackIds = [item.subtitleTrack + 1]
          return q
        })
        const request = new m.QueueLoadRequest(items)
        request.startIndex = 0
        await new Promise<void>((done, failed) =>
          session.getSessionObj().queueLoad(request, () => done(), (err) =>
            failed(new Error(err?.description || err?.code || "the TV didn't take the episodes")),
          ),
        )
      } else {
        const request = new m.LoadRequest(describe(media))
        request.currentTime = media.startAt
        request.autoplay = true
        if (media.subtitleTrack != null) request.activeTrackIds = [media.subtitleTrack + 1]
        await session.loadMedia(request)
      }
    } catch (e) {
      sent = before
      throw e
    }
    setPlaying(media.key)
  }, [])

  /** The TV connected, else Chrome's own list, which also makes it search (on a Mac it finds TVs only once
   *  something asks); false when that list was closed without a pick. */
  const connect = useCallback(async () => {
    const ctx = sdk().cast?.framework.CastContext.getInstance()
    if (!ctx) throw new Error('Casting is not available in this browser')
    if (ctx.getCurrentSession()) return true
    try {
      await ctx.requestSession()
    } catch (e) {
      // "cancel": closed without a pick, nothing went wrong.
      if (/cancel/i.test(e instanceof Error ? e.message : String(e))) return false
      throw e
    }
    return !!ctx.getCurrentSession()
  }, [])

  const stop = useCallback(() => sdk().cast?.framework.CastContext.getInstance().endCurrentSession(true), [])
  const playPause = useCallback(() => controller.current?.playOrPause(), [])
  const seek = useCallback((to: number) => {
    if (!player.current || !controller.current) return
    player.current.currentTime = to
    controller.current.seek()
  }, [])

  return { phase, sdkState, device, remote, playing, connect, start, stop, playPause, seek }
}
