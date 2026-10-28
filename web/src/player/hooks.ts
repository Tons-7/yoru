import { useCallback, useEffect, useEffectEvent, useRef, useState, type RefObject } from 'react'
import type Hls from 'hls.js'
import type JASSUB from 'jassub'
// ?worker&url, not ?url: the plain form copies worker.js verbatim, leaving its
// relative imports pointing at paths that do not exist in a flat bundle.
import jassubWorkerUrl from './subtitle-worker?worker&url'
import jassubWasmUrl from 'jassub/dist/wasm/jassub-worker.wasm?url'
import jassubModernWasmUrl from 'jassub/dist/wasm/jassub-worker-modern.wasm?url'
import type { PgsRenderer } from 'libpgs'
import pgsWorkerUrl from 'libpgs/dist/libpgs.worker.js?url'
import type { SkipRange, StreamInfo, SubtitleTrack } from '../lib/api'
import { restyle as restyleASS, type SubLook } from './subLook'
import { airplayHere } from './airplay'

// hls.js's details for a codec the browser won't take at all.
const REFUSED_CODEC = new Set<string>(['bufferAddCodecError', 'bufferIncompatibleCodecsError', 'manifestIncompatibleCodecsError'])

/** Attaches HLS with hls.js wherever it runs (Chrome's own HLS hides errors); native only without MSE. */
export function useHlsSource(
  video: HTMLVideoElement | null,
  playlist?: string,
  startAt = 0,
  onGone?: () => void,
  /** A decode failure; true when it is handled (reopened converted), so no error is shown. */
  onUndecodable?: () => boolean,
  /** The playlist an Apple TV fetches itself, offered beside the page's own source where AirPlay is. */
  airplay?: string,
) {
  // Tagged with what it was about: the last episode's error never sits over the next one's search.
  const [failure, setFailure] = useState<{
    video: HTMLVideoElement
    playlist: string
    message: string
    final?: boolean
  } | null>(null)
  const current = failure && failure.video === video && failure.playlist === playlist ? failure : null
  const error = current?.message ?? null
  const hls = useRef<Hls | null>(null)
  // hls.js has appended the playlist's last segment: what is buffered is all there will be.
  const streamEnded = useRef(false)
  // Read at attach time, not tracked: a resume update mid-episode must not tear the player down.
  const readStart = useEffectEvent(() => startAt)
  const gone = useEffectEvent(() => onGone?.())
  const undecodable = useEffectEvent(() => !!onUndecodable?.())
  // Read at attach time too: a new signed link mid-episode needn't rebuild the player.
  const readAirplay = useEffectEvent(() => airplay)
  // The Apple TV's source beside hls.js's, where AirPlay is.
  const alternative = useRef<HTMLSourceElement | null>(null)
  // Its link follows the subtitles picked here as they change, for when an Apple TV is picked.
  useEffect(() => {
    if (alternative.current && airplay) alternative.current.src = airplay
  }, [airplay])

  useEffect(() => {
    if (!video || !playlist) return
    // final: trying the same release again can't help.
    const setError = (message: string, final = false) => setFailure({ video, playlist, message, final })

    let cancelled = false
    let native = false
    streamEnded.current = false
    let seek: (() => void) | undefined
    let recoveries = 0
    let recoveredAt = 0
    // Native HLS (iOS) reports failure only here.
    const onMediaError = () => {
      if (!native) return
      const code = video.error?.code
      const decode = code === MediaError.MEDIA_ERR_DECODE || code === MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED
      if (decode && undecodable()) return
      setError(decode ? "This device can't decode this release." : 'Playback failed')
    }
    video.addEventListener('error', onMediaError)
    // A seek takes hls.js out of its ended state; it says so again once it reaches the end.
    const onSeeking = () => {
      streamEnded.current = false
    }
    video.addEventListener('seeking', onSeeking)

    void (async () => {
      // A stale chunk after an update otherwise left the player loading forever.
      const loaded = await import('hls.js').catch(() => null)
      if (cancelled) return
      if (!loaded) {
        setError('The player could not load. Reload the page.')
        return
      }
      const HlsCtor = loaded.default

      if (!HlsCtor.isSupported()) {
        if (!video.canPlayType('application/vnd.apple.mpegurl')) {
          setError('This browser cannot play HLS.')
          return
        }
        native = true
        video.setAttribute('src', playlist)
        // A plain src reload starts at zero unless seeked by hand.
        const at = readStart()
        if (at > 0) {
          seek = () => {
            if (Math.abs(video.currentTime - at) > 2) video.currentTime = at
          }
          video.addEventListener('loadedmetadata', seek, { once: true })
        }
        return
      }

      const start = readStart()
      const instance = new HlsCtor({
        // The resume segment first, so the encoder started there isn't restarted from zero.
        startPosition: start > 0 ? start : -1,
        // A modest lookahead: asking far past one in-order encoder reads as a stall.
        maxBufferLength: 20,
        maxMaxBufferLength: 60,
        // No LL-HLS parts here; it only adds edge-seeking behaviour to a plain on-demand transcode.
        lowLatencyMode: false,
        manifestLoadingMaxRetry: 8,
        manifestLoadingRetryDelay: 800,
        levelLoadingMaxRetry: 8,
        fragLoadingMaxRetry: 10,
      })

      instance.on(HlsCtor.Events.BUFFER_EOS, () => {
        streamEnded.current = true
      })
      // A recovery empties the buffers: what was all there is no longer is.
      instance.on(HlsCtor.Events.BUFFER_RESET, () => {
        streamEnded.current = false
      })
      instance.on(HlsCtor.Events.ERROR, (_event, data) => {
        // 422: the encoder fails every pass (transcode.Unconvertible); said now, not after hls.js retries.
        if (data.response?.code === 422) {
          instance.stopLoad()
          setError("Yoru couldn't convert this release to play here. Try another release, or mpv.", true)
          return
        }
        // A codec refused outright, even one track of two (the picture would play silent): no recovery helps.
        if (REFUSED_CODEC.has(data.details) && undecodable()) {
          instance.stopLoad()
          return
        }
        if (!data.fatal) return
        switch (data.type) {
          case HlsCtor.ErrorTypes.NETWORK_ERROR:
            // 404 means the session was reaped, not a flaky network; retrying
            // can never succeed. Stop and let the reopen rebuild the player.
            if (data.response?.code === 404) {
              instance.stopLoad()
              gone()
            } else {
              instance.startLoad()
            }
            break
          case HlsCtor.ErrorTypes.MEDIA_ERROR: {
            // A codec this browser cannot decode fails the same way forever;
            // recover twice (the second time swapping audio codec), then say so.
            const now = Date.now()
            recoveries = now - recoveredAt < 10_000 ? recoveries + 1 : 1
            recoveredAt = now
            if (recoveries > 2) {
              if (undecodable()) {
                instance.stopLoad()
                break
              }
              setError("This browser can't decode this release. Try mpv, or another release.")
              break
            }
            if (recoveries === 2) instance.swapAudioCodec()
            instance.recoverMediaError()
            break
          }
          default:
            // hls.js names like "manifestParsingError" mean nothing to a viewer.
            setError(
              /manifest|level/i.test(data.details ?? '')
                ? "The episode's stream could not be loaded."
                : 'Playback stopped unexpectedly.',
            )
        }
      })

      // Safari's managed media source goes to an Apple TV only beside an HLS source it fetches: that one,
      // second. hls.js empties the sources and turns remote playback off on each attach, a recovery's too.
      const offer = () => {
        const remote = readAirplay()
        if (!remote || !airplayHere() || cancelled) return
        // The attribute behind disableRemotePlayback, which hls.js set.
        video.removeAttribute('disableremoteplayback')
        const source = (alternative.current ??= video.ownerDocument.createElement('source'))
        source.type = 'application/x-mpegURL'
        source.src = remote
        if (source.parentNode !== video) video.append(source)
      }
      instance.on(HlsCtor.Events.MEDIA_ATTACHED, offer)

      instance.loadSource(playlist)
      instance.attachMedia(video)
      hls.current = instance
      offer()
    })()

    return () => {
      cancelled = true
      video.removeEventListener('error', onMediaError)
      video.removeEventListener('seeking', onSeeking)
      hls.current?.destroy()
      hls.current = null
      // Before the reload below, or Safari would play the Apple TV's source here.
      alternative.current?.remove()
      alternative.current = null
      if (native && seek) video.removeEventListener('loadedmetadata', seek)
      // Episodes share the element, emptied so the next source doesn't attach to a finished one.
      video.removeAttribute('src')
      video.load()
      // Every release of an episode has the same playlist URL: the next attach starts without this one's error.
      setFailure(null)
    }
  }, [video, playlist])

  return { error, final: !!current?.final, hls, streamEnded }
}

