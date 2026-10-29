import { useLayoutEffect, useRef, useState, useSyncExternalStore, type MouseEvent, type PointerEvent as ReactPointerEvent } from 'react'

// The browser player's video: for the docked player's Play, and whether leaving the watch page docks it.

let video: HTMLVideoElement | null = null
const listeners = new Set<() => void>()
const emit = () => listeners.forEach((l) => l())

export function attachVideo(v: HTMLVideoElement | null) {
  if (video === v) return
  for (const type of ['play', 'pause', 'ended']) {
    video?.removeEventListener(type, emit)
    v?.addEventListener(type, emit)
  }
  video = v
  emit()
}

export function detachVideo(v: HTMLVideoElement | null) {
  if (video === v) attachVideo(null)
}

export const playingNow = () => !!video && !video.paused && !video.ended

export const currentVideo = () => video

export function useCurrentVideo(): HTMLVideoElement | null {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    currentVideo,
  )
}

export function usePlayingNow(): boolean {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => listeners.delete(l)
    },
    playingNow,
  )
}

export function togglePlay() {
  if (!video) return
  if (video.paused) void video.play().catch(() => {})
  else video.pause()
}

export type Corner = 'tl' | 'tr' | 'bl' | 'br'
const CORNER_KEY = 'yoru.mini.corner'

function savedCorner(): Corner {
  try {
    const c = localStorage.getItem(CORNER_KEY)
    return c === 'tl' || c === 'tr' || c === 'bl' ? c : 'br'
  } catch {
    return 'br'
  }
}

/** The docked player held and moved; let go, it glides to the nearest corner, kept per device. */
export function useDockDrag(on: boolean) {
  const [corner, setCorner] = useState<Corner>(savedCorner)
  const [drops, setDrops] = useState(0)
  const ref = useRef<HTMLDivElement>(null)
  const from = useRef<DOMRect | null>(null)
  const moved = useRef(false)

  useLayoutEffect(() => {
    const el = ref.current
    const was = from.current
    from.current = null
    if (!el) return
    if (!on || !was) {
      el.style.transform = el.style.transition = el.style.willChange = ''
      return
    }
    // Measured without the drag's offset, then started from where it was let go.
    el.style.transition = 'none'
    el.style.transform = ''
    const now = el.getBoundingClientRect()
    el.style.transform = `translate(${was.left - now.left}px, ${was.top - now.top}px)`
    void el.offsetWidth
    el.style.transition = 'transform 280ms cubic-bezier(0.2, 0.8, 0.2, 1)'
    el.style.transform = ''
    const done = () => (el.style.willChange = el.style.transition = '')
    el.addEventListener('transitionend', done, { once: true })
    return () => el.removeEventListener('transitionend', done)
  }, [on, corner, drops])

  const onPointerDownCapture = (e: ReactPointerEvent<HTMLDivElement>) => {
    const el = ref.current
    if (!on || !el || e.button !== 0) return
    const sx = e.clientX
    const sy = e.clientY
    // Caught mid-glide, it carries on from where it is.
    const now = getComputedStyle(el).transform
    const at = new DOMMatrix(now === 'none' ? undefined : now)
    moved.current = false
    const move = (ev: PointerEvent) => {
      const dx = ev.clientX - sx
      const dy = ev.clientY - sy
      if (!moved.current && Math.hypot(dx, dy) < 8) return
      if (!moved.current) {
        el.style.transition = 'none'
        el.style.willChange = 'transform'
      }
      moved.current = true
      el.style.transform = `translate3d(${at.m41 + dx}px, ${at.m42 + dy}px, 0)`
    }
    const up = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      window.removeEventListener('pointercancel', up)
      if (!moved.current) return
      const r = el.getBoundingClientRect()
      const next: Corner = `${r.top + r.height / 2 < innerHeight / 2 ? 't' : 'b'}${r.left + r.width / 2 < innerWidth / 2 ? 'l' : 'r'}`
      from.current = r
      setCorner(next)
      setDrops((n) => n + 1)
      try {
        localStorage.setItem(CORNER_KEY, next)
      } catch {
        // This visit only.
      }
    }
    window.addEventListener('pointermove', move)
    window.addEventListener('pointerup', up)
    window.addEventListener('pointercancel', up)
  }

  // A drag isn't a press: the click that ends one goes nowhere.
  const onClickCapture = (e: MouseEvent) => {
    if (!moved.current) return
    moved.current = false
    e.preventDefault()
    e.stopPropagation()
  }

  return { corner, ref, onPointerDownCapture, onClickCapture }
}
