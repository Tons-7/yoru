import { useSyncExternalStore } from 'react'
import type { Anime4KPipeline } from 'anime4k-webgpu'
import type { FhEngine, FhModel, FhPacer, FhStream } from './flyhigh_web'
import { api, fetchAuthed } from '../lib/api'
import { readKey, writeKey } from '../lib/storage'
import { delayAudio } from './audioDelay'
import { nearestRate } from './rates'

type Lib = typeof import('./flyhigh_web')

export interface SmoothFiles {
  ready: boolean
  version: string
  wanted: string
  canInstall: boolean
  progress?: { stage: string; bytes: number; total: number; error?: string }
}

export class SmoothMissing extends Error {}

export type DeviceSmooth = 'auto' | 'off' | 'on'
const DEVICE_KEY = 'yoru.smooth'

export function deviceSmooth(): DeviceSmooth {
  const v = readKey(DEVICE_KEY)
  return v === 'on' || v === 'off' ? v : 'auto'
}
export const setDeviceSmooth = (choice: DeviceSmooth) => writeKey(DEVICE_KEY, choice === 'auto' ? null : choice)

const touchDevice = () => typeof matchMedia === 'function' && matchMedia('(hover: none) and (pointer: coarse)').matches

export function smoothHere(choice: DeviceSmooth, shared: boolean): boolean {
  if (choice === 'off') return false
  if (choice === 'on') return true
  return shared && !touchDevice()
}

export type SmoothRate = 'auto' | 'high' | 'mid' | 'low'
export const SMOOTH_RATES: readonly SmoothRate[] = ['auto', 'high', 'mid', 'low']
export const asSmoothRate = (v: string | undefined): SmoothRate =>
  SMOOTH_RATES.includes(v as SmoothRate) ? (v as SmoothRate) : 'auto'
export const RATE_NAMES: Record<SmoothRate, string> = { auto: 'Auto', high: 'Highest', mid: 'Middle', low: 'Lowest' }

export const HIGHEST_FPS = 120
export const ANIME_FPS = 24000 / 1001
const MAX_FACTOR = 6

export function factorsFor(screenHz: number, sourceHz: number): number[] {
  const source = sourceHz || ANIME_FPS
  const ceiling = Math.min(HIGHEST_FPS, screenHz || HIGHEST_FPS) + 0.5
  const out: number[] = []
  for (let n = MAX_FACTOR; n >= 2; n--) if (n * source <= ceiling) out.push(n)
  return out.length ? out : [1]
}

function bestFit(screenHz: number, sourceHz: number): number {
  const factors = factorsFor(screenHz, sourceHz)
  if (!screenHz) return factors[0]
  const even = factors.find((n) => {
    const ticks = screenHz / (n * (sourceHz || ANIME_FPS))
    return Math.abs(ticks - Math.round(ticks)) < 0.1
  })
  return even ?? factors[0]
}

export function rateFactor(rate: SmoothRate, screenHz: number, sourceHz: number, stepsDown = 0): number {
  const factors = factorsFor(screenHz, sourceHz)
  if (rate === 'auto') return factors[Math.min(factors.length - 1, factors.indexOf(bestFit(screenHz, sourceHz)) + stepsDown)]
  if (rate === 'high') return factors[0]
  if (rate === 'low') return factors[factors.length - 1]
  return factors[Math.floor(factors.length / 2)]
}

export function rateFps(rate: SmoothRate, screenHz: number, sourceHz: number): number {
  return rateFactor(rate, screenHz, sourceHz) * (sourceHz || ANIME_FPS)
}

function knownValue() {
  let value = 0
  const watchers = new Set<() => void>()
  return {
    get: () => value,
    tell: (next: number) => {
      if (next === value) return
      value = next
      for (const watcher of watchers) watcher()
    },
    subscribe: (watcher: () => void) => {
      watchers.add(watcher)
      return () => watchers.delete(watcher)
    },
  }
}
const knownScreen = knownValue()
const knownSource = knownValue()
const knownRunning = knownValue()
export const lastScreenHz = knownScreen.get
export const useKnownScreenHz = (): number => useSyncExternalStore(knownScreen.subscribe, knownScreen.get)
export const useKnownSourceHz = (): number => useSyncExternalStore(knownSource.subscribe, knownSource.get)
export const useRunningFps = (): number => useSyncExternalStore(knownRunning.subscribe, knownRunning.get)

