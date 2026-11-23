import { useState, type ReactNode } from 'react'
import { statusLabel } from '../lib/api'
import { cx } from '../lib/format'
import { countFor, moreFiltersOn, sortLabel, SORTS, STATUS_ORDER, type Counts, type Layout, type MoreFilters } from '../lib/library'
import { useFilters } from '../lib/queries'
import { BottomSheet } from './BottomSheet'
import { FilterToggle, SearchInput, Segmented, Select, Switch } from './ui'

export interface ControlProps {
  status: string
  fav: boolean
  sort: string
  layout: Layout
  counts?: Counts
  text: string
  onText: (text: string) => void
  onStatus: (status: string) => void
  onFav: (on: boolean) => void
  onSort: (sort: string) => void
  onLayout: (layout: Layout) => void
  more: MoreFilters
  onMore: (key: keyof MoreFilters, value: string | boolean) => void
  onClearMore: () => void
}

const FORMATS: Array<{ value: string; label: string }> = [
  { value: 'TV', label: 'TV' },
  { value: 'MOVIE', label: 'Films' },
  { value: 'OVA', label: 'OVA' },
  { value: 'ONA', label: 'ONA' },
  { value: 'SPECIAL', label: 'Specials' },
]
const THIS_YEAR = new Date().getFullYear()
const FALLBACK_YEARS = Array.from({ length: THIS_YEAR - 1979 }, (_, i) => THIS_YEAR + 1 - i)

const SEASONS: Array<{ value: string; label: string }> = [
  { value: 'WINTER', label: 'Winter' },
  { value: 'SPRING', label: 'Spring' },
  { value: 'SUMMER', label: 'Summer' },
  { value: 'FALL', label: 'Fall' },
]

function MoreFilterFields({ more, onMore, onClearMore, stacked = false }: Pick<ControlProps, 'more' | 'onMore' | 'onClearMore'> & { stacked?: boolean }) {
  const vocab = useFilters().data as { genres?: string[]; years?: number[] } | undefined
  const years = vocab?.years?.length ? vocab.years : FALLBACK_YEARS
  const field = stacked ? 'w-full' : ''
  return (
    <div className={cx('flex flex-wrap items-center gap-2', stacked && 'flex-col items-stretch')}>
      <Select value={more.format} onChange={(v) => onMore('format', v)} aria-label="Format" className={field}>
        <option value="">Any format</option>
        {FORMATS.map((f) => (
          <option key={f.value} value={f.value}>
            {f.label}
          </option>
        ))}
      </Select>
      <Select value={more.season} onChange={(v) => onMore('season', v)} aria-label="Season" className={field}>
        <option value="">Any season</option>
        {SEASONS.map((s) => (
          <option key={s.value} value={s.value}>
            {s.label}
          </option>
        ))}
      </Select>
      <Select value={more.year} onChange={(v) => onMore('year', v)} aria-label="Year" className={field}>
        <option value="">Any year</option>
        {years.map((y) => (
          <option key={y} value={String(y)}>
            {y}
          </option>
        ))}
      </Select>
      <Select value={more.genre} onChange={(v) => onMore('genre', v)} aria-label="Genre" className={field}>
        <option value="">Any genre</option>
        {(vocab?.genres ?? []).map((g) => (
          <option key={g} value={g}>
            {g}
          </option>
        ))}
      </Select>
      {stacked ? (
        <>
          <label className="flex min-h-[52px] cursor-pointer items-center justify-between gap-3 px-1 text-[15px] text-base-100">
            Airing now
            <Switch on={more.airing} onChange={(v) => onMore('airing', v)} aria-label="Airing now" />
          </label>
          <label className="flex min-h-[52px] cursor-pointer items-center justify-between gap-3 px-1 text-[15px] text-base-100">
            Downloaded
            <Switch on={more.disk} onChange={(v) => onMore('disk', v)} aria-label="Downloaded" />
          </label>
        </>
      ) : (
        <>
          <FilterToggle on={more.airing} onChange={(v) => onMore('airing', v)}>
            Airing now
          </FilterToggle>
          <FilterToggle on={more.disk} onChange={(v) => onMore('disk', v)}>
            Downloaded
          </FilterToggle>
        </>
      )}
      {moreFiltersOn(more) && (
        <button
          type="button"
          onClick={onClearMore}
          className="tap relative min-h-11 rounded-lg px-2 text-sm font-medium text-accent-300 hover:text-accent-200 sm:min-h-0"
        >
          Clear filters
        </button>
      )}
    </div>
  )
}

