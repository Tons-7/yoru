import { cx } from '../lib/format'

const GENRES = [
  { value: 'Action', label: 'Action', from: '#b4423a', to: '#6f2430' },
  { value: 'Romance', label: 'Romance', from: '#c06a8a', to: '#6d3150' },
  { value: 'Fantasy', label: 'Fantasy', from: '#3f7fae', to: '#24405f' },
  { value: 'Comedy', label: 'Comedy', from: '#d0913a', to: '#7a4f1c' },
  { value: 'Slice of Life', label: 'Slice of life', from: '#4c8d6a', to: '#244a39' },
  { value: 'Mystery', label: 'Mystery', from: '#6c5ac0', to: '#382d73' },
]

export function RecentSearches({
  searches,
  onPick,
  onForget,
  onForgetAll,
}: {
  searches: string[]
  onPick: (search: string) => void
  onForget: (search: string) => void
  onForgetAll: () => void
}) {
  return (
    <section aria-label="Recent searches" className="flex items-center gap-2 max-sm:-mx-4 max-sm:overflow-x-auto max-sm:px-4 max-sm:no-scrollbar sm:flex-wrap">
      <h2 className="mr-1 shrink-0 text-[13px] font-semibold text-base-400">Recent</h2>
      <ul className="contents">
        {searches.map((search) => (
          <li key={search} className="flex h-9 shrink-0 items-center rounded-full bg-base-850 text-[13px] font-medium text-base-200">
            <button
              type="button"
              onClick={() => onPick(search)}
              className="tap relative flex h-full max-w-[14rem] items-center gap-1.5 rounded-l-full pr-1 pl-3 transition-colors hover:text-white"
            >
              <ClockGlyph />
              <span className="truncate">{search}</span>
            </button>
            <button
              type="button"
              onClick={() => onForget(search)}
              aria-label={`Forget ${search}`}
              title="Forget this search"
              className="tap relative grid h-full w-8 place-items-center rounded-r-full text-base-500 transition-colors hover:text-white"
            >
              <CrossGlyph />
            </button>
          </li>
        ))}
      </ul>
      <button
        type="button"
        onClick={onForgetAll}
        className="tap relative h-9 shrink-0 px-2 text-[13px] font-medium text-base-500 transition-colors hover:text-white"
      >
        Clear
      </button>
    </section>
  )
}

export function GenreTiles({ chosen, onToggle }: { chosen: string[]; onToggle: (genre: string) => void }) {
  return (
    <section aria-label="Genres" className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
      {GENRES.map((genre) => {
        const on = chosen.includes(genre.value)
        return (
          <button
            key={genre.value}
            type="button"
            aria-pressed={on}
            onClick={() => onToggle(genre.value)}
            style={{ backgroundImage: `linear-gradient(135deg, ${genre.from}, ${genre.to})` }}
            className={cx(
              'press relative flex h-12 items-center rounded-card px-3.5 text-left text-[15px] font-semibold text-white transition-[filter] hover:brightness-110',
              on && 'ring-2 ring-white ring-inset',
            )}
          >
            {genre.label}
            {on && (
              <span className="absolute top-1/2 right-3 grid size-5 -translate-y-1/2 place-items-center rounded-full bg-white text-base-950">
                <CheckGlyph />
              </span>
            )}
          </button>
        )
      })}
    </section>
  )
}

function ClockGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-3.5 shrink-0 text-base-500" aria-hidden>
      <circle cx="12" cy="12" r="8.5" fill="none" stroke="currentColor" strokeWidth="1.9" />
      <path d="M12 7.5V12l3 2" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" />
    </svg>
  )
}

function CrossGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-3.5" aria-hidden>
      <path d="m6 6 12 12M18 6 6 18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
    </svg>
  )
}

function CheckGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-3.5" aria-hidden>
      <path d="m5 13 4 4L19 7" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}