/** Nudges a phone decoder that stops presenting frames after a seek, only while the page itself paints. */
export function useSeekRecovery(video: HTMLVideoElement | null, hls: RefObject<Hls | null>) {
  useEffect(() => {
    if (!video) return
    const request = video.requestVideoFrameCallback?.bind(video)
    if (!request) return

    // Frames and paints are counted only while a seek is being watched, not for the player's whole life.
    let frames = 0
    let painted = 0
    let videoHandle = 0
    let paintHandle = 0
    let watching = false
    let timer = 0
    let attempts = 0

    const watch = () => {
      if (watching) return
      watching = true
      videoHandle = request(function onVideoFrame() {
        frames++
        if (watching) videoHandle = request(onVideoFrame)
      })
      paintHandle = requestAnimationFrame(function onPaint() {
        painted++
        if (watching) paintHandle = requestAnimationFrame(onPaint)
      })
    }
    const unwatch = () => {
      watching = false
      window.clearInterval(timer)
      video.cancelVideoFrameCallback?.(videoHandle)
      cancelAnimationFrame(paintHandle)
    }

    const arm = () => {
      window.clearInterval(timer)
      watch()
      const framesAtSeek = frames
      const paintedAtSeek = painted
      const timeAtSeek = video.currentTime
      timer = window.setInterval(() => {
        if (frames > framesAtSeek) {
          attempts = 0
          unwatch()
          return
        }
        // Nothing is expected while paused, still buffering, or out of sight.
        if (video.paused || video.seeking || video.readyState < 3) return
        if (document.visibilityState !== 'visible') return
        // The page is not painting either, so the video is not the odd one out.
        if (painted - paintedAtSeek < 10) return
        if (video.currentTime - timeAtSeek < 3) return

        unwatch()
        attempts++
        if (attempts === 1) {
          video.currentTime += 0.01
        } else if (attempts === 2) {
          hls.current?.recoverMediaError()
        }
      }, 1000)
    }

    video.addEventListener('seeked', arm)
    return () => {
      unwatch()
      video.removeEventListener('seeked', arm)
    }
  }, [video, hls])
}

// Well under the server's ten-minute idle reaper, even with hidden-tab throttling.
const KEEPALIVE_EVERY = 45_000

