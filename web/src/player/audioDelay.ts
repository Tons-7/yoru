const MAX_SECONDS = 0.5
const FADE_SECONDS = 0.06
const GLIDE_LIMIT = 0.05
const GLIDE_RATE = 0.002

interface Line {
  delay: DelayNode
  gain: GainNode
}
interface Route {
  lines: [Line, Line]
  active: 0 | 1
  fadeUntil: number
  pending: number
}

const contexts = new WeakMap<HTMLMediaElement, AudioContext>()
const routes = new WeakMap<HTMLMediaElement, Route>()
const routing = new WeakMap<HTMLMediaElement, Promise<Route | null>>()
const wanted = new WeakMap<HTMLMediaElement, number>()
const watchers = new WeakMap<HTMLMediaElement, Set<(ms: number) => void>>()
const blocked = new WeakSet<HTMLMediaElement>()
const wakers = new WeakMap<HTMLMediaElement, () => void>()

function waitForGesture(video: HTMLMediaElement) {
  if (blocked.has(video)) return
  blocked.add(video)
  const clear = () => {
    blocked.delete(video)
    window.removeEventListener('pointerdown', clear, true)
    window.removeEventListener('keydown', clear, true)
  }
  window.addEventListener('pointerdown', clear, true)
  window.addEventListener('keydown', clear, true)
}

function contextFor(video: HTMLMediaElement) {
  let context = contexts.get(video)
  if (!context) {
    const made = new AudioContext({ latencyHint: 'interactive' })
    const wake = () => {
      if (made.state !== 'running' && made.state !== 'closed' && routes.has(video)) void made.resume().catch(() => {})
    }
    made.addEventListener('statechange', wake)
    video.addEventListener('play', wake)
    wakers.set(video, wake)
    contexts.set(video, made)
    context = made
  }
  return context
}

async function route(video: HTMLMediaElement): Promise<Route | null> {
  const context = contextFor(video)
  if (context.state !== 'running') {
    await Promise.race([context.resume().catch(() => {}), new Promise((r) => setTimeout(r, 300))])
  }
  if (context.state !== 'running') {
    waitForGesture(video)
    return null
  }
  const out = context.destination
  if (out.maxChannelCount > out.channelCount) out.channelCount = out.maxChannelCount
  out.channelInterpretation = 'speakers'
  const source = context.createMediaElementSource(video)
  const line = (on: boolean): Line => {
    const delay = context.createDelay(MAX_SECONDS)
    const gain = context.createGain()
    gain.gain.value = on ? 1 : 0
    source.connect(delay).connect(gain).connect(out)
    return { delay, gain }
  }
  const made: Route = { lines: [line(true), line(false)], active: 0, fadeUntil: 0, pending: 0 }
  routes.set(video, made)
  return made
}

function routed(video: HTMLMediaElement): Promise<Route | null> {
  const known = routes.get(video)
  if (known) return Promise.resolve(known)
  if (blocked.has(video)) return Promise.resolve(null)
  let pending = routing.get(video)
  if (!pending) {
    pending = route(video).finally(() => routing.delete(video))
    routing.set(video, pending)
  }
  return pending
}

function apply(video: HTMLMediaElement, path: Route) {
  const seconds = wanted.get(video) ?? 0
  const outgoing = path.lines[path.active]
  const context = outgoing.delay.context
  const now = context.currentTime
  const running = context.state === 'running'
  if (running && now < path.fadeUntil) {
    if (!path.pending) {
      path.pending = window.setTimeout(() => {
        path.pending = 0
        if (routes.get(video) === path) apply(video, path)
      }, (path.fadeUntil - now) * 1000 + 10)
    }
    return
  }
  const held = outgoing.delay.delayTime
  const change = Math.abs(held.value - seconds)
  if (change < 0.001) {
    tell(video, seconds * 1000)
    return
  }
  if (running && change <= GLIDE_LIMIT) {
    for (const line of path.lines) {
      const param = line.delay.delayTime
      param.cancelScheduledValues(now)
      param.setValueAtTime(param.value, now)
      param.linearRampToValueAtTime(seconds, now + change / GLIDE_RATE)
    }
    tell(video, seconds * 1000)
    return
  }
  const next = path.active === 0 ? 1 : 0
  const incoming = path.lines[next]
  const end = now + FADE_SECONDS
  incoming.delay.delayTime.cancelScheduledValues(now)
  incoming.delay.delayTime.setValueAtTime(seconds, now)
  for (const [line, to] of [
    [outgoing, 0],
    [incoming, 1],
  ] as const) {
    line.gain.gain.cancelScheduledValues(now)
    line.gain.gain.setValueAtTime(line.gain.gain.value, now)
    line.gain.gain.linearRampToValueAtTime(to, end)
  }
  outgoing.delay.delayTime.cancelScheduledValues(now)
  outgoing.delay.delayTime.setValueAtTime(seconds, end + 0.01)
  path.active = next
  path.fadeUntil = end + 0.01
  tell(video, seconds * 1000)
}

export async function delayAudio(video: HTMLMediaElement, ms: number): Promise<boolean> {
  const seconds = Math.min(MAX_SECONDS, Math.max(0, ms / 1000))
  wanted.set(video, seconds)
  const known = routes.get(video)
  if (known) {
    apply(video, known)
    return true
  }
  if (seconds === 0) {
    tell(video, 0)
    return true
  }
  const path = await routed(video)
  if (!path) return false
  apply(video, path)
  return true
}

export function releaseAudio(video: HTMLMediaElement) {
  const context = contexts.get(video)
  const wake = wakers.get(video)
  if (wake) video.removeEventListener('play', wake)
  const path = routes.get(video)
  if (path?.pending) clearTimeout(path.pending)
  contexts.delete(video)
  routes.delete(video)
  wanted.delete(video)
  wakers.delete(video)
  if (context && context.state !== 'closed') void context.close().catch(() => {})
}

export function audioDelayed(video: HTMLMediaElement): number {
  const path = routes.get(video)
  return path ? path.lines[path.active].delay.delayTime.value * 1000 : 0
}

function tell(video: HTMLMediaElement, ms: number) {
  for (const watcher of watchers.get(video) ?? []) watcher(ms)
}

export function watchAudioDelay(video: HTMLMediaElement, watcher: (ms: number) => void): () => void {
  let set = watchers.get(video)
  if (!set) {
    set = new Set()
    watchers.set(video, set)
  }
  set.add(watcher)
  return () => set.delete(watcher)
}
