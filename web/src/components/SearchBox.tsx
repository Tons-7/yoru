import { Fragment, useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api, query, statusLabel, type DiscoverItem, type LibraryItem } from '../lib/api'
import { cx, tint } from '../lib/format'
import { useDebounced, useDismiss } from '../lib/hooks'
import { rememberSearch } from '../lib/recentSearches'
import { fromLocal, libraryFirst, rowFacts, type SearchRow } from '../lib/searchRows'
import { openableLink, openLinkPath, openLinkWords } from '../lib/showLink'
import { plainKey } from './keys'
import { Badge } from './ui'

const LIBRARY_ROWS = 3
const ALL_ROWS = 5

export function SearchBox() {
  const navigate = useNavigate()
  const [value, setValue] = useState('')

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (plainKey(e) !== '/') return
      e.preventDefault()
      const field = document.getElementById('yoru-search')
      if (field?.offsetParent) field.focus()
      else navigate('/browse')
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate])

  const q = useDebounced(value.trim(), 350)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const close = useCallback(() => setOpen(false), [])
  const box = useDismiss<HTMLFormElement>(close, open)
  const link = openableLink(value)
  const searching = q.length >= 2 && !link

  const local = useQuery({
    enabled: searching,
    queryKey: ['search', 'library', q],
    queryFn: ({ signal }) => api.get<{ results: LibraryItem[] }>(`/api/search/local${query({ q, limit: 8 })}`, signal),
    staleTime: 30_000,
    placeholderData: keepPreviousData,
  })
  const all = useQuery({
    enabled: searching,
    queryKey: ['search', 'all', q],
    queryFn: ({ signal }) => api.get<{ items: DiscoverItem[] }>(`/api/browse${query({ q, perPage: 8 })}`, signal),
    staleTime: 60_000,
    retry: 1,
    placeholderData: keepPreviousData,
  })

  const library: SearchRow[] = []
  const rest: SearchRow[] = []
  if (searching) {
    const shaped = libraryFirst(all.data?.items ?? [], local.data?.results, 1)
    library.push(...shaped.mine.slice(0, LIBRARY_ROWS))
    const shown = new Set(library.map((r) => r.id))
    const pool = all.isError ? (local.data?.results ?? []).map(fromLocal) : [...shaped.mine.slice(LIBRARY_ROWS), ...shaped.rest]
    for (const row of pool) {
      if (rest.length >= ALL_ROWS) break
      if (!shown.has(row.id)) {
        rest.push(row)
        shown.add(row.id)
      }
    }
  }
  const hits = [...library, ...rest]
  const last = hits.length
  const showMenu = open && (searching || (!!link && value.trim().length >= 2))
  const waiting = searching && all.isPending

  const browse = () => {
    if (!value.trim()) return
    setOpen(false)
    if (link) {
      setValue('')
      navigate(openLinkPath(value))
      return
    }
    rememberSearch(value)
    navigate(`/browse?q=${encodeURIComponent(value.trim())}`)
  }
  const pick = (id: number) => {
    rememberSearch(value)
    setOpen(false)
    setValue('')
    navigate(`/anime/${id}`)
  }

  const option = (row: SearchRow, i: number) => {
    const facts = rowFacts(row)
    return (
      <li key={row.id} id={`yoru-search-opt-${i}`} role="option" aria-selected={i === active}>
        <button
          type="button"
          tabIndex={-1}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => pick(row.id)}
          className={cx(
            'flex w-full items-center gap-3 rounded-lg px-2 py-1.5 text-left transition-colors',
            i === active ? 'bg-base-800' : 'hover:bg-base-800',
          )}
        >
          <span
            className="h-12 w-8 shrink-0 overflow-hidden rounded bg-base-800"
            style={row.color ? { backgroundColor: tint(row.color, 0.3) } : undefined}
          >
            {row.thumb && <img src={row.thumb} alt="" loading="lazy" decoding="async" className="size-full object-cover" />}
          </span>
          <span className="min-w-0 flex-1">
            <span className="line-clamp-2 text-sm leading-snug font-medium text-base-100">{row.title}</span>
            {facts.length > 0 && (
              <span className="mt-0.5 block truncate text-xs text-base-400 tabular-nums">
                {facts.map((fact, n) => (
                  <Fragment key={n}>
                    {n > 0 && ' · '}
                    <span className={fact.airing ? 'text-sky-300' : undefined}>{fact.text}</span>
                  </Fragment>
                ))}
              </span>
            )}
          </span>
          {row.listStatus && <Badge tone="accent">{statusLabel(row.listStatus)}</Badge>}
        </button>
      </li>
    )
  }

  const group = (label: string) => (
    <li role="presentation" className="px-2 pt-2 pb-1 text-xs font-semibold text-base-400">
      {label}
    </li>
  )

  return (
    <form
      ref={box}
      role="search"
      className="relative ml-auto flex min-w-0 flex-1 justify-end max-lg:hidden sm:max-w-xs"
      onSubmit={(e) => {
        e.preventDefault()
        if (active >= 0 && hits[active]) pick(hits[active].id)
        else browse()
      }}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false)
      }}
    >
      <div className="relative w-full">
        <svg
          viewBox="0 0 24 24"
          className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-base-500"
          aria-hidden
        >
          <circle cx="11" cy="11" r="6.5" fill="none" stroke="currentColor" strokeWidth="1.8" />
          <path d="m16 16 4.5 4.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
        </svg>
        <input
          id="yoru-search"
          value={value}
          onChange={(e) => {
            setValue(e.target.value)
            setOpen(true)
            setActive(-1)
          }}
          onFocus={() => setOpen(true)}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setOpen(true)
              setActive((a) => Math.min(last, a + 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setActive((a) => Math.max(-1, a - 1))
            } else if (e.key === 'Escape') {
              setOpen(false)
            }
          }}
          role="combobox"
          aria-expanded={showMenu}
          aria-controls="yoru-search-results"
          aria-autocomplete="list"
          aria-activedescendant={showMenu && active >= 0 ? `yoru-search-opt-${active}` : undefined}
          autoComplete="off"
          placeholder="Search anime…"
          aria-label="Search anime"
          className={cx(
            'h-9 w-full rounded-xl bg-base-800 pl-8 text-sm text-base-100 placeholder:text-base-500 focus:outline-hidden focus-visible:ring-2 focus-visible:ring-accent-400 pointer-coarse:h-11',
            value ? 'pr-8' : 'pr-3',
          )}
        />
        {!value && (
          <kbd className="pointer-events-none absolute top-1/2 right-2 -translate-y-1/2 rounded border border-base-750 bg-base-850 px-1.5 font-mono text-xs leading-4 text-base-500">
            /
          </kbd>
        )}
        {value && (
          <button
            type="button"
            aria-label="Clear search"
            onClick={() => {
              setValue('')
              setActive(-1)
              document.getElementById('yoru-search')?.focus()
            }}
            className="tap absolute top-1/2 right-1 grid size-6 -translate-y-1/2 place-items-center rounded text-base-500 transition-colors hover:text-base-100"
          >
            <svg viewBox="0 0 20 20" aria-hidden className="size-3.5">
              <path d="M5 5l10 10M15 5L5 15" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
            </svg>
          </button>
        )}
      </div>

      {showMenu && (
        <ul
          id="yoru-search-results"
          role="listbox"
          aria-label="Search results"
          className="absolute top-full left-0 z-50 mt-1.5 max-h-[calc(100dvh-5rem)] w-96 max-w-[calc(100vw-2rem)] animate-rise overflow-y-auto rounded-sheet bg-base-750 p-1.5 shadow-panel scrollbar-thin"
        >
          {library.length > 0 && group('In your library')}
          {library.map((hit, i) => option(hit, i))}
          {(rest.length > 0 || waiting || all.isError) && searching && group('All anime')}
          {rest.map((hit, i) => option(hit, library.length + i))}
          {waiting && rest.length === 0 && (
            <li role="presentation" className="px-2 py-2 text-xs text-base-400">
              Searching all anime…
            </li>
          )}
          {all.isError && (
            <li role="presentation" className="flex items-center justify-between gap-2 px-2 py-1.5 text-xs text-base-400">
              <span>Couldn't search AniList. Showing what Yoru has saved.</span>
              <button
                type="button"
                tabIndex={-1}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => void all.refetch()}
                className="tap relative shrink-0 rounded px-1.5 py-0.5 font-medium text-accent-300 hover:bg-base-800"
              >
                Retry
              </button>
            </li>
          )}
          {searching && hits.length === 0 && !waiting && !all.isError && all.isSuccess && (
            <li role="presentation" className="px-2 py-2 text-xs text-base-400">
              Nothing matches “{q}”.
            </li>
          )}
          {searching && <li role="presentation" className="mx-1.5 my-1 h-px bg-white/[0.07]" />}
          <li id={`yoru-search-opt-${last}`} role="option" aria-selected={active === last}>
            <button
              type="button"
              tabIndex={-1}
              onMouseDown={(e) => e.preventDefault()}
              onClick={browse}
              className={cx(
                'flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm font-medium text-accent-300 transition-colors',
                active === last ? 'bg-base-800' : 'hover:bg-base-800',
              )}
            >
              <span className="min-w-0 flex-1 truncate">
                {link ? openLinkWords(link) : `See all results for “${value.trim()}”`}
              </span>
              <span aria-hidden className="text-base-500">
                ›
              </span>
            </button>
          </li>
        </ul>
      )}
    </form>
  )
}