/** Pings the playlist while playing (not paused) so the reaper keeps it; a 404 has onLost reopen it. */
export function useKeepAlive(
  video: HTMLVideoElement | null,
  playlist?: string,
  onLost?: () => void,
) {
  const lost = useEffectEvent(() => onLost?.())

  useEffect(() => {
    if (!video || !playlist) return
    let cancelled = false

    const ping = async () => {
      try {
        const res = await fetch(playlist, { credentials: 'same-origin' })
        if (!cancelled && res.status === 404) lost()
      } catch {
        // Offline or mid-restart; the next ping tries again.
      }
    }
    const kick = () => void ping()

    const id = window.setInterval(() => {
      if (!video.paused && !video.ended) void ping()
    }, KEEPALIVE_EVERY)
    const onVisible = () => {
      if (document.visibilityState === 'visible') void ping()
    }
    document.addEventListener('visibilitychange', onVisible)
    video.addEventListener('play', kick)

    return () => {
      cancelled = true
      window.clearInterval(id)
      document.removeEventListener('visibilitychange', onVisible)
      video.removeEventListener('play', kick)
    }
  }, [video, playlist])
}

// A stall counts once it lasts this long, and this many within the window make a slow link.
const STALL_COUNTS = 2_000
const SLOW_STALLS = 2
const SLOW_WINDOW = 90_000

/** Tells onSlow when playback keeps stalling while it plays on: not the first buffering, nor a seek's. */
export function useSlowLink(video: HTMLVideoElement | null, onSlow?: () => void) {
  const slow = useEffectEvent(() => onSlow?.())
  const watching = !!onSlow
  useEffect(() => {
    if (!video || !watching) return
    let since = 0
    let stalls: number[] = []
    let seeking = false
    const waiting = () => {
      // Startup and seeks wait for data on any link.
      if (!seeking && video.currentTime > 10) since = Date.now()
    }
    const resumed = () => {
      const now = Date.now()
      if (since && now - since >= STALL_COUNTS) {
        stalls = [...stalls.filter((at) => now - at < SLOW_WINDOW), now]
        if (stalls.length >= SLOW_STALLS) {
          stalls = []
          slow()
        }
      }
      since = 0
    }
    const seek = () => {
      seeking = true
      since = 0
    }
    const sought = () => {
      seeking = false
    }
    video.addEventListener('waiting', waiting)
    video.addEventListener('playing', resumed)
    video.addEventListener('seeking', seek)
    video.addEventListener('seeked', sought)
    return () => {
      video.removeEventListener('waiting', waiting)
      video.removeEventListener('playing', resumed)
      video.removeEventListener('seeking', seek)
      video.removeEventListener('seeked', sought)
    }
  }, [video, watching])
}

/** Pokes a clock stopped with data buffered ahead (tiny seek, then recovery); no buffer is a slow encoder. */
export function useStallWatchdog(video: HTMLVideoElement | null, hls: RefObject<Hls | null>) {
  useEffect(() => {
    if (!video) return

    let last = -1
    let stalledFor = 0
    let attempts = 0

    const id = window.setInterval(() => {
      const t = video.currentTime
      if (
        document.visibilityState !== 'visible' ||
        video.paused || video.ended || video.seeking || t !== last
      ) {
        if (t !== last) attempts = 0
        last = t
        stalledFor = 0
        return
      }
      stalledFor++

      let ahead = false
      for (let i = 0; i < video.buffered.length; i++) {
        if (video.buffered.start(i) <= t + 0.5 && video.buffered.end(i) > t + 1) ahead = true
      }
      if (!ahead || stalledFor < 4 || attempts >= 2) return

      stalledFor = 0
      attempts++
      if (attempts === 1) video.currentTime = t + 0.05
      else hls.current?.recoverMediaError()
    }, 1000)

    return () => window.clearInterval(id)
  }, [video, hls])
}

export interface SubtitleLook {
  /** Dialogue size as a multiple of the file's own. */
  scale: number
  /** Seconds the subtitles show late; negative shows them early. */
  delay: number
  /** How dialogue is drawn over the file's own styling (see subLook.ts). */
  preset?: SubLook
}

type ASSStyle = Awaited<ReturnType<JASSUB['renderer']['getStyles']>>[number]

// Signs and songs are typeset to the picture; only dialogue is resized.
const TYPESET_STYLE = /sign|song|kara|title|logo|note|insert|\b(op|ed)\b/i

