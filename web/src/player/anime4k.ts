import { useEffect, useEffectEvent, useRef, useState } from 'react'
import type { Anime4KPipeline } from 'anime4k-webgpu'
import { readKey, writeKey } from '../lib/storage'
import { api } from '../lib/api'
import { claimCanvas, presentSmooth, SmoothMissing, type SmoothFiles, type SmoothRate, type SmoothState } from './smooth'

export function webgpuAvailable(): boolean {
  return typeof navigator !== 'undefined' && 'gpu' in navigator
}

export type UpscaleTier = 'light' | 'balanced' | 'full'
export type DeviceUpscale = 'auto' | 'off' | UpscaleTier
export type AutoTier = UpscaleTier | 'off'

const DEVICE_KEY = 'yoru.anime4k'
const AUTO_KEY = 'yoru.anime4k.auto'
const TIERS: readonly string[] = ['light', 'balanced', 'full']

export function deviceUpscale(): DeviceUpscale {
  const v = readKey(DEVICE_KEY)
  if (v === 'on') return 'full'
  return v === 'off' || TIERS.includes(v ?? '') ? (v as DeviceUpscale) : 'auto'
}
export const setDeviceUpscale = (choice: DeviceUpscale) => writeKey(DEVICE_KEY, choice === 'auto' ? null : choice)

export function autoTier(): AutoTier | null {
  const v = readKey(AUTO_KEY)
  return v === 'off' || TIERS.includes(v ?? '') ? (v as AutoTier) : null
}
export const setAutoTier = (tier: AutoTier | null) => writeKey(AUTO_KEY, tier)

export const lowerTier = (tier: UpscaleTier): AutoTier => (tier === 'full' ? 'balanced' : tier === 'balanced' ? 'light' : 'off')

const touchDevice = () => typeof matchMedia === 'function' && matchMedia('(hover: none) and (pointer: coarse)').matches

export function upscaleHere(choice: DeviceUpscale, shared: boolean, settled: AutoTier | null) {
  const fallback: UpscaleTier = touchDevice() ? 'light' : 'full'
  if (choice === 'off') return { enabled: false, tier: fallback, auto: false }
  if (choice !== 'auto') return { enabled: true, tier: choice, auto: false }
  const tier = settled && settled !== 'off' ? settled : fallback
  return { enabled: shared && !touchDevice() && settled !== 'off', tier, auto: true }
}

type Mode = 'A' | 'B' | 'C' | 'A+A' | 'B+B' | 'C+A'
type Size = { width: number; height: number }
type Lib = typeof import('anime4k-webgpu')

function chain(lib: Lib, device: GPUDevice, input: GPUTexture, native: Size, target: Size, mode: Mode, tier: UpscaleTier) {
  const out: Anime4KPipeline[] = []
  let texture = input
  const add = (p: Anime4KPipeline) => {
    out.push(p)
    texture = p.getOutputTexture()
  }
  const here = () => ({ device, inputTexture: texture })

  let size = native
  if (target.width < native.width * 0.85) {
    add(new lib.Downscale({ ...here(), targetDimensions: target }))
    size = target
  }

  if (tier === 'full') {
    const presets = { A: lib.ModeA, B: lib.ModeB, C: lib.ModeC, 'A+A': lib.ModeAA, 'B+B': lib.ModeBB, 'C+A': lib.ModeCA }
    add(new presets[mode]({ ...here(), nativeDimensions: size, targetDimensions: target }))
    return out
  }

  const grows = target.width > size.width * 1.2 && target.height > size.height * 1.2
  const upscale = () => {
    add(new lib.CNNx2M(here()))
    if (target.width < size.width * 2) add(new lib.Downscale({ ...here(), targetDimensions: target }))
  }

  if (tier === 'light') {
    add(new lib.DoG(here()))
    if (grows) upscale()
    return out
  }

  const Restore = mode.startsWith('B') ? lib.CNNSoftM : lib.CNNM
  add(new lib.ClampHighlights(here()))
  if (mode !== 'C' && mode !== 'C+A') add(new Restore(here()))
  if (grows) upscale()
  if (mode === 'A+A' || mode === 'B+B') add(new Restore(here()))
  if (mode === 'C+A') add(new lib.CNNM(here()))
  return out
}

const PRESENT = /* wgsl */ `
struct Out { @builtin(position) pos: vec4f, @location(0) uv: vec2f }
@vertex fn vert_main(@builtin(vertex_index) i: u32) -> Out {
  var quad = array<vec2f, 6>(vec2f(-1, -1), vec2f(1, -1), vec2f(-1, 1), vec2f(-1, 1), vec2f(1, -1), vec2f(1, 1));
  var o: Out;
  o.pos = vec4f(quad[i], 0, 1);
  o.uv = vec2f((quad[i].x + 1) / 2, (1 - quad[i].y) / 2);
  return o;
}
@group(0) @binding(1) var samp: sampler;
@group(0) @binding(2) var tex: texture_2d<f32>;
@fragment fn main(@location(0) uv: vec2f) -> @location(0) vec4f { return textureSample(tex, samp, uv); }
`

