import { useCallback, useLayoutEffect, useRef } from 'react'

// Touch on the picture: hold for 2×, drag to scrub, pinch to fill, swipe down out of fullscreen.

export interface Gestures {
  /** Held still: 2× from on until off. */
  hold: (on: boolean) => void
  /** A sideways drag: seconds so far, then done on lift; a drag the browser took ends at 0. */
  scrub: (seconds: number, done: boolean) => void
  /** Two fingers spread (true) or pinched (false), once per touch. */
  pinch: (spread: boolean) => void
  /** In fullscreen, a long drag down (true) or up (false). */
  swipe: (down: boolean) => void
}

const HOLD_MS = 450
// Past this a finger is moving, not resting or tapping.
const SLOP = 12
const SWIPE = 70

// over: the touch did its gesture; the rest of it, to the last finger's lift, does nothing.
type Kind = 'none' | 'hold' | 'scrub' | 'pinch' | 'swipe' | 'over'

/** The picture's pointer handlers; used() says the touch that just ended was a gesture, not a tap. */
export function useTouchGestures(g: Gestures, opts: { fullscreen: boolean; secondsAcross: number }) {
  const points = useRef(new Map<number, { x0: number; y0: number; x: number; y: number }>())
  const kind = useRef<Kind>('none')
  const spent = useRef(false)
  const timer = useRef<number | undefined>(undefined)
  const spread = useRef(0)
  const scrubbed = useRef(0)
  // The handlers stay the same functions; what they call is the last render's.
  const latest = useRef({ g, opts })
  useLayoutEffect(() => {
    latest.current = { g, opts }
  })

  const distance = () => {
    const [a, b] = [...points.current.values()]
    return a && b ? Math.hypot(a.x - b.x, a.y - b.y) : 0
  }
  const end = useCallback((as: Kind) => {
    window.clearTimeout(timer.current)
    if (as === 'hold') latest.current.g.hold(false)
    kind.current = 'none'
  }, [])

  const onPointerDown = useCallback(
    (e: React.PointerEvent<HTMLElement>) => {
      if (e.pointerType !== 'touch') return
      // A new touch's first finger: any finger still listed lifted somewhere this never heard.
      if (e.isPrimary) points.current.clear()
      points.current.set(e.pointerId, { x0: e.clientX, y0: e.clientY, x: e.clientX, y: e.clientY })
      // Followed to the lift even off the picture.
      try {
        e.currentTarget.setPointerCapture(e.pointerId)
      } catch {
        // Already gone.
      }
      if (points.current.size === 1) {
        kind.current = 'none'
        spent.current = false
        timer.current = window.setTimeout(() => {
          if (kind.current !== 'none' || points.current.size !== 1) return
          kind.current = 'hold'
          spent.current = true
          latest.current.g.hold(true)
        }, HOLD_MS)
      } else if (points.current.size === 2) {
        end(kind.current)
        kind.current = 'pinch'
        spent.current = true
        spread.current = distance()
      }
    },
    [end],
  )

  const onPointerMove = useCallback((e: React.PointerEvent<HTMLElement>) => {
    const p = points.current.get(e.pointerId)
    if (!p) return
    p.x = e.clientX
    p.y = e.clientY
    const dx = p.x - p.x0
    const dy = p.y - p.y0
    const { g: on, opts: o } = latest.current
    if (kind.current === 'pinch') {
      if (points.current.size !== 2) return
      const ratio = spread.current > 0 ? distance() / spread.current : 1
      if (ratio > 1.25 || ratio < 0.8) {
        on.pinch(ratio > 1)
        kind.current = 'over'
      }
      return
    }
    if (kind.current === 'none' && points.current.size === 1) {
      if (Math.abs(dx) > SLOP && Math.abs(dx) > Math.abs(dy) * 1.5) {
        window.clearTimeout(timer.current)
        kind.current = 'scrub'
        spent.current = true
      } else if (o.fullscreen && Math.abs(dy) > SLOP && Math.abs(dy) > Math.abs(dx) * 1.5) {
        window.clearTimeout(timer.current)
        kind.current = 'swipe'
        spent.current = true
      } else if (Math.hypot(dx, dy) > SLOP) {
        // A scroll of the page, not a hold.
        window.clearTimeout(timer.current)
      }
    }
    if (kind.current === 'scrub') {
      const width = e.currentTarget.getBoundingClientRect().width || 1
      scrubbed.current = (dx / width) * o.secondsAcross
      on.scrub(scrubbed.current, false)
    }
  }, [])

  const finish = useCallback(
    (e: React.PointerEvent<HTMLElement>, lifted: boolean) => {
      const p = points.current.get(e.pointerId)
      if (!p) return
      points.current.delete(e.pointerId)
      const was = kind.current
      if (was === 'scrub') latest.current.g.scrub(lifted ? scrubbed.current : 0, true)
      if (was === 'swipe' && lifted && Math.abs(p.y - p.y0) > SWIPE) latest.current.g.swipe(p.y > p.y0)
      if (points.current.size === 0 || was === 'hold') end(was)
      // A pinch is two fingers: one lifted ends it.
      else if (was === 'pinch') kind.current = 'over'
    },
    [end],
  )
  const onPointerUp = useCallback((e: React.PointerEvent<HTMLElement>) => finish(e, true), [finish])
  // The browser took the touch (a scroll): nothing it started is carried out.
  const onPointerCancel = useCallback((e: React.PointerEvent<HTMLElement>) => finish(e, false), [finish])

  return {
    handlers: { onPointerDown, onPointerMove, onPointerUp, onPointerCancel },
    used: () => spent.current,
    touching: () => points.current.size > 0,
  }
}