/** Renders ASS subtitles with JASSUB, as native tracks lose the styling and embedded fonts; true once it can draw. */
export function useSubtitles(
  video: HTMLVideoElement | null,
  stream: StreamInfo | undefined,
  trackIndex: number | null,
  /** The episode's fonts, null until the server has said which. */
  fonts: string[] | null = [],
  rebuild?: unknown,
  look: SubtitleLook = { scale: 1, delay: 0 },
  /** Why a track can't show yet, in words for the viewer. */
  onProblem?: (message: string) => void,
): boolean {
  const instance = useRef<JASSUB | null>(null)
  const pictures = useRef<PgsRenderer | null>(null)
  // The last instance's teardown, which the next one waits for.
  const destroying = useRef<Promise<unknown> | undefined>(undefined)
  const lookRef = useRef(look)
  lookRef.current = look
  // Set per renderer: re-applies the size, and redraws the current cue.
  const restyle = useRef<(() => Promise<void>) | null>(null)
  const repaint = useRef<(() => void) | null>(null)

  // Late fonts join the running renderer: rebuilding for them blanked an episode's opening lines.
  const fontsRef = useRef<string[]>([])
  fontsRef.current = fonts ?? []
  const fontsKnown = useRef(false)
  fontsKnown.current = fonts !== null
  const fontKey = fonts?.join('|') ?? null
  const fontsIn = useRef(new Set<string>())
  const fontsLoading = useRef<Promise<unknown>>(Promise.resolve())
  // What the running renderer draws ("stream|track"), and what it has its track and every font for.
  const drawing = useRef('')
  const [readyFor, setReadyFor] = useState('')

  // Hands the renderer the fonts it lacks; ready once all of the episode's are known and in.
  const catchUp = useEffectEvent(async () => {
    const r = instance.current
    const drawn = drawing.current
    if (!r || !drawn) return
    const fresh = fontsRef.current.filter((url) => !fontsIn.current.has(url))
    for (const url of fresh) fontsIn.current.add(url)
    if (fresh.length) fontsLoading.current = Promise.all([fontsLoading.current, r.renderer?.addFonts(fresh).catch(() => {})])
    await fontsLoading.current
    if (instance.current !== r) return
    // JASSUB doesn't redraw for a font it was given.
    if (fresh.length) repaint.current?.()
    if (fontsKnown.current) setReadyFor(drawn)
  })
  useEffect(() => {
    void catchUp().catch(() => {})
  }, [fontKey])

  // A fullscreen transition leaves the canvas opaque over the video: black
  // picture, controls and audio fine. It belongs to a worker, so rebuild it.
  const [surface, setSurface] = useState(0)
  useEffect(() => {
    const rebuild = () => setSurface((n) => n + 1)
    document.addEventListener('fullscreenchange', rebuild)
    return () => document.removeEventListener('fullscreenchange', rebuild)
  }, [])

  // Same opaque canvas at startup: the renderer sizes against the video, so
  // attaching before dimensions exist leaves it covering the picture. Rebuild.
  useEffect(() => {
    if (!video) return
    const rebuild = () => setSurface((n) => n + 1)
    video.addEventListener('loadedmetadata', rebuild)
    video.addEventListener('resize', rebuild)
    return () => {
      video.removeEventListener('loadedmetadata', rebuild)
      video.removeEventListener('resize', rebuild)
    }
  }, [video])

  useEffect(() => {
    if (!video || !stream) return

    const track: SubtitleTrack | undefined =
      trackIndex === null ? undefined : stream.subtitles?.find((t) => t.index === trackIndex)

    let cancelled = false
    let detach: (() => void) | undefined

    void (async () => {
      // Overlap left a dead renderer on screen; capped, as a teardown waits out a renderer still starting.
      await Promise.race([destroying.current?.catch(() => {}), new Promise((done) => setTimeout(done, 250))])
      // Superseded while waiting: the renderer here now is the next run's, which every cleanup hands over.
      if (cancelled || !track) return

      // No dimensions yet, no canvas; the listeners above rerun this once they arrive.
      if (!video.videoWidth || !video.videoHeight) return
      const drawn = `${stream.id}|${track.index}`
      // Nothing more is coming for this track (it failed, or the problem was said): don't wait on it.
      const settle = () => !cancelled && setReadyFor(drawn)

      // The track, or null once its problem is said; pictures still downloading (409) are asked for again.
      const load = async (what: string): Promise<Response | null> => {
        for (let said = false; !cancelled; await new Promise((r) => setTimeout(r, 30_000))) {
          const res = await fetch(track.url).catch(() => null)
          if (cancelled || res?.ok) return res
          if (!said) {
            const error = res ? ((await res.json().catch(() => null)) as { error?: string } | null)?.error : null
            onProblem?.(error ?? `Couldn't load ${what}`)
            said = true
            settle()
          }
          if (res?.status !== 409) return null
        }
        return null
      }

      if (track.pictures) {
        const res = await load('these picture subtitles')
        if (!res || cancelled) return settle()
        const { PgsRenderer } = await import('libpgs')
        const data = await res.arrayBuffer()
        if (cancelled) return
        const pgs = new PgsRenderer({ video, workerUrl: pgsWorkerUrl, timeOffset: -lookRef.current.delay })
        pictures.current = pgs
        await pgs.loadFromBuffer(data).catch(() => onProblem?.("Couldn't read these picture subtitles"))
        // libpgs never draws the first set (it starts as "last drawn") nor on a paused video: drawn here.
        if (!cancelled) {
          pgs.renderAtTimestamp(-1)
          pgs.renderAtTimestamp(video.currentTime + pgs.timeOffset)
        }
        return settle()
      }

      const { default: Renderer } = await import('jassub')
      // A whole track is read here and handed over as text, so a failure (Jimaku busy, a key gone) is said.
      const whole = track.whole ? await load('these subtitles').then((r) => r?.text() ?? null) : undefined
      if (cancelled || whole === null) return settle()

      // No fonts here: JASSUB fetches every one before the track, which a slow link waited seconds on.
      fontsIn.current = new Set()
      fontsLoading.current = Promise.resolve()
      const renderer = new Renderer({
        video,
        ...(whole === undefined ? { subUrl: track.url } : { subContent: whole }),
        workerUrl: jassubWorkerUrl,
        wasmUrl: jassubWasmUrl,
        modernWasmUrl: jassubModernWasmUrl,
        // Subtitles are authored at 1080p; rendering above that is waste.
        prescaleFactor: 0.8,
        // Capped: uncapped, every resize step repaints, and dragging a window edge locked the machine.
        maxRenderHeight: 1080,
        timeOffset: -lookRef.current.delay,
      })
      instance.current = renderer

      // Without this a startup failure is silent: nothing is ever drawn.
      try {
        await renderer.ready
      } catch (err) {
        console.error('subtitle renderer failed to start:', err)
      }
      if (cancelled) return

      // Draw the cue the clock is on now; the next frame, paused, never comes.
      const resync = () => {
        if (document.visibilityState !== 'visible') return
        if (!video.videoWidth || !video.videoHeight) return
        // Caught: a call still in flight when the renderer is torn down rejects on its released worker.
        void renderer
          .manualRender(
            {
              mediaTime: video.currentTime,
              width: video.videoWidth,
              height: video.videoHeight,
              expectedDisplayTime: performance.now(),
            },
            true,
          )
          ?.catch(() => {})
      }
      document.addEventListener('visibilitychange', resync)
      // A paused seek may present no new frame, leaving the old cue up.
      video.addEventListener('seeked', resync)
      detach = () => {
        document.removeEventListener('visibilitychange', resync)
        video.removeEventListener('seeked', resync)
      }
      repaint.current = resync

      // Made over from the file's own styles, reread after each refresh so steps never compound.
      let originals: ASSStyle[] | null = null
      // Nothing applied yet: the first pass sets the styles, which also draws a paused picture's line.
      let applied = ''
      restyle.current = async () => {
        const { scale, preset = '' } = lookRef.current
        if (`${scale}|${preset}` === applied || cancelled) return
        const worker = renderer.renderer
        originals ??= await worker.getStyles()
        if (!originals.length) originals = null
        await Promise.all(
          (originals ?? []).map((s, i) =>
            TYPESET_STYLE.test(s.Name) ? undefined : worker.setStyle(restyleASS({ ...s, FontSize: s.FontSize * scale }, preset), i),
          ),
        )
        applied = `${scale}|${preset}`
        resync()
        // A font loads as its line is first drawn, and JASSUB doesn't redraw then: a paused picture is redrawn.
        for (const ms of [300, 1500]) setTimeout(() => !cancelled && video.paused && resync(), ms)
      }
      resync()
      void restyle.current().catch(() => {})
      drawing.current = drawn
      void catchUp().catch(() => {})

      if (!track.whole) {
        void keepFilling(renderer, track.url, video, () => cancelled, () => {
          originals = null
          applied = '1|'
          void restyle.current?.().catch(() => {})
        }).catch(() => {})
      }
    })()

    return () => {
      cancelled = true
      detach?.()
      restyle.current = null
      repaint.current = null
      drawing.current = ''
      destroying.current = instance.current ? Promise.resolve(instance.current.destroy()) : undefined
      instance.current = null
      pictures.current?.dispose()
      pictures.current = null
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [video, stream, trackIndex, rebuild, surface])

  useEffect(() => {
    if (pictures.current) pictures.current.timeOffset = -look.delay
    if (!instance.current) return
    instance.current.timeOffset = -look.delay
    repaint.current?.()
  }, [look.delay])

  useEffect(() => {
    void restyle.current?.().catch(() => {})
  }, [look.scale, look.preset])

  const track = trackIndex === null ? undefined : stream?.subtitles?.find((t) => t.index === trackIndex)
  return !stream || !track || readyFor === `${stream.id}|${track.index}`
}

function cueCount(ass: string): number {
  return (ass.match(/^Dialogue:/gm) ?? []).length
}

// A gap larger than this is an unfilled hole in the download, not the silence between two lines of dialogue.
const CUE_GAP = 30

/** How far the cues cover contiguously from `from` seconds, not the furthest cue (holes). */
export function coverageFrom(ass: string, from: number): number {
  const cues: Array<[number, number]> = []
  for (const m of ass.matchAll(
    /^Dialogue:\s*[^,]*,(\d+):(\d+):(\d+(?:\.\d+)?),(\d+):(\d+):(\d+(?:\.\d+)?)/gm,
  )) {
    const start = Number(m[1]) * 3600 + Number(m[2]) * 60 + Number(m[3])
    const end = Number(m[4]) * 3600 + Number(m[5]) * 60 + Number(m[6])
    cues.push([start, end])
  }
  cues.sort((a, b) => a[0] - b[0])

  let reach = from
  for (const [start, end] of cues) {
    if (start > reach + CUE_GAP) break
    if (end > reach) reach = end
  }
  return reach
}

const FILL_SOON = 5_000
// The shortest gap between asks when new video keeps arriving.
const FILL_FRESH = 1_000
const FILL_EVERY = 15_000
const FILL_QUIET = 45_000
// How far past the playhead the track should reach before polling can relax.
const FILL_AHEAD = 120

/** Where the buffered video around the playhead ends. */
function bufferedEnd(video: HTMLVideoElement): number {
  const at = video.currentTime
  for (let i = 0; i < video.buffered.length; i++) {
    if (video.buffered.start(i) <= at + 0.5 && video.buffered.end(i) >= at) return video.buffered.end(i)
  }
  return at
}

// Polls wait here: on a paused or hidden tab they'd keep the session alive and hold the queue.
// Without a video, only until the page is on screen.
export async function untilWatching(video: HTMLVideoElement | null, cancelled: () => boolean) {
  while (!cancelled() && (document.visibilityState !== 'visible' || (video && video.paused))) {
    await new Promise((r) => setTimeout(r, 1000))
  }
}

/** Rereads a track while the source grows, counting cues, not reach (head and tail load first). */
async function keepFilling(
  // Behind a Comlink proxy, so setTrack is called as a method, or Comlink posts the proxy itself.
  renderer: { renderer?: { setTrack?: (content: string) => unknown } },
  url: string,
  video: HTMLVideoElement,
  cancelled: () => boolean,
  onTrack: () => void,
) {
  const worker = renderer.renderer
  if (!worker?.setTrack) return

  let cues = 0
  let loaded = ''
  let quiet = 0
  // How far the video was buffered at the last ask: the encoder writes a segment's lines with it.
  let askedUpTo = 0

  for (let attempt = 0; !cancelled(); attempt++) {
    // Poll sooner while just ahead of the playhead is uncovered, whatever a tail cue suggests.
    const near = coverageFrom(loaded, video.currentTime) - video.currentTime < FILL_AHEAD
    const wait = near ? (quiet >= 6 ? FILL_EVERY : FILL_SOON) : quiet >= 3 ? FILL_QUIET : FILL_EVERY
    const began = Date.now()
    // A seek lands somewhere with no lines yet, and new video brings new lines; ask then, not on the timer.
    await new Promise<void>((r) => {
      let soon = 0
      const done = () => {
        clearTimeout(timer)
        clearTimeout(soon)
        video.removeEventListener('seeked', done)
        video.removeEventListener('progress', grew)
        r()
      }
      const grew = () => {
        if (soon || bufferedEnd(video) < askedUpTo + 1) return
        soon = window.setTimeout(done, Math.max(0, FILL_FRESH - (Date.now() - began)))
      }
      const timer = setTimeout(done, wait)
      video.addEventListener('seeked', done, { once: true })
      video.addEventListener('progress', grew)
      grew()
    })
    askedUpTo = bufferedEnd(video)
    await untilWatching(video, cancelled)
    if (cancelled()) return

    try {
      // fill defeats the cache; at is the viewer's place, read directly while downloading.
      const res = await fetch(`${url}?fill=${attempt}&at=${Math.floor(video.currentTime)}`)
      // The session is gone; so is the player, shortly.
      if (res.status === 404) return
      if (!res.ok) continue

      const complete = res.headers.get('X-Yoru-Complete') === 'true'
      const text = await res.text()
      const got = cueCount(text)
      if (got > cues) {
        cues = got
        loaded = text
        quiet = 0
        await worker.setTrack(text)
        onTrack()
      } else {
        quiet++
      }
      if (complete) return
    } catch {
      // A failed refresh just means trying again.
    }
  }
}

/** Fonts arrive after the stream opens, null until the server says; a new generation (rebuilt session) asks again. */
export function useEmbeddedFonts(fontsUrl?: string, generation = 0): string[] | null {
  // Kept with the URL it answers, so a new episode's fonts read as unknown again.
  const [answer, setAnswer] = useState<{ url: string; fonts: string[] }>()

  useEffect(() => {
    if (!fontsUrl) return
    // Same list: keep the identity, so nothing is added again.
    const answered = (fonts: string[]) =>
      setAnswer((prev) =>
        prev?.url === fontsUrl && prev.fonts.join('|') === fonts.join('|') ? prev : { url: fontsUrl, fonts },
      )

    let cancelled = false
    let attempts = 0

    const poll = async () => {
      if (cancelled) return
      // Given up: none, so the episode's start stops waiting on them.
      if (attempts++ > 40) {
        setAnswer((prev) => (prev?.url === fontsUrl ? prev : { url: fontsUrl, fonts: [] }))
        return
      }
      try {
        const res = await fetch(fontsUrl, { credentials: 'same-origin' })
        const body = (await res.json()) as {
          ready: boolean
          fonts: Array<{ url: string }>
        }
        if (cancelled) return
        if (body.ready) {
          answered(body.fonts.map((f) => f.url))
          return
        }
      } catch {
        // A failed poll is not worth surfacing; the default font is readable.
      }
      // Quick at first: an episode's first lines wait on this answer.
      window.setTimeout(poll, Math.min(2000, 250 * 2 ** (attempts - 1)))
    }

    void poll()
    return () => {
      cancelled = true
    }
  }, [fontsUrl, generation])

  // None offered is a known none.
  if (!fontsUrl) return NO_FONTS
  return answer?.url === fontsUrl ? answer.fonts : null
}

const NO_FONTS: string[] = []

/** What an AniSkip range is: op and mixed-op open, ed and mixed-ed end, recap is its own. */
export type SkipName = 'opening' | 'ending' | 'recap' | 'preview' | 'eyecatch' | 'chapter'

export function skipKind(kind: string): SkipName | undefined {
  const k = kind.toLowerCase()
  if (k === 'op' || k === 'mixed-op') return 'opening'
  if (k === 'ed' || k === 'mixed-ed') return 'ending'
  if (k === 'recap') return 'recap'
  if (k === 'preview') return 'preview'
  if (k === 'eyecatch') return 'eyecatch'
  // A chapter named in Settings as one to skip.
  if (k === 'skip') return 'chapter'
  return undefined
}

export type AutoSkip = { op: boolean; ed: boolean; recap: boolean; preview: boolean }

function wanted(kind: SkipName | undefined, on: AutoSkip): boolean {
  switch (kind) {
    case 'opening':
      return on.op
    case 'ending':
      return on.ed
    case 'recap':
      return on.recap
    case 'preview':
    case 'eyecatch':
      return on.preview
    case 'chapter':
      return true
    default:
      return false
  }
}

/** What was just jumped over, offered back for a moment; byHand: Skip was pressed, so no "Watch it". */
export type Skipped = { range: SkipRange; at: number; byHand?: boolean }

// A tap this soon after the offer shows is the second half of a double tap on Skip, in the same place.
export const SKIPPED_GUARD = 500

// How long "Watch it" stays: long enough to reach for, short enough not to sit over the picture.
const SKIPPED_FOR = 3000

// Each range skips once: seeking back into one is deliberate. onKept hears of a skip left standing (no
// "Watch it", no "Wrong skip") once its moment has passed.
export function useAutoSkip(
  video: HTMLVideoElement | null,
  ranges: SkipRange[],
  enabled: AutoSkip,
  onKept?: (range: SkipRange) => void,
) {
  const skipped = useRef<Set<string>>(new Set())
  const [last, setLast] = useState<Skipped | null>(null)
  // The skip on offer, until it's undone or its moment passes; a newer one replacing it counts it kept.
  const pending = useRef<SkipRange | null>(null)
  const onKeptRef = useRef(onKept)
  useEffect(() => {
    onKeptRef.current = onKept
  })
  const offer = useCallback((next: Skipped) => {
    if (pending.current) onKeptRef.current?.(pending.current)
    pending.current = next.range
    setLast(next)
  }, [])

  useEffect(() => {
    skipped.current.clear()
  }, [ranges])

  useEffect(() => {
    if (!video || ranges.length === 0) return

    const onTime = () => {
      const t = video.currentTime
      for (const range of ranges) {
        // A shape change upstream must not throw inside timeupdate.
        if (!range?.kind || !wanted(skipKind(range.kind), enabled)) continue

        const key = `${range.kind}-${range.start}`
        if (skipped.current.has(key)) continue
        if (t >= range.start && t < range.end - 0.4) {
          skipped.current.add(key)
          video.currentTime = range.end
          offer({ range, at: Date.now() })
          return
        }
      }
    }

    video.addEventListener('timeupdate', onTime)
    return () => video.removeEventListener('timeupdate', onTime)
  }, [video, ranges, enabled, offer])

  useEffect(() => {
    if (!last) return
    const id = window.setTimeout(() => {
      if (pending.current) onKeptRef.current?.(pending.current)
      pending.current = null
      setLast(null)
    }, SKIPPED_FOR)
    return () => window.clearTimeout(id)
  }, [last])

  // Its key stays skipped, so "Watch it" seeking back to the start plays it through this time.
  const dismiss = useCallback(() => {
    pending.current = null
    setLast(null)
  }, [])
  // A Skip pressed by hand: kept, and open to a "Wrong skip" when it's AniSkip's.
  const note = useCallback(
    (range: SkipRange) => {
      skipped.current.add(`${range.kind}-${range.start}`)
      offer({ range, at: Date.now(), byHand: true })
    },
    [offer],
  )
  return { skipped: last, dismiss, note }
}

/** Floats the whole player (native PiP drops subtitles); container must be a portal host, to keep handlers. */
export function useDocumentPiP(container: HTMLElement | null) {
  const [active, setActive] = useState(false)
  const [win, setWin] = useState<Window | null>(null)
  const supported =
    typeof window !== 'undefined' && 'documentPictureInPicture' in window

  const toggle = async () => {
    if (!supported || !container) return

    const dpip = (window as unknown as {
      documentPictureInPicture: {
        window: Window | null
        requestWindow: (o: { width: number; height: number }) => Promise<Window>
      }
    }).documentPictureInPicture

    if (dpip.window) {
      dpip.window.close()
      setActive(false)
      return
    }

    const rect = container.getBoundingClientRect()
    // Not the on-page size: full width on a wide monitor opens a window partly off screen.
    const pip = await dpip.requestWindow(pipSize(container))

    // Styles are copied across, linked sheets relinked: inlined, their font URLs resolve against about:blank.
    for (const sheet of Array.from(document.styleSheets)) {
      if (sheet.href) {
        const link = pip.document.createElement('link')
        link.rel = 'stylesheet'
        link.href = sheet.href
        pip.document.head.append(link)
        continue
      }
      try {
        const style = pip.document.createElement('style')
        style.textContent = Array.from(sheet.cssRules)
          .map((rule) => rule.cssText)
          .join('')
        pip.document.head.append(style)
      } catch {
        // A cross-origin sheet cannot be read; nothing here depends on one.
      }
    }
    pip.document.title = document.title
    pip.document.body.style.margin = '0'
    pip.document.body.style.background = '#000'
    pip.document.documentElement.style.height = '100%'
    pip.document.body.style.height = '100%'

    // A placeholder holds the moved player's place, or the page collapses around the gap.
    const placeholder = document.createElement('div')
    placeholder.style.cssText = [
      `height:${Math.round(rect.height)}px`,
      'display:grid',
      'place-items:center',
      'background:#000',
      'border-radius:0.75rem',
      'color:#8891a5',
      'font-size:0.875rem',
    ].join(';')
    placeholder.textContent = 'Playing in picture in picture'

    container.replaceWith(placeholder)
    container.style.setProperty('height', '100%')
    pip.document.body.append(container)
    setActive(true)
    setWin(pip)

    pip.addEventListener('pagehide', () => {
      container.style.removeProperty('height')
      placeholder.replaceWith(container)
      setActive(false)
      setWin(null)
    })
  }

  // Leaving the page takes the player with it, so the floating window would be left holding a dead one.
  useEffect(() => {
    return () => {
      const w = (window as unknown as { documentPictureInPicture?: { window: Window | null } })
        .documentPictureInPicture?.window
      w?.close()
    }
  }, [])

  return { supported, active, toggle, window: win }
}

type WebkitVideo = HTMLVideoElement & {
  webkitSetPresentationMode?: (mode: 'inline' | 'picture-in-picture' | 'fullscreen') => void
  webkitPresentationMode?: string
}

/** The video alone in the system's floating window (phones, Safari); subtitles go as its text track. */
export function useVideoPiP(video: HTMLVideoElement | null) {
  const [active, setActive] = useState(false)
  const v = video as WebkitVideo | null
  const supported =
    !!v && ((document.pictureInPictureEnabled && !v.disablePictureInPicture) || typeof v.webkitSetPresentationMode === 'function')
  useEffect(() => {
    if (!v) return
    const on = () => setActive(true)
    const off = () => setActive(false)
    const mode = () => setActive(v.webkitPresentationMode === 'picture-in-picture')
    v.addEventListener('enterpictureinpicture', on)
    v.addEventListener('leavepictureinpicture', off)
    v.addEventListener('webkitpresentationmodechanged', mode)
    return () => {
      v.removeEventListener('enterpictureinpicture', on)
      v.removeEventListener('leavepictureinpicture', off)
      v.removeEventListener('webkitpresentationmodechanged', mode)
    }
  }, [v])
  const toggle = async () => {
    if (!v) return
    if (document.pictureInPictureElement) return document.exitPictureInPicture().catch(() => {})
    if (document.pictureInPictureEnabled && v.requestPictureInPicture) {
      await v.requestPictureInPicture().catch(() => {})
      return
    }
    v.webkitSetPresentationMode?.(v.webkitPresentationMode === 'picture-in-picture' ? 'inline' : 'picture-in-picture')
  }
  return { supported, active, toggle }
}

/** An iPhone's fullscreen: the system's own player, which shows only the video's text tracks. */
export function useSystemFullscreen(video: HTMLVideoElement | null) {
  const [active, setActive] = useState(false)
  useEffect(() => {
    if (!video) return
    const on = () => setActive(true)
    const off = () => setActive(false)
    video.addEventListener('webkitbeginfullscreen', on)
    video.addEventListener('webkitendfullscreen', off)
    return () => {
      video.removeEventListener('webkitbeginfullscreen', on)
      video.removeEventListener('webkitendfullscreen', off)
    }
  }, [video])
  return active
}

// Small enough to sit over other windows (a third of the screen at most), shaped like the video.
const pipMinWidth = 320
const pipMaxWidth = 800

function pipSize(container: HTMLElement) {
  const video = container.querySelector('video')
  const ratio =
    video && video.videoWidth > 0 && video.videoHeight > 0
      ? video.videoWidth / video.videoHeight
      : 16 / 9

  const available = window.screen?.availWidth || window.innerWidth || 1280
  const width = Math.round(
    Math.min(pipMaxWidth, Math.max(pipMinWidth, available * 0.35)),
  )
  return { width, height: Math.round(width / ratio) }
}

/** The scrub preview sheet, once ffmpeg has laid it out. */
export interface Sheet {
  ready: boolean
  interval: number
  columns: number
  rows: number
  count: number
  width: number
  height: number
  url: string
  /** A downloading file's sheet so far: have holds a 1 for each tile there, version counts them. */
  partial?: boolean
  have?: string
  version?: number
}

/** Asks for the preview sheet, polling while it is built and while a downloading file's fills in. */
export function useThumbnails(streamId?: string, video?: HTMLVideoElement | null): Sheet | undefined {
  // Tagged with its stream, so another episode never shows this one's sheet while it asks for its own.
  const [found, setFound] = useState<{ stream: string; sheet: Sheet }>()

  useEffect(() => {
    if (!streamId) return

    let timer = 0
    let cancelled = false
    const opened = Date.now()

    let asked = false
    const ask = async () => {
      try {
        // The first answer may already be ready; only the re-polls wait.
        if (asked) await untilWatching(video ?? null, () => cancelled)
        asked = true
        if (cancelled) return
        const res = await fetch(`/api/stream/${streamId}/thumbnails`)
        if (!res.ok || cancelled) return
        const body = (await res.json()) as Omit<Sheet, 'url'>
        if (cancelled || !body.interval) return

        if (body.ready) {
          // Each release of an episode has the same URL; stamped, the browser can't paint the last one's sheet.
          const url = `/api/stream/${streamId}/thumbs.jpg?at=${opened}&v=${body.partial ? (body.version ?? 0) : 'whole'}`
          // The same answer again keeps the sheet as it is, so the image isn't fetched anew.
          setFound((was) => (was?.stream === streamId && was.sheet.url === url ? was : { stream: streamId, sheet: { ...body, url } }))
          if (!body.partial) return
        }
        // Asked for as long as it plays: tiles come as the file downloads, the whole sheet once it has.
        timer = window.setTimeout(ask, 15_000)
      } catch {
        // A preview is a nicety; failing to get one is not worth reporting.
      }
    }
    void ask()

    return () => {
      cancelled = true
      window.clearTimeout(timer)
      // Another release reopens under the same id; its sheet is asked for afresh.
      setFound(undefined)
    }
  }, [streamId, video])

  return found && found.stream === streamId ? found.sheet : undefined
}

/** Whether the upscaler's canvas holds the video's current frame, which a pause, seek or lost device spoils. */
export function useFreshFrames(video: HTMLVideoElement | null, enabled: boolean): boolean {
  const [fresh, setFresh] = useState(false)

  useEffect(() => {
    if (!video || !enabled) return

    // Without it the canvas never claims to be current, costing upscaling, not the picture.
    const request = video.requestVideoFrameCallback?.bind(video)
    if (!request) return

    let handle = 0
    let cancelled = false

    const onFrame = () => {
      if (cancelled) return
      setFresh(true)
      handle = request(onFrame)
    }
    handle = request(onFrame)

    // A seek leaves the canvas on the old frame, so it stands down until the renderer catches up.
    const stale = () => setFresh(false)
    video.addEventListener('seeking', stale)
    video.addEventListener('emptied', stale)

    return () => {
      cancelled = true
      video.cancelVideoFrameCallback?.(handle)
      video.removeEventListener('seeking', stale)
      video.removeEventListener('emptied', stale)
      // Another video, or upscaling off, leaves the canvas on this one's last frame.
      setFresh(false)
    }
  }, [video, enabled])

  return fresh
}