const STATUS_CHOICES = ['', ...STATUS_ORDER]

function choiceLabel(status: string) {
  return status ? statusLabel(status) : 'All'
}

export function DesktopControls(props: ControlProps) {
  const { status, fav, sort, layout, counts, text, onText, onStatus, onFav, onSort, onLayout } = props
  const sorts = sortsWith(sort)
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Segmented
        label="Library status"
        options={STATUS_CHOICES.map((s) => ({ value: s, label: choiceLabel(s), count: countFor(counts, s, fav) }))}
        value={status}
        onChange={onStatus}
      />
      <FilterToggle on={fav} onChange={onFav}>
        Favourites only
      </FilterToggle>
      <div className="ml-auto flex flex-wrap items-center gap-2">
        <SearchInput
          value={text}
          onChange={(e) => onText(e.target.value)}
          onClear={() => onText('')}
          placeholder="Filter your list…"
          aria-label="Filter library"
          className="w-64 max-w-full"
        />
        <Select value={sort} onChange={onSort} aria-label="Sort library">
          {sorts.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </Select>
        <LayoutToggle layout={layout} onLayout={onLayout} />
      </div>
      <div className="basis-full">
        <MoreFilterFields more={props.more} onMore={props.onMore} onClearMore={props.onClearMore} />
      </div>
    </div>
  )
}

export function PhoneControls(props: ControlProps) {
  const { status, fav, sort, layout, counts, onStatus, onFav, onSort, onLayout } = props
  const [sheet, setSheet] = useState<'status' | 'sort' | null>(null)
  const close = () => setSheet(null)
  const count = countFor(counts, status, fav)
  return (
    <div className="flex items-center gap-1.5">
      <button
        type="button"
        aria-haspopup="dialog"
        aria-expanded={sheet === 'status'}
        onClick={() => setSheet('status')}
        className="press flex h-11 max-w-[55%] shrink-0 items-center gap-1 rounded-xl bg-base-800 px-2.5 text-sm font-medium text-base-100"
      >
        {fav && <HeartGlyph />}
        <span className="truncate">
          {choiceLabel(status)}
          {count !== undefined && ` · ${count}`}
        </span>
        <ChevronGlyph />
      </button>
      <button
        type="button"
        aria-haspopup="dialog"
        aria-expanded={sheet === 'sort'}
        aria-label={`Sort: ${sortLabel(sort)}`}
        onClick={() => setSheet('sort')}
        className="press flex h-11 min-w-0 flex-1 items-center gap-1.5 rounded-xl bg-base-800 px-2.5 text-sm font-medium text-base-100"
      >
        <SortGlyph />
        <span className="truncate">{sortLabel(sort)}</span>
      </button>
      <LayoutToggle layout={layout} onLayout={onLayout} narrow />

      {sheet === 'status' && (
        <BottomSheet title="Status" onClose={close}>
          <div role="radiogroup" aria-label="Status">
            {STATUS_CHOICES.map((s) => {
              const n = countFor(counts, s, fav)
              return (
                <Radio
                  key={s || 'all'}
                  checked={s === status}
                  onPick={() => {
                    onStatus(s)
                    close()
                  }}
                  aside={
                    n !== undefined && (
                      <span className={cx('tabular-nums', n === 0 ? 'text-base-500' : 'text-base-400')}>{n}</span>
                    )
                  }
                  dim={n === 0}
                >
                  {choiceLabel(s)}
                </Radio>
              )
            })}
          </div>
          <label className="mt-1 flex min-h-[52px] cursor-pointer items-center justify-between gap-3 border-t border-base-700 px-3 text-[15px] text-base-100">
            Favourites only
            <Switch on={fav} onChange={onFav} aria-label="Favourites only" />
          </label>
          <div className="border-t border-base-700 px-2 pt-3 pb-1">
            <MoreFilterFields more={props.more} onMore={props.onMore} onClearMore={props.onClearMore} stacked />
          </div>
        </BottomSheet>
      )}
      {sheet === 'sort' && (
        <BottomSheet title="Sort by" onClose={close}>
          <div role="radiogroup" aria-label="Sort by">
            {sortsWith(sort).map((s) => (
              <Radio
                key={s.value}
                checked={s.value === sort}
                onPick={() => {
                  onSort(s.value)
                  close()
                }}
              >
                {s.label}
              </Radio>
            ))}
          </div>
        </BottomSheet>
      )}
    </div>
  )
}