export function rateLabel(rate: SmoothRate, screenHz: number, sourceHz: number, runningFps = 0): string {
  return `${runningFps || Math.round(rateFps(rate, screenHz, sourceHz))} fps`
}

const canvasOwners = new WeakMap<HTMLCanvasElement, object>()
export function claimCanvas(canvas: HTMLCanvasElement): object {
  const token = {}
  canvasOwners.set(canvas, token)
  return token
}
export const ownsCanvas = (canvas: HTMLCanvasElement, token: object) => canvasOwners.get(canvas) === token

export interface SmoothLive {
  shown: number
  screenHz: number
}
const live = new WeakMap<HTMLVideoElement, SmoothLive>()
export const smoothLive = (video: HTMLVideoElement): SmoothLive | undefined => live.get(video)

export type SmoothState = 'off' | 'starting' | 'on' | 'unsupported' | 'failed' | 'missing'

interface Engine {
  engine: FhEngine
  model: FhModel
  device: GPUDevice
}

let loading: Promise<Engine> | null = null
let broken: { err: unknown; at: number } | null = null
const RETRY_BROKEN_MS = 10 * 60_000
const lostListeners = new Set<() => void>()

export const forgetSmoothFailure = () => {
  broken = null
}

function smoothEngine(): Promise<Engine> {
  if (broken && Date.now() - broken.at < RETRY_BROKEN_MS) return Promise.reject(broken.err)
  broken = null
  loading ??= load().catch((err) => {
    loading = null
    throw err
  })
  return loading
}

const file = async (name: string) => {
  const res = await fetchAuthed(`/api/smooth/${name}`)
  if (!res.ok) throw new Error(`${name}: ${res.status}`)
  return res
}

let libLoading: Promise<Lib> | null = null

function engineLib(): Promise<Lib> {
  libLoading ??= file('flyhigh_web.js')
    .then((r) => r.text())
    .then(async (code) => {
      const url = URL.createObjectURL(new Blob([code], { type: 'text/javascript' }))
      try {
        const lib = (await import(/* @vite-ignore */ url)) as Lib
        await lib.default({ module_or_path: file('flyhigh_web_bg.wasm') })
        return lib
      } finally {
        URL.revokeObjectURL(url)
      }
    })
    .catch((err) => {
      libLoading = null
      throw err
    })
  return libLoading
}

async function load(): Promise<Engine> {
  const status = await api.get<SmoothFiles>('/api/smooth')
  if (!status.ready) throw new SmoothMissing('smooth motion is not installed on the PC')
  const [lib, graph, manifest, weights] = await Promise.all([
    engineLib(),
    file('graph.json').then((r) => r.text()),
    file('flyhighfg.json').then((r) => r.text()),
    file('flyhighfg.bin').then((r) => r.arrayBuffer()),
  ])
  let engine: FhEngine
  let device: GPUDevice
  let model: FhModel
  try {
    engine = await lib.FhEngine.create({})
    device = engine.device() as GPUDevice
    model = engine.loadModel(graph, manifest, new Uint8Array(weights))
  } catch (err) {
    broken = { err, at: Date.now() }
    throw err
  }
  void device.lost.then(() => {
    loading = null
    for (const listener of lostListeners) listener()
  })
  return { engine, model, device }
}

function recording(device: GPUDevice) {
  const made: Array<GPUTexture | GPUBuffer> = []
  const proxy = new Proxy(device, {
    get(target, prop) {
      if (prop === 'createTexture') {
        return (d: GPUTextureDescriptor) => {
          const t = target.createTexture(d)
          made.push(t)
          return t
        }
      }
      if (prop === 'createBuffer') {
        return (d: GPUBufferDescriptor) => {
          const b = target.createBuffer(d)
          made.push(b)
          return b
        }
      }
      const v = Reflect.get(target, prop, target)
      return typeof v === 'function' ? v.bind(target) : v
    },
  })
  return { device: proxy, release: () => made.splice(0).forEach((r) => r.destroy()) }
}

const BLIT = /* wgsl */ `
struct V { @builtin(position) p: vec4f, @location(0) uv: vec2f }
@vertex fn vs(@builtin(vertex_index) i: u32) -> V {
  var pos = array<vec2f, 3>(vec2f(-1, -1), vec2f(3, -1), vec2f(-1, 3));
  var o: V;
  o.p = vec4f(pos[i], 0, 1);
  o.uv = vec2f(pos[i].x * 0.5 + 0.5, 0.5 - pos[i].y * 0.5);
  return o;
}
@group(0) @binding(0) var t: texture_2d<f32>;
@group(0) @binding(1) var s: sampler;
@fragment fn fs(i: V) -> @location(0) vec4f { return vec4f(textureSampleLevel(t, s, i.uv, 0).rgb, 1); }`

