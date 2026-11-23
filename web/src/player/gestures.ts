import { useCallback, useEffect, useLayoutEffect, useRef } from 'react'

export interface Gestures {
  hold: (on: boolean) => void
  scrub: (seconds: number, done: boolean) => void
  pinch: (spread: boolean) => void
  swipe: (down: boolean) => void
}

const HOLD_MS = 450
const SLOP = 12
const SWIPE = 70

type Kind = 'none' | 'hold' | 'scrub' | 'pinch' | 'swipe' | 'over'

export function useTouchGestures(g: Gestures, opts: { swipes: boolean; secondsAcross: number }) {
  const points = useRef(new Map<number, { x0: number; y0: number; x: number; y: number }>())
  const kind = useRef<Kind>('none')
  const spent = useRef(false)
  const timer = useRef<number | undefined>(undefined)
  const spread = useRef(0)
  const scrubbed = useRef(0)
  const latest = useRef({ g, opts })
  useLayoutEffect(() => {
    latest.current = { g, opts }
  })
  useEffect(() => () => window.clearTimeout(timer.current), [])

  const distance = () => {
    const [a, b] = [...points.current.values()]
    return a && b ? Math.hypot(a.x - b.x, a.y - b.y) : 0
  }
  const end = useCallback((as: Kind) => {
    window.clearTimeout(timer.current)
    if (as === 'hold') latest.current.g.hold(false)
    if (as === 'scrub') latest.current.g.scrub(0, true)
    kind.current = 'none'
  }, [])

  const onPointerDown = useCallback(
    (e: React.PointerEvent<HTMLElement>) => {
      if (e.pointerType !== 'touch') return
      if (e.isPrimary) points.current.clear()
      points.current.set(e.pointerId, { x0: e.clientX, y0: e.clientY, x: e.clientX, y: e.clientY })
      try {
        e.currentTarget.setPointerCapture(e.pointerId)
      } catch {
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
      } else if (o.swipes && Math.abs(dy) > SLOP && Math.abs(dy) > Math.abs(dx) * 1.5) {
        window.clearTimeout(timer.current)
        kind.current = 'swipe'
        spent.current = true
      } else if (Math.hypot(dx, dy) > SLOP) {
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
      else if (was === 'pinch') kind.current = 'over'
    },
    [end],
  )
  const onPointerUp = useCallback((e: React.PointerEvent<HTMLElement>) => finish(e, true), [finish])
  const onPointerCancel = useCallback((e: React.PointerEvent<HTMLElement>) => finish(e, false), [finish])

  return {
    handlers: { onPointerDown, onPointerMove, onPointerUp, onPointerCancel },
    used: () => spent.current,
    touching: () => points.current.size > 0,
  }
}
