import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useDismiss, useModalFocus } from '../lib/hooks'
import { renderAvatar } from '../lib/profile'
import { Button } from './ui'

const MAX_ZOOM = 4
const CIRCLE_SHARE = 0.84

export function AvatarEditor({ file, onCancel, onSave }: { file: File; onCancel: () => void; onSave: (image: string) => void }) {
  const panel = useDismiss<HTMLDivElement>(onCancel)
  useModalFocus(panel)
  const stage = useRef<HTMLDivElement>(null)
  const [size, setSize] = useState({ w: 0, h: 0 })
  const [picture, setPicture] = useState<HTMLImageElement | null>(null)
  const [problem, setProblem] = useState('')
  const [zoom, setZoom] = useState(1)
  const [at, setAt] = useState({ x: 0, y: 0 })
  const drag = useRef<{ id: number; x: number; y: number } | null>(null)

  useEffect(() => {
    const url = URL.createObjectURL(file)
    const img = new Image()
    let gone = false
    img.onload = () => {
      if (gone) return
      if (img.naturalWidth && img.naturalHeight) setPicture(img)
      else setProblem("That picture couldn't be read.")
    }
    img.onerror = () => !gone && setProblem("That picture couldn't be read.")
    img.src = url
    return () => {
      gone = true
      URL.revokeObjectURL(url)
    }
  }, [file])

  useLayoutEffect(() => {
    const el = stage.current
    if (!el) return
    const measure = () => setSize({ w: el.clientWidth, h: el.clientHeight })
    measure()
    const watching = new ResizeObserver(measure)
    watching.observe(el)
    const still = (e: WheelEvent) => e.preventDefault()
    el.addEventListener('wheel', still, { passive: false })
    return () => {
      watching.disconnect()
      el.removeEventListener('wheel', still)
    }
  }, [])

  const circle = Math.round(Math.min(size.w, size.h) * CIRCLE_SHARE)
  const fit = picture && circle ? circle / Math.min(picture.naturalWidth, picture.naturalHeight) : 1
  const scale = fit * zoom
  const width = (picture?.naturalWidth ?? 0) * scale
  const height = (picture?.naturalHeight ?? 0) * scale
  const held = (p: { x: number; y: number }, w = width, h = height) => ({
    x: Math.min((w - circle) / 2, Math.max(-(w - circle) / 2, p.x)),
    y: Math.min((h - circle) / 2, Math.max(-(h - circle) / 2, p.y)),
  })
  const zoomTo = (next: number) => {
    const z = Math.min(MAX_ZOOM, Math.max(1, next))
    if (!picture) return
    setZoom(z)
    setAt((was) => held({ x: (was.x * z) / zoom, y: (was.y * z) / zoom }, picture.naturalWidth * fit * z, picture.naturalHeight * fit * z))
  }

  const pos = held(at)
  const save = () => {
    if (!picture || !circle) return
    const side = Math.min(circle / scale, picture.naturalWidth, picture.naturalHeight)
    const clampTo = (v: number, most: number) => Math.min(Math.max(0, v), Math.max(0, most))
    try {
      onSave(
        renderAvatar(picture, {
          side,
          x: clampTo(picture.naturalWidth / 2 - pos.x / scale - side / 2, picture.naturalWidth - side),
          y: clampTo(picture.naturalHeight / 2 - pos.y / scale - side / 2, picture.naturalHeight - side),
        }),
      )
    } catch (err) {
      setProblem(err instanceof Error ? err.message : "That picture couldn't be used.")
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label="Edit picture"
        className="animate-fade-in relative w-full max-w-xl rounded-xl border border-base-800 bg-base-900 p-4 shadow-2xl"
      >
        <h2 className="text-base font-semibold text-white">Edit picture</h2>
        <p className="mt-0.5 text-xs text-base-500">Drag or use the arrow keys to move it. The slider zooms.</p>

        <div
          ref={stage}
          tabIndex={picture ? 0 : -1}
          role="group"
          aria-label="Picture position: arrow keys move it"
          onKeyDown={(e) => {
            const step = e.shiftKey ? 40 : 10
            const by = { ArrowLeft: [step, 0], ArrowRight: [-step, 0], ArrowUp: [0, step], ArrowDown: [0, -step] }[e.key]
            if (!picture || !by) return
            e.preventDefault()
            setAt(held({ x: pos.x + by[0], y: pos.y + by[1] }))
          }}
          className="relative mt-4 aspect-4/3 max-h-[60vh] w-full touch-none overflow-hidden rounded-lg bg-base-950 select-none focus-visible:ring-2 focus-visible:ring-accent-400 focus-visible:outline-hidden"
          style={{ cursor: picture ? 'grab' : 'default' }}
          onPointerDown={(e) => {
            if (!picture) return
            e.currentTarget.setPointerCapture(e.pointerId)
            drag.current = { id: e.pointerId, x: e.clientX - pos.x, y: e.clientY - pos.y }
          }}
          onPointerMove={(e) => {
            const d = drag.current
            if (d?.id === e.pointerId) setAt(held({ x: e.clientX - d.x, y: e.clientY - d.y }))
          }}
          onPointerUp={() => (drag.current = null)}
          onPointerCancel={() => (drag.current = null)}
          onWheel={(e) => zoomTo(zoom * (e.deltaY < 0 ? 1.1 : 1 / 1.1))}
        >
          {picture && circle > 0 && (
            <img
              src={picture.src}
              alt=""
              draggable={false}
              className="pointer-events-none absolute top-1/2 left-1/2 max-w-none"
              style={{ width, height, transform: `translate(calc(-50% + ${pos.x}px), calc(-50% + ${pos.y}px))` }}
            />
          )}
          <div
            aria-hidden
            data-crop
            className="pointer-events-none absolute top-1/2 left-1/2 -translate-1/2 rounded-full shadow-[0_0_0_9999px_rgba(8,9,13,0.62)] ring-2 ring-white/85"
            style={{ width: circle, height: circle }}
          />
        </div>

        <label className="mt-4 flex items-center gap-3 text-xs text-base-400">
          Zoom
          <input
            type="range"
            min={1}
            max={MAX_ZOOM}
            step={0.01}
            value={zoom}
            disabled={!picture}
            onChange={(e) => zoomTo(Number(e.target.value))}
            aria-label="Zoom"
            className="min-w-0 flex-1 accent-accent-500"
          />
        </label>

        {problem && <p className="mt-3 text-xs text-red-300">{problem}</p>}

        <div className="mt-4 flex justify-end gap-2">
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="primary" onClick={save} disabled={!picture}>
            Save
          </Button>
        </div>
      </div>
    </div>
  )
}
