import { useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, query } from '../lib/api'
import { cx } from '../lib/format'
import { useDebounced } from '../lib/hooks'
import { buttonClass } from '../lib/ui'
import { csv, FILTER_KEYS, useBrowse } from './browseQuery'
import { Sheet } from './Sheet'
import { Select, TextInput } from './ui'

export interface FilterVocabulary {
  genres: string[]
  formats: string[]
  statuses: string[]
  seasons: string[]
  sorts: string[]
  years: number[]
}

const FORMAT_LABELS: Record<string, string> = {
  TV: 'TV',
  TV_SHORT: 'TV short',
  OVA: 'OVA',
  ONA: 'ONA',
}

const STATUS_LABELS: Record<string, string> = {
  RELEASING: 'Airing',
  NOT_YET_RELEASED: 'Not yet aired',
  HIATUS: 'On hiatus',
}

function pretty(value: string, labels?: Record<string, string>) {
  return labels?.[value] ?? value.replace(/_/g, ' ').toLowerCase().replace(/^\w/, (c) => c.toUpperCase())
}

export function FilterSheet({
  params,
  vocab,
  onApply,
  onClose,
}: {
  params: Record<string, string>
  vocab?: FilterVocabulary
  onApply: (next: Record<string, string>) => void
  onClose: () => void
}) {
  const [draft, setDraft] = useState(() => Object.fromEntries(Object.entries(params).filter(([k]) => k !== 'page')))
  const asked = useDebounced(draft, 400)
  const { results, count, capped } = useBrowse(asked)
  const settled = JSON.stringify(asked) === JSON.stringify(draft) && !results.isPending
  const set = (key: string, value?: string) =>
    setDraft((d) => {
      const next = { ...d }
      if (value) next[key] = value
      else delete next[key]
      return next
    })
  const toggle = (key: string, value: string, many: boolean) => {
    const now = csv(draft[key])
    const next = many
      ? now.includes(value)
        ? now.filter((v) => v !== value)
        : [...now, value]
      : now[0] === value
        ? []
        : [value]
    set(key, next.join(','))
  }
  const clearAll = () => setDraft((d) => Object.fromEntries(Object.entries(d).filter(([k]) => !FILTER_KEYS.includes(k))))
  const filtering = FILTER_KEYS.some((k) => draft[k])
  const empty = settled && results.data?.items.length === 0

  const label = !settled
    ? 'Show results'
    : empty
      ? 'Nothing matches'
      : count !== undefined
        ? `Show ${count.toLocaleString()}${capped ? '+' : ''} anime`
        : results.data?.hasMore
          ? `Show ${results.data.items.length}+ anime`
          : 'Show results'

  const chips = (key: string, options: string[], many: boolean, labels?: Record<string, string>) => (
    <div className="flex flex-wrap gap-2">
      {options.map((option) => {
        const on = csv(draft[key]).includes(option)
        return (
          <button
            key={option}
            type="button"
            aria-pressed={on}
            onClick={() => toggle(key, option, many)}
            className={cx(
              'tap relative flex h-9 items-center gap-1.5 rounded-poster px-3 text-sm transition-colors',
              on ? 'bg-accent-500/20 font-medium text-accent-300' : 'bg-base-800 text-base-200 hover:bg-base-700',
            )}
          >
            {on && <CheckGlyph />}
            {pretty(option, labels)}
          </button>
        )
      })}
    </div>
  )

  return (
    <Sheet
      open
      onClose={onClose}
      title="Filters"
      tall
      side={false}
      footer={
        <>
          <button type="button" onClick={clearAll} disabled={!filtering} className={buttonClass('ghost', 'lg')}>
            Clear all
          </button>
          <button
            type="button"
            onClick={() => onApply(draft)}
            disabled={empty}
            className={cx(buttonClass('primary', 'lg'), 'min-w-0 flex-1 tabular-nums')}
          >
            {label}
          </button>
        </>
      }
    >
      <div className="space-y-6 pt-1">
        <Group title="Genre">{chips('genres', vocab?.genres ?? [], true)}</Group>
        <Group title="Format">{chips('formats', vocab?.formats ?? [], true, FORMAT_LABELS)}</Group>
        <Group title="Status">{chips('statuses', vocab?.statuses ?? [], true, STATUS_LABELS)}</Group>
        <Group title="Season">{chips('season', vocab?.seasons ?? [], false)}</Group>
        <Group title="Year">
          <Select value={draft.year ?? ''} onChange={(v) => set('year', v)} aria-label="Year" className="w-full">
            <option value="">Any year</option>
            {(vocab?.years ?? []).map((y) => (
              <option key={y} value={String(y)}>
                {y}
              </option>
            ))}
          </Select>
        </Group>
        <Group title="Studio">
          <StudioSearch
            value={draft.studio ? { id: Number(draft.studio), name: draft.studioName ?? 'Studio' } : undefined}
            onChange={(s) =>
              setDraft((d) => {
                const next = { ...d }
                delete next.studio
                delete next.studioName
                return s ? { ...next, studio: String(s.id), studioName: s.name } : next
              })
            }
          />
        </Group>
      </div>
    </Sheet>
  )
}