const BLIT_VIDEO = BLIT.replace('texture_2d<f32>', 'texture_external').replace(
  'textureSampleLevel(t, s, i.uv, 0)',
  'textureSampleBaseClampToEdge(t, s, i.uv)',
)

const INPUTS = 12
const OUTPUTS = 12
const HISTORY = 8
const OFFSET_SAMPLES = 48
const SLOW_WINDOWS = 3
const SLOW_WINDOW_MS = 2000
const SLOW_DROPS_PER_SECOND = 5
const SLOW_LAG_MS = 220
const RECOVER_WINDOWS = 15

type Frame = { source: number } | { output: number }
interface Request {
  id: number
  previous: number
  current: number
  t: number
  probe?: boolean
}

export async function presentSmooth({
  video,
  canvas,
  held,
  rate,
  build,
  onSlow,
  onLost,
}: {
  video: HTMLVideoElement
  canvas: HTMLCanvasElement
  held: boolean
  rate: SmoothRate
  build?: (device: GPUDevice, input: GPUTexture, shared: boolean) => Anime4KPipeline[]
  onSlow: () => void
  onLost: () => void
}) {
  const { engine, model, device } = await smoothEngine()
  const lib = await engineLib()
  const context = canvas.getContext('webgpu')
  if (!context) throw new Error('no WebGPU canvas context')

  const run = { stopped: false }
  const undo: Array<() => void> = []
  const release = () => {
    for (const step of undo.splice(0).reverse()) {
      try {
        step()
      } catch (err) {
        console.debug('smooth motion: released after its device', err)
      }
    }
  }
  const lost = () => {
    if (!run.stopped) {
      run.stopped = true
      release()
      void delayAudio(video, 0).catch(() => {})
    }
    onLost()
  }
  lostListeners.add(lost)
  undo.push(() => lostListeners.delete(lost))

  try {
    return await start({ lib, engine, model, device, context, video, canvas, held, rate, build, onSlow, run, undo, release })
  } catch (err) {
    run.stopped = true
    release()
    throw err
  }
}

