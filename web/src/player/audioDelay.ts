const MAX_SECONDS = 0.5

const contexts = new WeakMap<HTMLMediaElement, AudioContext>()
const routes = new WeakMap<HTMLMediaElement, DelayNode>()
const routing = new WeakMap<HTMLMediaElement, Promise<DelayNode | null>>()
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

async function route(video: HTMLMediaElement): Promise<DelayNode | null> {
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
  const delay = context.createDelay(MAX_SECONDS)
  context.createMediaElementSource(video).connect(delay).connect(out)
  routes.set(video, delay)
  return delay
}

function routed(video: HTMLMediaElement): Promise<DelayNode | null> {
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

function apply(video: HTMLMediaElement, delay: DelayNode) {
  const seconds = wanted.get(video) ?? 0
  delay.delayTime.setTargetAtTime(seconds, delay.context.currentTime, 0.08)
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
  const delay = await routed(video)
  if (!delay) return false
  apply(video, delay)
  return true
}

export function releaseAudio(video: HTMLMediaElement) {
  const context = contexts.get(video)
  const wake = wakers.get(video)
  if (wake) video.removeEventListener('play', wake)
  contexts.delete(video)
  routes.delete(video)
  wanted.delete(video)
  wakers.delete(video)
  if (context && context.state !== 'closed') void context.close().catch(() => {})
}

export function audioDelayed(video: HTMLMediaElement): number {
  const delay = routes.get(video)
  return delay ? delay.delayTime.value * 1000 : 0
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
