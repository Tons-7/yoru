import { useCallback, useEffect, useEffectEvent, useRef, useState, type RefObject } from 'react'
import type Hls from 'hls.js'
import type JASSUB from 'jassub'
import jassubWorkerUrl from './subtitle-worker?worker&url'
import jassubWasmUrl from 'jassub/dist/wasm/jassub-worker.wasm?url'
import jassubModernWasmUrl from 'jassub/dist/wasm/jassub-worker-modern.wasm?url'
import type { PgsRenderer } from 'libpgs'
import pgsWorkerUrl from 'libpgs/dist/libpgs.worker.js?url'
import type { SkipRange, StreamInfo, SubtitleTrack } from '../lib/api'
import { restyle as restyleASS, type SubLook } from './subLook'
import { airplayHere } from './airplay'
import { setFloating } from '../lib/miniplayer'

const REFUSED_CODEC = new Set<string>(['bufferAddCodecError', 'bufferIncompatibleCodecsError', 'manifestIncompatibleCodecsError'])

export function useHlsSource(
  video: HTMLVideoElement | null,
  playlist?: string,
  startAt = 0,
  onGone?: () => void,
  onUndecodable?: () => boolean,
  airplay?: string,
) {
  const [failure, setFailure] = useState<{
    video: HTMLVideoElement
    playlist: string
    message: string
    final?: boolean
  } | null>(null)
  const current = failure && failure.video === video && failure.playlist === playlist ? failure : null
  const error = current?.message ?? null
  const hls = useRef<Hls | null>(null)
  const streamEnded = useRef(false)
  const readStart = useEffectEvent(() => startAt)
  const gone = useEffectEvent(() => onGone?.())
  const undecodable = useEffectEvent(() => !!onUndecodable?.())
  const readAirplay = useEffectEvent(() => airplay)
  const alternative = useRef<HTMLSourceElement | null>(null)
  useEffect(() => {
    if (alternative.current && airplay) alternative.current.src = airplay
  }, [airplay])

  useEffect(() => {
    if (!video || !playlist) return
    const setError = (message: string, final = false) => setFailure({ video, playlist, message, final })

    let cancelled = false
    let native = false
    streamEnded.current = false
    let seek: (() => void) | undefined
    let recoveries = 0
    let recoveredAt = 0
    const onMediaError = () => {
      if (!native) return
      const code = video.error?.code
      const decode = code === MediaError.MEDIA_ERR_DECODE || code === MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED
      if (decode && undecodable()) return
      setError(decode ? "This device can't decode this release." : 'Playback failed')
    }
    video.addEventListener('error', onMediaError)
    const onSeeking = () => {
      streamEnded.current = false
    }
    video.addEventListener('seeking', onSeeking)

    void (async () => {
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
        startPosition: start > 0 ? start : -1,
        maxBufferLength: 20,
        maxMaxBufferLength: 60,
        lowLatencyMode: false,
        manifestLoadingMaxRetry: 8,
        manifestLoadingRetryDelay: 800,
        levelLoadingMaxRetry: 8,
        fragLoadingMaxRetry: 10,
      })

      instance.on(HlsCtor.Events.BUFFER_EOS, () => {
        streamEnded.current = true
      })
      instance.on(HlsCtor.Events.BUFFER_RESET, () => {
        streamEnded.current = false
      })
      instance.on(HlsCtor.Events.ERROR, (_event, data) => {
        if (data.response?.code === 422) {
          instance.stopLoad()
          setError("Yoru couldn't convert this release to play here. Try another release, or mpv.", true)
          return
        }
        if (REFUSED_CODEC.has(data.details) && undecodable()) {
          instance.stopLoad()
          return
        }
        if (!data.fatal) return
        switch (data.type) {
          case HlsCtor.ErrorTypes.NETWORK_ERROR:
            if (data.response?.code === 404) {
              instance.stopLoad()
              gone()
            } else {
              instance.startLoad()
            }
            break
          case HlsCtor.ErrorTypes.MEDIA_ERROR: {
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
            setError(
              /manifest|level/i.test(data.details ?? '')
                ? "The episode's stream could not be loaded."
                : 'Playback stopped unexpectedly.',
            )
        }
      })

      const offer = () => {
        const remote = readAirplay()
        if (!remote || !airplayHere() || cancelled) return
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
      alternative.current?.remove()
      alternative.current = null
      if (native && seek) video.removeEventListener('loadedmetadata', seek)
      video.removeAttribute('src')
      video.load()
      setFailure(null)
    }
  }, [video, playlist])

  return { error, final: !!current?.final, hls, streamEnded }
}

export function useSeekRecovery(video: HTMLVideoElement | null, hls: RefObject<Hls | null>) {
  useEffect(() => {
    if (!video) return
    const request = video.requestVideoFrameCallback?.bind(video)
    if (!request) return

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
        if (video.paused || video.seeking || video.readyState < 3) return
        if (document.visibilityState !== 'visible') return
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

const KEEPALIVE_EVERY = 45_000

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
      } catch {}
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

const STALL_COUNTS = 2_000
const SLOW_STALLS = 2
const SLOW_WINDOW = 90_000

export function useSlowLink(video: HTMLVideoElement | null, onSlow?: () => void) {
  const slow = useEffectEvent(() => onSlow?.())
  const watching = !!onSlow
  useEffect(() => {
    if (!video || !watching) return
    let since = 0
    let stalls: number[] = []
    let seeking = false
    const waiting = () => {
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
  scale: number
  delay: number
  preset?: SubLook
  raised?: boolean
}

type ASSStyle = Awaited<ReturnType<JASSUB['renderer']['getStyles']>>[number]

const TYPESET_STYLE = /sign|song|kara|title|logo|note|insert|\b(op|ed)\b/i

export function useSubtitles(
  video: HTMLVideoElement | null,
  stream: StreamInfo | undefined,
  trackIndex: number | null,
  fonts: string[] | null = [],
  rebuild?: unknown,
  look: SubtitleLook = { scale: 1, delay: 0 },
  onProblem?: (message: string) => void,
): boolean {
  const instance = useRef<JASSUB | null>(null)
  const pictures = useRef<PgsRenderer | null>(null)
  const destroying = useRef<Promise<unknown> | undefined>(undefined)
  const lookRef = useRef(look)
  lookRef.current = look
  const restyle = useRef<(() => Promise<void>) | null>(null)
  const repaint = useRef<(() => void) | null>(null)

  const fontsRef = useRef<string[]>([])
  fontsRef.current = fonts ?? []
  const fontsKnown = useRef(false)
  fontsKnown.current = fonts !== null
  const fontKey = fonts?.join('|') ?? null
  const fontsIn = useRef(new Set<string>())
  const fontsLoading = useRef<Promise<unknown>>(Promise.resolve())
  const drawing = useRef('')
  const [readyFor, setReadyFor] = useState('')

  const catchUp = useEffectEvent(async () => {
    const r = instance.current
    const drawn = drawing.current
    if (!r || !drawn) return
    const fresh = fontsRef.current.filter((url) => !fontsIn.current.has(url))
    for (const url of fresh) fontsIn.current.add(url)
    if (fresh.length) fontsLoading.current = Promise.all([fontsLoading.current, r.renderer?.addFonts(fresh).catch(() => {})])
    await fontsLoading.current
    if (instance.current !== r) return
    if (fresh.length) repaint.current?.()
    if (fontsKnown.current) setReadyFor(drawn)
  })
  useEffect(() => {
    void catchUp().catch(() => {})
  }, [fontKey])

  const [surface, setSurface] = useState(0)
  useEffect(() => {
    const rebuild = () => setSurface((n) => n + 1)
    document.addEventListener('fullscreenchange', rebuild)
    return () => document.removeEventListener('fullscreenchange', rebuild)
  }, [])

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
      await Promise.race([destroying.current?.catch(() => {}), new Promise((done) => setTimeout(done, 250))])
      if (cancelled || !track) return

      if (!video.videoWidth || !video.videoHeight) return
      const drawn = `${stream.id}|${track.index}`
      const settle = () => !cancelled && setReadyFor(drawn)

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
        if (!cancelled) {
          pgs.renderAtTimestamp(-1)
          pgs.renderAtTimestamp(video.currentTime + pgs.timeOffset)
        }
        return settle()
      }

      const { default: Renderer } = await import('jassub')
      const whole = track.whole ? await load('these subtitles').then((r) => r?.text() ?? null) : undefined
      if (cancelled || whole === null) return settle()

      fontsIn.current = new Set()
      fontsLoading.current = Promise.resolve()
      const renderer = new Renderer({
        video,
        ...(whole === undefined ? { subUrl: track.url } : { subContent: whole }),
        workerUrl: jassubWorkerUrl,
        wasmUrl: jassubWasmUrl,
        modernWasmUrl: jassubModernWasmUrl,
        prescaleFactor: 0.8,
        maxRenderHeight: 1080,
        timeOffset: -lookRef.current.delay,
      })
      instance.current = renderer

      try {
        await renderer.ready
      } catch (err) {
        console.error('subtitle renderer failed to start:', err)
      }
      if (cancelled) return

      const resync = () => {
        if (document.visibilityState !== 'visible') return
        if (!video.videoWidth || !video.videoHeight) return
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
      video.addEventListener('seeked', resync)
      const elsewhere = video.ownerDocument.defaultView
      let refit = 0
      const follow = () => {
        if (!elsewhere) return
        const canvas = video.parentElement?.querySelector<HTMLCanvasElement>('canvas.JASSUB')
        if (canvas && video.videoWidth && video.videoHeight) {
          const ratio = video.videoWidth / video.videoHeight
          const width = Math.min(video.clientWidth, video.clientHeight * ratio)
          const height = width / ratio
          canvas.style.width = `${Math.round(width)}px`
          canvas.style.height = `${Math.round(height)}px`
          canvas.style.left = `${video.offsetLeft + (video.clientWidth - width) / 2}px`
          canvas.style.top = `${video.offsetTop + (video.clientHeight - height) / 2}px`
        }
        elsewhere.clearTimeout(refit)
        refit = elsewhere.setTimeout(() => void renderer.resize().catch(() => {}), pipSettleMs)
      }
      const sized = elsewhere && elsewhere !== window ? new elsewhere.ResizeObserver(follow) : null
      sized?.observe(video)
      detach = () => {
        document.removeEventListener('visibilitychange', resync)
        video.removeEventListener('seeked', resync)
        sized?.disconnect()
        elsewhere?.clearTimeout(refit)
      }
      repaint.current = resync

      let originals: ASSStyle[] | null = null
      const lookKey = (scale: number, preset: string, raised: boolean) => `${scale}|${preset}|${raised}`
      let applied = ''
      restyle.current = async () => {
        const { scale, preset = '', raised = false } = lookRef.current
        const key = lookKey(scale, preset, raised)
        if (key === applied || cancelled) return
        const worker = renderer.renderer
        originals ??= await worker.getStyles()
        if (!originals.length) originals = null
        await Promise.all(
          (originals ?? []).map((s, i) =>
            TYPESET_STYLE.test(s.Name)
              ? undefined
              : worker.setStyle(restyleASS({ ...s, FontSize: s.FontSize * scale }, preset, raised), i),
          ),
        )
        applied = key
        resync()
        for (const ms of [300, 1500]) setTimeout(() => !cancelled && video.paused && resync(), ms)
      }
      resync()
      void restyle.current().catch(() => {})
      drawing.current = drawn
      void catchUp().catch(() => {})

      if (!track.whole) {
        void keepFilling(renderer, track.url, video, () => cancelled, () => {
          originals = null
          applied = lookKey(1, '', false)
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
  }, [look.scale, look.preset, look.raised])

  const track = trackIndex === null ? undefined : stream?.subtitles?.find((t) => t.index === trackIndex)
  return !stream || !track || readyFor === `${stream.id}|${track.index}`
}

function cueCount(ass: string): number {
  return (ass.match(/^Dialogue:/gm) ?? []).length
}

const CUE_GAP = 30

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
const FILL_FRESH = 1_000
const FILL_EVERY = 15_000
const FILL_QUIET = 45_000
const FILL_AHEAD = 120

function bufferedEnd(video: HTMLVideoElement): number {
  const at = video.currentTime
  for (let i = 0; i < video.buffered.length; i++) {
    if (video.buffered.start(i) <= at + 0.5 && video.buffered.end(i) >= at) return video.buffered.end(i)
  }
  return at
}

export async function untilWatching(video: HTMLVideoElement | null, cancelled: () => boolean) {
  while (!cancelled() && (document.visibilityState !== 'visible' || (video && video.paused))) {
    await new Promise((r) => setTimeout(r, 1000))
  }
}

async function keepFilling(
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
  let askedUpTo = 0

  for (let attempt = 0; !cancelled(); attempt++) {
    const near = coverageFrom(loaded, video.currentTime) - video.currentTime < FILL_AHEAD
    const wait = near ? (quiet >= 6 ? FILL_EVERY : FILL_SOON) : quiet >= 3 ? FILL_QUIET : FILL_EVERY
    const began = Date.now()
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
      const res = await fetch(`${url}?fill=${attempt}&at=${Math.floor(video.currentTime)}`)
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
    } catch {}
  }
}

export function useEmbeddedFonts(fontsUrl?: string, generation = 0): string[] | null {
  const [answer, setAnswer] = useState<{ url: string; fonts: string[] }>()

  useEffect(() => {
    if (!fontsUrl) return
    const answered = (fonts: string[]) =>
      setAnswer((prev) =>
        prev?.url === fontsUrl && prev.fonts.join('|') === fonts.join('|') ? prev : { url: fontsUrl, fonts },
      )

    let cancelled = false
    let attempts = 0

    const poll = async () => {
      if (cancelled) return
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
      } catch {}
      window.setTimeout(poll, Math.min(2000, 250 * 2 ** (attempts - 1)))
    }

    void poll()
    return () => {
      cancelled = true
    }
  }, [fontsUrl, generation])

  if (!fontsUrl) return NO_FONTS
  return answer?.url === fontsUrl ? answer.fonts : null
}

const NO_FONTS: string[] = []

export type SkipName = 'opening' | 'ending' | 'recap' | 'preview' | 'eyecatch' | 'chapter'

export function skipKind(kind: string): SkipName | undefined {
  const k = kind.toLowerCase()
  if (k === 'op' || k === 'mixed-op') return 'opening'
  if (k === 'ed' || k === 'mixed-ed') return 'ending'
  if (k === 'recap') return 'recap'
  if (k === 'preview') return 'preview'
  if (k === 'eyecatch') return 'eyecatch'
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

export type Skipped = { range: SkipRange; at: number; byHand?: boolean }

export const SKIPPED_GUARD = 500

const SKIPPED_FOR = 5000

export function useAutoSkip(
  video: HTMLVideoElement | null,
  ranges: SkipRange[],
  enabled: AutoSkip,
  onKept?: (range: SkipRange) => void,
) {
  const skipped = useRef<Set<string>>(new Set())
  const [last, setLast] = useState<Skipped | null>(null)
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

  const dismiss = useCallback(() => {
    pending.current = null
    setLast(null)
  }, [])
  const note = useCallback(
    (range: SkipRange) => {
      skipped.current.add(`${range.kind}-${range.start}`)
      offer({ range, at: Date.now(), byHand: true })
    },
    [offer],
  )
  return { skipped: last, dismiss, note }
}

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
    const pip = await dpip.requestWindow(pipSize(container))

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
      } catch {}
    }
    pip.document.title = document.title
    pip.document.body.style.margin = '0'
    pip.document.body.style.background = '#000'
    pip.document.documentElement.style.height = '100%'
    pip.document.body.style.height = '100%'

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
    pip.document.body.style.overflow = 'hidden'
    pip.document.body.append(container)
    setActive(true)
    setWin(pip)
    setFloating(true)

    let settle = 0
    pip.addEventListener('resize', () => {
      container.style.setProperty('opacity', '0.99')
      pip.clearTimeout(settle)
      settle = pip.setTimeout(() => container.style.removeProperty('opacity'), pipSettleMs)
    })

    pip.addEventListener('pagehide', () => {
      pip.clearTimeout(settle)
      container.style.removeProperty('opacity')
      container.style.removeProperty('height')
      placeholder.replaceWith(container)
      setActive(false)
      setWin(null)
      setFloating(false)
    })
  }

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

const pipMinWidth = 320
const pipMaxWidth = 800
const pipSettleMs = 250

function pipSize(container: HTMLElement) {
  const video = container.querySelector('video')
  const ratio =
    video && video.videoWidth > 0 && video.videoHeight > 0
      ? video.videoWidth / video.videoHeight
      : 16 / 9

  const availableWidth = window.screen?.availWidth || window.innerWidth || 1280
  const availableHeight = window.screen?.availHeight || window.innerHeight || 720
  const width = Math.round(
    Math.min(pipMaxWidth, Math.max(pipMinWidth, availableWidth * 0.35), availableHeight * 0.45 * ratio),
  )
  return { width, height: Math.round(width / ratio) }
}

export interface Sheet {
  ready: boolean
  interval: number
  columns: number
  rows: number
  count: number
  width: number
  height: number
  url: string
  partial?: boolean
  have?: string
  version?: number
}

export function useThumbnails(streamId?: string, video?: HTMLVideoElement | null): Sheet | undefined {
  const [found, setFound] = useState<{ stream: string; sheet: Sheet }>()

  useEffect(() => {
    if (!streamId) return

    let timer = 0
    let cancelled = false
    const opened = Date.now()

    let asked = false
    const ask = async () => {
      try {
        if (asked) await untilWatching(video ?? null, () => cancelled)
        asked = true
        if (cancelled) return
        const res = await fetch(`/api/stream/${streamId}/thumbnails`)
        if (!res.ok || cancelled) return
        const body = (await res.json()) as Omit<Sheet, 'url'>
        if (cancelled || !body.interval) return

        if (body.ready) {
          const url = `/api/stream/${streamId}/thumbs.jpg?at=${opened}&v=${body.partial ? (body.version ?? 0) : 'whole'}`
          setFound((was) => (was?.stream === streamId && was.sheet.url === url ? was : { stream: streamId, sheet: { ...body, url } }))
          if (!body.partial) return
        }
        timer = window.setTimeout(ask, 15_000)
      } catch {}
    }
    void ask()

    return () => {
      cancelled = true
      window.clearTimeout(timer)
      setFound(undefined)
    }
  }, [streamId, video])

  return found && found.stream === streamId ? found.sheet : undefined
}

export function useFreshFrames(video: HTMLVideoElement | null, enabled: boolean): boolean {
  const [fresh, setFresh] = useState(false)

  useEffect(() => {
    if (!video || !enabled) return

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

    const stale = () => setFresh(false)
    video.addEventListener('seeking', stale)
    video.addEventListener('emptied', stale)

    return () => {
      cancelled = true
      video.cancelVideoFrameCallback?.(handle)
      video.removeEventListener('seeking', stale)
      video.removeEventListener('emptied', stale)
      setFresh(false)
    }
  }, [video, enabled])

  return fresh
}
