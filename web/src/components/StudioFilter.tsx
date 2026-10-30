import { useCallback, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useQuery } from '@tanstack/react-query'
import { api, query } from '../lib/api'
import { cx } from '../lib/format'
import { TextInput } from './ui'
import { useAnchorBox, useDebounced, useDismiss, useModalFocus } from '../lib/hooks'

interface Studio {
  id: number
  name: string
}

export function StudioFilter({
  value,
  onChange,
}: {
  value?: Studio
  onChange: (studio?: Studio) => void
}) {
  const [open, setOpen] = useState(false)
  const [term, setTerm] = useState('')
  const search = useDebounced(term.trim(), 250)
  const close = useCallback(() => setOpen(false), [])
  const ref = useDismiss<HTMLDivElement>(close, open)
  const trigger = useRef<HTMLButtonElement>(null)
  const box = useAnchorBox(trigger, open)
  const menuRef = useRef<HTMLDivElement>(null)
  useModalFocus(menuRef, open)

  const found = useQuery({
    enabled: open && search.length >= 2,
    queryKey: ['studios', search],
    queryFn: ({ signal }) =>
      api.get<{ studios: Studio[] }>(`/api/studios${query({ q: search })}`, signal),
    staleTime: 60 * 60_000,
  })

  return (
    <div className="relative" ref={ref}>
      <button
        ref={trigger}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={cx(
          'flex h-9 items-center gap-1.5 rounded-full px-3.5 text-sm font-medium transition-colors pointer-coarse:min-h-11',
          value
            ? 'bg-accent-500/15 text-accent-300 hover:bg-accent-500/25'
            : open
              ? 'bg-base-750 text-white'
              : 'bg-base-800 text-base-200 hover:bg-base-750 hover:text-white',
        )}
      >
        {value ? value.name : 'Studio'}
        <svg viewBox="0 0 24 24" className="size-3.5 opacity-60" aria-hidden>
          <path d="m6 9 6 6 6-6" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
        </svg>
      </button>
      {open &&
        box &&
        createPortal(
          <div
            ref={menuRef}
            data-portal-menu
            style={{
              top: Math.min(box.bottom + 6, window.innerHeight - 340),
              left: Math.max(8, Math.min(box.left, window.innerWidth - 264)),
            }}
            className="fixed z-50 w-64 animate-rise rounded-xl bg-base-750 p-2 shadow-panel"
          >
            <TextInput
              autoFocus
              value={term}
              onChange={(e) => setTerm(e.target.value)}
              placeholder="Search studios…"
              aria-label="Search studios"
              className="w-full"
            />
            <ul role="listbox" className="mt-1.5 max-h-64 overflow-y-auto scrollbar-thin">
              {search.length < 2 ? (
                <li className="px-2 py-1.5 text-xs text-base-500">Type a name, e.g. MAPPA</li>
              ) : found.isPending ? (
                <li className="px-2 py-1.5 text-xs text-base-500">Searching…</li>
              ) : found.isError ? (
                <li className="px-2 py-1.5 text-xs text-recap">Studio search is unavailable right now</li>
              ) : (found.data?.studios ?? []).length === 0 ? (
                <li className="px-2 py-1.5 text-xs text-base-500">No studio by that name</li>
              ) : (
                found.data!.studios.map((s) => (
                  <li key={s.id}>
                    <button
                      role="option"
                      aria-selected={false}
                      onClick={() => {
                        onChange(s)
                        setOpen(false)
                        setTerm('')
                      }}
                      className="w-full rounded-lg px-2.5 py-1.5 text-left text-sm text-base-200 hover:bg-base-700 pointer-coarse:min-h-11"
                    >
                      {s.name}
                    </button>
                  </li>
                ))
              )}
            </ul>
          </div>,
          document.body,
        )}
    </div>
  )
}
