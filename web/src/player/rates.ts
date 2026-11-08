import { useEffect, useState } from 'react'

const COMMON_RATES = [48, 50, 60, 72, 75, 90, 100, 120, 144, 165, 180, 200, 240, 280, 360, 480]

export function nearestRate(hz: number): number {
  const near = COMMON_RATES.reduce((a, b) => (Math.abs(b - hz) < Math.abs(a - hz) ? b : a))
  return Math.abs(near - hz) / hz < 0.04 ? near : Math.round(hz)
}

export function useScreenHz(active: boolean): number {
  const [hz, setHz] = useState(0)
  useEffect(() => {
    if (!active) return
    let raf = 0
    let last = 0
    const gaps: number[] = []
    const tick = (now: number) => {
      if (last) gaps.push(now - last)
      last = now
      if (gaps.length < 90) {
        raf = requestAnimationFrame(tick)
        return
      }
      const median = gaps.sort((a, b) => a - b)[gaps.length >> 1]
      setHz(nearestRate(1000 / median))
    }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [active])
  return hz
}

export function formatFps(fps: number): string {
  return Number.isInteger(fps) ? String(fps) : fps.toFixed(3).replace(/\.?0+$/, '')
}