async function present(
  video: HTMLVideoElement,
  canvas: HTMLCanvasElement,
  build: (device: GPUDevice, input: GPUTexture) => Anime4KPipeline[],
) {
  const adapter = await navigator.gpu.requestAdapter()
  if (!adapter) throw new Error('WebGPU not supported')
  const device = await adapter.requestDevice()
  try {
    return presentOn(device, video, canvas, build)
  } catch (err) {
    device.destroy()
    throw err
  }
}

function presentOn(
  device: GPUDevice,
  video: HTMLVideoElement,
  canvas: HTMLCanvasElement,
  build: (device: GPUDevice, input: GPUTexture) => Anime4KPipeline[],
) {
  const context = canvas.getContext('webgpu')
  if (!context) throw new Error('no WebGPU canvas context')
  const format = navigator.gpu.getPreferredCanvasFormat()
  context.configure({ device, format, alphaMode: 'premultiplied' })
  claimCanvas(canvas)

  const { videoWidth: width, videoHeight: height } = video
  const input = device.createTexture({
    size: [width, height, 1],
    format: 'rgba16float',
    usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
  })
  const pipelines = build(device, input)
  const layout = device.createBindGroupLayout({
    entries: [
      { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
      { binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: {} },
    ],
  })
  const shader = device.createShaderModule({ code: PRESENT })
  const pipeline = device.createRenderPipeline({
    layout: device.createPipelineLayout({ bindGroupLayouts: [layout] }),
    vertex: { module: shader, entryPoint: 'vert_main' },
    fragment: { module: shader, entryPoint: 'main', targets: [{ format }] },
    primitive: { topology: 'triangle-list' },
  })
  const group = device.createBindGroup({
    layout,
    entries: [
      { binding: 1, resource: device.createSampler({ magFilter: 'linear', minFilter: 'linear' }) },
      { binding: 2, resource: pipelines[pipelines.length - 1].getOutputTexture().createView() },
    ],
  })

  let stopped = false
  let handle = 0
  const frame = () => {
    if (stopped) return
    if (video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA) {
      device.queue.copyExternalImageToTexture({ source: video }, { texture: input }, [width, height])
    }
    const encoder = device.createCommandEncoder()
    for (const p of pipelines) p.pass(encoder)
    const pass = encoder.beginRenderPass({
      colorAttachments: [
        {
          view: context.getCurrentTexture().createView(),
          clearValue: { r: 0, g: 0, b: 0, a: 1 },
          loadOp: 'clear',
          storeOp: 'store',
        },
      ],
    })
    pass.setPipeline(pipeline)
    pass.setBindGroup(0, group)
    pass.draw(6)
    pass.end()
    device.queue.submit([encoder.finish()])
    handle = video.requestVideoFrameCallback(frame)
  }
  handle = video.requestVideoFrameCallback(frame)

  return {
    device,
    stop: () => {
      stopped = true
      video.cancelVideoFrameCallback(handle)
      device.destroy()
    },
  }
}

const SLOW_FRAME_MS = 36
const SLOW_WINDOW = 72

export type UpscaleState = 'off' | 'starting' | 'on' | 'unsupported' | 'failed'

export function useAnime4K({
  video,
  canvas,
  enabled,
  mode,
  tier,
  source,
  onSlow,
  smooth,
}: {
  video: HTMLVideoElement | null
  canvas: HTMLCanvasElement | null
  enabled: boolean
  mode: string
  tier: UpscaleTier
  source?: string
  onSlow?: () => void
  smooth?: { enabled: boolean; held: boolean; rate: SmoothRate; onSlow?: () => void }
}): { upscale: UpscaleState; smooth: SmoothState } {
  const slow = useEffectEvent(() => onSlow?.())
  const smoothSlow = useEffectEvent(() => smooth?.onSlow?.())
  const upscaleSteps = useEffectEvent(() => !!onSlow)
  const smoothing = !!smooth?.enabled
  const held = smoothing && !!smooth?.held
  const pace: SmoothRate = smoothing ? (smooth?.rate ?? 'auto') : 'auto'
  const startingPace = useEffectEvent(() => pace)
  const live = useRef<{ setRate: (rate: SmoothRate) => void } | null>(null)
  useEffect(() => live.current?.setRate(pace), [pace])
  const [filesLanded, setFilesLanded] = useState(0)
  const retry = smoothing ? filesLanded : 0

  const [generation, setGeneration] = useState(0)
  useEffect(() => {
    const restart = () => setGeneration((n) => n + 1)
    document.addEventListener('fullscreenchange', restart)
    return () => document.removeEventListener('fullscreenchange', restart)
  }, [])

  const run = [video, canvas, enabled, mode, tier, generation, source, smoothing, held, retry]
  type Ended = 'on' | 'failed'
  type SmoothEnded = Ended | 'missing'
  const [outcome, setOutcome] = useState<{ run: unknown[]; upscale: Ended; smooth: SmoothEnded }>()

  useEffect(() => {
    if ((!enabled && !smoothing) || !webgpuAvailable() || !video || !canvas) return

    let cancelled = false
    let stop: (() => void) | undefined
    const run = [video, canvas, enabled, mode, tier, generation, source, smoothing, held, retry]
    const settle = (upscale: Ended, smooth: SmoothEnded) => setOutcome({ run, upscale, smooth })
    let smoothEnded: SmoothEnded = 'on'

    const start = async () => {
      if (!video.videoWidth) {
        await new Promise<void>((resolve) => {
          video.addEventListener('loadedmetadata', () => resolve(), { once: true })
        })
      }
      if (cancelled) return

      const lib = enabled ? await import('anime4k-webgpu') : null
      if (cancelled) return

      let build: ((gpu: GPUDevice, inputTexture: GPUTexture, shared?: boolean) => Anime4KPipeline[]) | undefined
      if (lib) {
        const native = { width: video.videoWidth, height: video.videoHeight }
        const fit = Math.min(
          (window.screen.width * devicePixelRatio) / native.width,
          (window.screen.height * devicePixelRatio) / native.height,
        )
        const scale = Math.min(2, fit)
        const target = { width: Math.round(native.width * scale) & ~1, height: Math.round(native.height * scale) & ~1 }
        canvas.setAttribute('width', String(target.width))
        canvas.setAttribute('height', String(target.height))

        const key = mode?.toUpperCase() as Mode
        const picked: Mode = ['A', 'B', 'C', 'A+A', 'B+B', 'C+A'].includes(key) ? key : 'A'
        build = (gpu, inputTexture, shared) => {
          if (!shared) {
            void gpu.lost.then(() => {
              if (!cancelled) settle('failed', 'failed')
            })
          }
          const pipelines = chain(lib, gpu, inputTexture, native, target, picked, tier)
          if (!shared) timeFrames(gpu, pipelines[pipelines.length - 1], () => !cancelled && slow())
          return pipelines
        }
      }

      if (smoothing) {
        try {
          const shown = await presentSmooth({
            video,
            canvas,
            held,
            rate: startingPace(),
            build,
            onSlow: () => {
              if (cancelled) return
              if (build && upscaleSteps()) slow()
              else smoothSlow()
            },
            onLost: () => !cancelled && settle('failed', 'failed'),
          })
          if (cancelled) {
            shown.stop()
            return
          }
          stop = shown.stop
          live.current = shown
          shown.setRate(startingPace())
          settle('on', 'on')
          return
        } catch (err) {
          if (cancelled) return
          smoothEnded = err instanceof SmoothMissing ? 'missing' : 'failed'
          if (smoothEnded === 'failed') console.warn('smooth motion unavailable, playing without it:', err)
          if (!build) {
            settle('failed', smoothEnded)
            return
          }
        }
      }
      if (!build) return

      const shown = await present(video, canvas, build)
      stop = shown.stop
      if (cancelled) {
        shown.stop()
        return
      }
      settle('on', smoothEnded)
    }

    void start().catch((err) => {
      if (cancelled) return
      console.warn('anime4k unavailable, playing without it:', err)
      settle('failed', smoothEnded === 'on' ? 'failed' : smoothEnded)
    })

    return () => {
      cancelled = true
      stop?.()
      stop = undefined
      live.current = null
      setOutcome(undefined)
    }
  }, [video, canvas, enabled, mode, tier, generation, source, smoothing, held, retry])

  const missing = smoothing && outcome?.smooth === 'missing'
  useEffect(() => {
    if (!missing) return
    let timer = 0
    let stopped = false
    const check = (wait: number) => {
      timer = window.setTimeout(() => {
        void api
          .get<SmoothFiles>('/api/smooth')
          .then((s) => {
            if (stopped) return
            if (s.ready) setFilesLanded((n) => n + 1)
            else check(s.canInstall ? 4000 : 30_000)
          })
          .catch(() => !stopped && check(30_000))
      }, wait)
    }
    check(4000)
    return () => {
      stopped = true
      window.clearTimeout(timer)
    }
  }, [missing])

  const current = outcome && outcome.run.every((dep, i) => Object.is(dep, run[i])) ? outcome : undefined
  const state = <T extends SmoothEnded>(wanted: boolean, ended: T | undefined) => {
    if (!wanted) return 'off'
    if (!webgpuAvailable()) return 'unsupported'
    if (!video || !canvas) return 'off'
    return ended ?? 'starting'
  }
  return { upscale: state(enabled, current?.upscale), smooth: state(smoothing, current?.smooth) }
}

function timeFrames(device: GPUDevice, last: Anime4KPipeline, slow: () => void) {
  const pass = last.pass.bind(last)
  let times: number[] = []
  let fired = false
  last.pass = (encoder) => {
    pass(encoder)
    if (fired) return
    const began = performance.now()
    queueMicrotask(() => {
      void device.queue.onSubmittedWorkDone().then(() => {
        times.push(performance.now() - began)
        if (times.length < SLOW_WINDOW) return
        const median = times.sort((a, b) => a - b)[times.length >> 1]
        times = []
        if (median > SLOW_FRAME_MS) {
          fired = true
          slow()
        }
      }, () => {})
    })
  }
}