function Radio({
  checked,
  onPick,
  aside,
  dim = false,
  children,
}: {
  checked: boolean
  onPick: () => void
  aside?: ReactNode
  dim?: boolean
  children: ReactNode
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={checked}
      onClick={onPick}
      className={cx(
        'flex min-h-[52px] w-full items-center gap-3 rounded-lg px-3 text-left text-[15px] transition-colors hover:bg-base-700',
        dim && !checked ? 'text-base-500' : 'text-base-100',
      )}
    >
      <span
        aria-hidden
        className={cx(
          'grid size-5 shrink-0 place-items-center rounded-full border-2',
          checked ? 'border-accent-400' : 'border-base-600',
        )}
      >
        {checked && <span className="size-2.5 rounded-full bg-accent-400" />}
      </span>
      <span className="min-w-0 flex-1 truncate">{children}</span>
      {aside}
    </button>
  )
}

export function LayoutToggle({
  layout,
  onLayout,
  narrow = false,
}: {
  layout: Layout
  onLayout: (layout: Layout) => void
  narrow?: boolean
}) {
  const option = (value: Layout, label: string, icon: ReactNode) => (
    <button
      type="button"
      aria-pressed={layout === value}
      aria-label={label}
      title={label}
      onClick={() => onLayout(value)}
      className={cx(
        'grid place-items-center rounded-lg transition-colors',
        narrow ? 'size-11' : 'size-9 pointer-coarse:size-11',
        layout === value ? 'bg-base-750 text-white' : 'text-base-400 hover:text-white',
      )}
    >
      {icon}
    </button>
  )
  return (
    <div role="group" aria-label="Show as" className={cx('flex shrink-0 rounded-xl bg-base-900', narrow ? '' : 'gap-0.5 p-0.5')}>
      {option('grid', 'Grid', <GridGlyph />)}
      {option('list', 'List', <ListGlyph />)}
    </div>
  )
}

export function SearchToggle({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      aria-expanded={open}
      aria-label="Search your library"
      onClick={onToggle}
      className="press grid size-11 place-items-center rounded-full text-base-300 transition-colors hover:bg-base-800 hover:text-white aria-expanded:bg-base-800 aria-expanded:text-white"
    >
      <svg viewBox="0 0 24 24" className="size-5" aria-hidden>
        <circle cx="11" cy="11" r="6.5" fill="none" stroke="currentColor" strokeWidth="1.8" />
        <path d="m16 16 4.5 4.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
      </svg>
    </button>
  )
}

function ChevronGlyph() {
  return (
    <svg viewBox="0 0 20 20" className="size-4 shrink-0 text-base-400" aria-hidden>
      <path d="M6 8l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function SortGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0 text-base-400" aria-hidden>
      <path d="M7 5v14m0 0-3-3m3 3 3-3M17 19V5m0 0-3 3m3-3 3 3" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function HeartGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4 shrink-0 text-accent-300" aria-hidden>
      <path d="M12 20s-7-4.4-7-10a4 4 0 0 1 7-2.6A4 4 0 0 1 19 10c0 5.6-7 10-7 10Z" fill="currentColor" />
    </svg>
  )
}

function GridGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" />
    </svg>
  )
}

function ListGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M9 6h11M9 12h11M9 18h11M4 6h1M4 12h1M4 18h1" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  )
}

function sortsWith(sort: string): Array<{ value: string; label: string }> {
  const sorts: Array<{ value: string; label: string }> = [...SORTS]
  if (!sorts.some((s) => s.value === sort)) sorts.push({ value: sort, label: sortLabel(sort) })
  return sorts
}