function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section>
      <h3 className="mb-2.5 text-sm font-semibold text-base-100">{title}</h3>
      {children}
    </section>
  )
}

interface Studio {
  id: number
  name: string
}

function StudioSearch({ value, onChange }: { value?: Studio; onChange: (studio?: Studio) => void }) {
  const [term, setTerm] = useState('')
  const search = useDebounced(term.trim(), 250)
  const found = useQuery({
    enabled: search.length >= 2,
    queryKey: ['studios', search],
    queryFn: ({ signal }) => api.get<{ studios: Studio[] }>(`/api/studios${query({ q: search })}`, signal),
    staleTime: 60 * 60_000,
  })

  return (
    <div className="space-y-2">
      {value && (
        <button
          type="button"
          onClick={() => onChange(undefined)}
          aria-label={`Remove Studio: ${value.name}`}
          className="tap relative flex h-9 items-center gap-1.5 rounded-poster bg-accent-500/20 pr-2 pl-3 text-sm font-medium text-accent-300 transition-colors hover:bg-accent-500/30"
        >
          {value.name}
          <CrossGlyph />
        </button>
      )}
      <TextInput
        value={term}
        onChange={(e) => setTerm(e.target.value)}
        placeholder="Search studios…"
        aria-label="Search studios"
        className="w-full"
      />
      {search.length >= 2 && (
        <ul className="space-y-1">
          {found.isPending ? (
            <li className="px-1 py-2 text-[13px] text-base-400">Searching…</li>
          ) : found.isError ? (
            <li className="flex items-center gap-3 px-1 py-2 text-[13px] text-red-300">
              Studio search didn't answer.
              <button type="button" onClick={() => found.refetch()} className={buttonClass('secondary', 'sm')}>
                Try again
              </button>
            </li>
          ) : (found.data?.studios ?? []).length === 0 ? (
            <li className="px-1 py-2 text-[13px] text-base-400">No studio by that name.</li>
          ) : (
            found.data!.studios.slice(0, 8).map((s) => (
              <li key={s.id}>
                <button
                  type="button"
                  onClick={() => {
                    onChange(s)
                    setTerm('')
                  }}
                  className="flex min-h-11 w-full items-center rounded-lg px-3 text-left text-sm text-base-100 transition-colors hover:bg-base-800"
                >
                  {s.name}
                </button>
              </li>
            ))
          )}
        </ul>
      )}
    </div>
  )
}

export function SortSheet({
  value,
  options,
  labels,
  searching,
  onChange,
  onClose,
}: {
  value?: string
  options: string[]
  labels: Record<string, string>
  searching: boolean
  onChange: (sort?: string) => void
  onClose: () => void
}) {
  const rows = [...(searching ? [{ value: '', label: 'Best match' }] : []), ...options.map((o) => ({ value: o, label: pretty(o, labels) }))]
  const current = value ?? (searching ? '' : 'popular')
  return (
    <Sheet open onClose={onClose} title="Sort" side={false}>
      <div role="radiogroup" aria-label="Sort" className="space-y-1">
        {rows.map((row) => {
          const on = row.value === current
          return (
            <button
              key={row.value || 'match'}
              type="button"
              role="radio"
              aria-checked={on}
              onClick={() => {
                onChange(row.value || undefined)
                onClose()
              }}
              className={cx(
                'flex min-h-12 w-full items-center justify-between gap-3 rounded-xl px-3 text-left text-[15px] transition-colors',
                on ? 'bg-accent-500/20 font-medium text-accent-300' : 'text-base-100 hover:bg-base-800',
              )}
            >
              {row.label}
              {on && <CheckGlyph />}
            </button>
          )
        })}
      </div>
    </Sheet>
  )
}

function CheckGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0" aria-hidden>
      <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function CrossGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-3.5" aria-hidden>
      <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" />
    </svg>
  )
}