async function start({
  lib,
  engine,
  model,
  device,
  context,
  video,
  canvas,
  held,
  rate,
  build,
  onSlow,
  run,
  undo,
  release,
}: {
  lib: Lib
  engine: FhEngine
  model: FhModel
  device: GPUDevice
  context: GPUCanvasContext
  video: HTMLVideoElement
  canvas: HTMLCanvasElement
  held: boolean
  rate: SmoothRate
  build?: (device: GPUDevice, input: GPUTexture, shared: boolean) => Anime4KPipeline[]
  onSlow: () => void
  run: { stopped: boolean }
  undo: Array<() => void>
  release: () => void
}) {
  knownSource.tell(0)
  const native = { width: video.videoWidth, height: video.videoHeight }
  let upscaleInput: GPUTexture | undefined
  let pipelines: Anime4KPipeline[] = []
  let size = native
  if (build) {
    const upscaling = recording(device)
    undo.push(upscaling.release)
    upscaleInput = upscaling.device.createTexture({
      size: [native.width, native.height, 1],
      format: 'rgba16float',
      usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
    })
    pipelines = build(upscaling.device, upscaleInput, true)
    const out = pipelines[pipelines.length - 1].getOutputTexture()
    size = { width: out.width, height: out.height }
  }
  canvas.setAttribute('width', String(size.width))
  canvas.setAttribute('height', String(size.height))

  const stream: FhStream = engine.stream(
    model,
    { width: size.width, height: size.height },
    { mode: 'video', cadence: held, held_frames: { mean: 0.0033, max: 0.06 }, inputs: INPUTS, outputs: OUTPUTS },
  )
  undo.push(() => stream.free())
  const base: { mode: { factor: number }; lookahead: boolean; fps_limit: number } = {
    mode: { factor: rateFactor(rate, lastScreenHz(), 0) },
    lookahead: true,
    fps_limit: HIGHEST_FPS,
  }
  const pacer: FhPacer = new lib.FhPacer({ ...base, playback_rate: video.playbackRate })
  undo.push(() => pacer.free())

  const format = navigator.gpu.getPreferredCanvasFormat()
  context.configure({ device, format, alphaMode: 'opaque' })
  const token = claimCanvas(canvas)
  undo.push(() => {
    if (ownsCanvas(canvas, token)) context.unconfigure()
  })

  const sampler = device.createSampler({ magFilter: 'linear', minFilter: 'linear' })
  const blitModule = device.createShaderModule({ code: BLIT })
  const pipelineFor = (module: GPUShaderModule, target: GPUTextureFormat) =>
    device.createRenderPipeline({
      layout: 'auto',
      vertex: { module, entryPoint: 'vs' },
      fragment: { module, entryPoint: 'fs', targets: [{ format: target }] },
      primitive: { topology: 'triangle-list' },
    })
  const toScreen = pipelineFor(blitModule, format)
  const toSlot = pipelineFor(blitModule, 'rgba8unorm')
  let fromVideo: GPURenderPipeline | undefined

  const draw = (encoder: GPUCommandEncoder, pipeline: GPURenderPipeline, group: GPUBindGroup, view: GPUTextureView) => {
    const pass = encoder.beginRenderPass({
      colorAttachments: [{ view, loadOp: 'clear', clearValue: { r: 0, g: 0, b: 0, a: 1 }, storeOp: 'store' }],
    })
    pass.setPipeline(pipeline)
    pass.setBindGroup(0, group)
    pass.draw(3)
    pass.end()
  }
  const groupFor = (pipeline: GPURenderPipeline, texture: GPUTexture) =>
    device.createBindGroup({
      layout: pipeline.getBindGroupLayout(0),
      entries: [
        { binding: 0, resource: texture.createView() },
        { binding: 1, resource: sampler },
      ],
    })

  const slotViews = new Map<number, GPUTextureView>()
  const slotView = (slot: number) => {
    let view = slotViews.get(slot)
    if (!view) {
      view = (stream.inputTexture(slot) as GPUTexture).createView()
      slotViews.set(slot, view)
    }
    return view
  }
  const upscaled = build ? groupFor(toSlot, pipelines[pipelines.length - 1].getOutputTexture()) : undefined

  let copyWorks = true
  const write = (slot: number) => {
    if (upscaleInput && upscaled) {
      if (video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA) {
        device.queue.copyExternalImageToTexture({ source: video }, { texture: upscaleInput }, [native.width, native.height])
      }
      const encoder = device.createCommandEncoder()
      for (const p of pipelines) p.pass(encoder)
      draw(encoder, toSlot, upscaled, slotView(slot))
      device.queue.submit([encoder.finish()])
      return
    }
    if (copyWorks) {
      try {
        stream.pushVideo(slot, video)
        return
      } catch (err) {
        if (!(err instanceof TypeError)) throw err
        copyWorks = false
      }
    }
    fromVideo ??= pipelineFor(device.createShaderModule({ code: BLIT_VIDEO }), 'rgba8unorm')
    const group = device.createBindGroup({
      layout: fromVideo.getBindGroupLayout(0),
      entries: [
        { binding: 0, resource: device.importExternalTexture({ source: video }) },
        { binding: 1, resource: sampler },
      ],
    })
    const encoder = device.createCommandEncoder()
    draw(encoder, fromVideo, group, slotView(slot))
    device.queue.submit([encoder.finish()])
  }

  const shownGroups = new Map<string, GPUBindGroup>()
  const present = (frame: Frame) => {
    const source = 'source' in frame
    const slot = source ? frame.source : frame.output
    const key = `${source ? 'i' : 'o'}${slot}`
    let group = shownGroups.get(key)
    if (!group) {
      group = groupFor(toScreen, (source ? stream.inputTexture(slot) : stream.outputTexture(slot)) as GPUTexture)
      shownGroups.set(key, group)
    }
    const encoder = device.createCommandEncoder()
    draw(encoder, toScreen, group, context.getCurrentTexture().createView())
    device.queue.submit([encoder.finish()])
  }

  let pending = 0
  let poking = false
  const poke = () => {
    if (pending <= 0 || run.stopped) {
      poking = false
      return
    }
    try {
      device.queue.submit([])
    } catch {
      poking = false
      return
    }
    setTimeout(poke, 1)
  }
  const nudge = <T>(promise: Promise<T>): Promise<T> => {
    pending++
    if (!poking) {
      poking = true
      setTimeout(poke, 8)
    }
    return promise.finally(() => pending--)
  }

  let pushes = 0
  let mids = 0
  const cost = (stage: 'pair' | 'mid', ms: Promise<number | null> | undefined) => {
    if (!ms) return
    nudge(ms)
      .then((v) => {
        if (v !== null && !run.stopped) pacer.costSample(performance.now(), stage, v)
      })
      .catch(() => {})
  }

  const pts = new Map<number, number>()
  const outPts = new Map<number, number>()
  let nextOut = 0
  let statsPending = false
  const generate = (now: number, requests: Request[]) => {
    for (const r of requests) {
      if (statsPending) {
        pacer.skipped(r.id)
        continue
      }
      const out = nextOut
      nextOut = (nextOut + 1) % OUTPUTS
      let written = false
      try {
        if (r.probe || mids++ % 30 === 0) {
          const g = stream.generateTimed(r.t, out) as { written: boolean; cost?: Promise<number | null> }
          cost('mid', g.cost)
          written = g.written
        } else {
          written = stream.generate(r.t, out)
        }
      } catch (err) {
        console.warn('smooth motion: a frame was not generated', err)
      }
      if (!written) {
        pacer.skipped(r.id)
        continue
      }
      pacer.generated(now, r.id, out)
      const a = pts.get(r.previous)
      const b = pts.get(r.current)
      if (a !== undefined && b !== undefined) outPts.set(out, a + (b - a) * r.t)
      else outPts.delete(out)
    }
  }

  const offsets: number[] = []
  let delayed = 0
  let lag = 0
  let audioFollows = true
  const followAudio = (frame: Frame) => {
    if (video.paused || video.seeking) return
    const shown = 'source' in frame ? pts.get(frame.source) : outPts.get(frame.output)
    if (shown === undefined) return
    offsets.push((video.currentTime * 1000 - shown) / (video.playbackRate || 1))
    if (offsets.length < OFFSET_SAMPLES) return
    const median = offsets.sort((x, y) => x - y)[offsets.length >> 1]
    offsets.length = 0
    lag = median
    if (!audioFollows) return
    const want = Math.round(Math.min(300, Math.max(0, median)))
    if (Math.abs(want - delayed) < 8) return
    delayed = want
    delayAudio(video, want)
      .then((ok) => {
        if (!ok) delayed = 0
      })
      .catch(() => {
        audioFollows = false
      })
  }

  let epoch = 0
  let prev = -1
  const recent: number[] = []
  let busy = false
  let handle = 0
  const onFrame = async (_now: number, meta: VideoFrameCallbackMetadata) => {
    if (run.stopped) return
    handle = video.requestVideoFrameCallback(onFrame)
    if (busy) return
    busy = true
    const mine = epoch
    try {
      const now = performance.now()
      const at = meta.mediaTime * 1000
      const slot = stream.nextInput(recent)
      if (slot < 0) return
      write(slot)
      pts.set(slot, at)
      let kind = 'motion'
      let motion: number | undefined
      if (prev >= 0) {
        const c = (await nudge(stream.classify(prev, slot))) as { kind: string; motion: number }
        if (run.stopped || mine !== epoch) return
        kind = c.kind
        motion = c.motion
      }
      generate(now, pacer.sourceFrame(now, { id: slot, ptsMs: at, kind, motion }) as Request[])
      let p: { statsPending?: boolean; cost?: Promise<number | null> }
      if (pushes++ % 30 === 0) {
        p = stream.pushTimed(slot, at)
        cost('pair', p.cost)
      } else {
        p = stream.push(slot, at)
      }
      prev = slot
      recent.push(slot)
      while (recent.length > HISTORY) recent.shift()
      if (p.statsPending) {
        statsPending = true
        try {
          await nudge(stream.stats())
        } finally {
          statsPending = false
        }
      }
    } catch (err) {
      if (!run.stopped) console.warn('smooth motion: a frame was skipped', err)
    } finally {
      busy = false
    }
  }

  let raf = 0
  let last = 0
  let windowStart = 0
  let lastDrops = -1
  let slowWindows = 0
  let slowFired = false
  const chargedDrops = () => {
    const c = (pacer.report() as { counters?: Record<string, number> }).counters ?? {}
    return (c.drops ?? 0) - (c.drops_stall_exempt ?? 0) - (c.drops_settle_exempt ?? 0)
  }
  let screenHz = 0
  let sourceHz = 0
  let checkedAt = 0
  let shownCount = 0
  let shownSince = 0
  let pace = rate
  let stepsDown = 0
  let cleanWindows = 0
  const applyPace = (now: number) => {
    const factor = rateFactor(pace, screenHz, sourceHz, stepsDown)
    const fps = Math.round(factor * (sourceHz || ANIME_FPS))
    canvas.dataset.target = String(fps)
    knownRunning.tell(fps)
    if (factor === base.mode.factor) return
    base.mode = { factor }
    pacer.setOptions(now, { ...base, playback_rate: video.playbackRate })
  }
  const settleScreen = (now: number) => {
    if (now - checkedAt < (screenHz ? 2000 : 250)) return
    checkedAt = now
    const r = pacer.report() as {
      display_ready?: boolean
      display?: { display_hz?: number }
      source_ready?: boolean
      source_hz?: number
    }
    const hz = r.display_ready && r.display?.display_hz ? nearestRate(r.display.display_hz) : screenHz
    const fps = r.source_ready && r.source_hz ? Math.round(r.source_hz * 1000) / 1000 : sourceHz
    if (hz === screenHz && fps === sourceHz) return
    screenHz = hz
    sourceHz = fps
    knownScreen.tell(screenHz)
    if (sourceHz) knownSource.tell(sourceHz)
    applyPace(now)
  }
  const countShown = (now: number) => {
    if (!shownSince) shownSince = now
    if (now - shownSince < 1000) return
    const shown = Math.round((shownCount * 1000) / (now - shownSince))
    shownCount = 0
    shownSince = now
    live.set(video, { shown, screenHz })
    canvas.dataset.shown = String(shown)
  }
  undo.push(() => {
    if (!ownsCanvas(canvas, token)) return
    live.delete(video)
    knownRunning.tell(0)
    delete canvas.dataset.shown
    delete canvas.dataset.target
  })
  const onTick = (now: number) => {
    if (run.stopped) return
    raf = requestAnimationFrame(onTick)
    if (last && now - last > 50) pacer.hostStall(now, now - last)
    last = now
    settleScreen(now)
    const tick = pacer.tick(now) as { present?: { frame: Frame }; generate: Request[] }
    if (tick.present) {
      present(tick.present.frame)
      followAudio(tick.present.frame)
      shownCount++
    }
    countShown(now)
    generate(now, tick.generate)

    if (!windowStart) windowStart = now
    if (now - windowStart < SLOW_WINDOW_MS || slowFired) return
    const drops = chargedDrops()
    const perSecond = lastDrops < 0 ? 0 : ((drops - lastDrops) * 1000) / (now - windowStart)
    lastDrops = drops
    windowStart = now
    if (video.paused) return
    const struggling = perSecond > SLOW_DROPS_PER_SECOND || lag > SLOW_LAG_MS
    slowWindows = struggling ? slowWindows + 1 : 0
    cleanWindows = struggling ? 0 : cleanWindows + 1
    if (stepsDown > 0 && cleanWindows >= RECOVER_WINDOWS) {
      stepsDown--
      cleanWindows = 0
      lastDrops = -1
      applyPace(now)
      return
    }
    if (slowWindows < SLOW_WINDOWS) return
    slowWindows = 0
    const factor = rateFactor(pace, screenHz, sourceHz, stepsDown)
    if (pace === 'auto' && rateFactor(pace, screenHz, sourceHz, stepsDown + 1) < factor) {
      stepsDown++
      lastDrops = -1
      applyPace(now)
      return
    }
    slowFired = true
    onSlow()
  }

  const restart = () => {
    epoch++
    pacer.reset(performance.now())
    stream.reset()
    prev = -1
    recent.length = 0
    pts.clear()
    outPts.clear()
    offsets.length = 0
    lastDrops = -1
    lag = 0
  }
  const speed = () => pacer.setOptions(performance.now(), { ...base, playback_rate: video.playbackRate })
  video.addEventListener('seeking', restart)
  video.addEventListener('ratechange', speed)
  undo.push(() => {
    video.removeEventListener('seeking', restart)
    video.removeEventListener('ratechange', speed)
  })

  stream.warmUp()
  await device.queue.onSubmittedWorkDone()
  if (run.stopped) throw new Error('stopped while starting')

  handle = video.requestVideoFrameCallback(onFrame)
  raf = requestAnimationFrame(onTick)
  undo.push(() => {
    video.cancelVideoFrameCallback(handle)
    cancelAnimationFrame(raf)
  })

  return {
    stop: () => {
      if (run.stopped) return
      run.stopped = true
      release()
      void delayAudio(video, 0).catch(() => {})
    },
    setRate: (next: SmoothRate) => {
      if (run.stopped || next === pace) return
      pace = next
      stepsDown = 0
      cleanWindows = 0
      applyPace(performance.now())
    },
  }
}
