import { useEffect, useState } from 'react'
import { cx } from '../lib/format'
import { useMediaQuery } from '../lib/hooks'
import { useFloating } from '../lib/miniplayer'

const EDGE = 24
const SHOWN_AFTER_SCROLL = 2500

export function ScrollButtons() {
  const floating = useFloating()
  const [place, setPlace] = useState({ long: false, top: true, bottom: false })
  const touch = useMediaQuery('(pointer: coarse)')
  const [moving, setMoving] = useState(false)

  useEffect(() => {
    let frame = 0
    let rest = 0
    const check = () => {
      frame = 0
      const room = document.documentElement.scrollHeight - window.innerHeight
      const y = window.scrollY
      const next = { long: room > window.innerHeight * 0.5, top: y < EDGE, bottom: y > room - EDGE }
      setPlace((was) => (was.long === next.long && was.top === next.top && was.bottom === next.bottom ? was : next))
    }
    const soon = () => {
      if (!frame) frame = requestAnimationFrame(check)
    }
    const scrolled = () => {
      soon()
      setMoving(true)
      window.clearTimeout(rest)
      rest = window.setTimeout(() => setMoving(false), SHOWN_AFTER_SCROLL)
    }
    check()
    const grows = new ResizeObserver(soon)
    grows.observe(document.body)
    window.addEventListener('scroll', scrolled, { passive: true })
    window.addEventListener('resize', soon)
    return () => {
      grows.disconnect()
      window.removeEventListener('scroll', scrolled)
      window.removeEventListener('resize', soon)
      cancelAnimationFrame(frame)
      window.clearTimeout(rest)
    }
  }, [])

  if (!place.long || floating) return null
  const shown = !touch || moving
  const go = (top: number) => {
    const still = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    window.scrollTo({ top, behavior: still ? 'auto' : 'smooth' })
  }
  const button = 'tap relative grid size-10 place-items-center text-base-200 transition-[color,opacity] hover:text-white disabled:opacity-35'
  return (
    <div
      role="group"
      aria-label="Scroll"
      aria-hidden={!shown || undefined}
      inert={!shown}
      className={cx(
        'fixed right-4 bottom-4 z-40 flex flex-col overflow-hidden rounded-full bg-base-800/85 shadow-panel ring-1 ring-line backdrop-blur-md transition-opacity duration-300 max-sm:bottom-[calc(4.75rem+env(safe-area-inset-bottom))]',
        shown ? 'opacity-100' : 'pointer-events-none opacity-0',
      )}
    >
      <button type="button" aria-label="Scroll to top" title="Top" disabled={place.top} onClick={() => go(0)} className={button}>
        <Chevron up />
      </button>
      <span aria-hidden className="mx-2.5 h-px bg-line" />
      <button
        type="button"
        aria-label="Scroll to bottom"
        title="Bottom"
        disabled={place.bottom}
        onClick={() => go(document.documentElement.scrollHeight)}
        className={button}
      >
        <Chevron />
      </button>
    </div>
  )
}

function Chevron({ up = false }: { up?: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className="size-4.5" aria-hidden>
      <path
        d={up ? 'm6 15 6-6 6 6' : 'm6 9 6 6 6-6'}
        fill="none"
        stroke="currentColor"
        strokeWidth="2.2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}
