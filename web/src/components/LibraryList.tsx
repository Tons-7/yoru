import { Link } from 'react-router-dom'
import { statusLabel, type LibraryItem } from '../lib/api'
import { cx } from '../lib/format'
import { statusWord, useProgress } from '../lib/library'
import { lastWatched, seasonOf } from '../lib/libraryText'
import { useNow } from '../lib/queries'
import { PlusOne, ProgressBar3, ProgressMeta } from './LibraryCard'
import { QuickEdit } from './QuickEdit'

function Thumb({ item, className }: { item: LibraryItem; className: string }) {
  return item.cover ? (
    <img src={item.cover} alt="" loading="lazy" decoding="async" className={cx('aspect-[2/3] shrink-0 rounded-md object-cover', className)} />
  ) : (
    <span aria-hidden className={cx('aspect-[2/3] shrink-0 rounded-md bg-base-850', className)} />
  )
}

export function LibraryRows({ items, showStatus }: { items: LibraryItem[]; showStatus: boolean }) {
  return (
    <ul className="divide-y divide-base-800/70">
      {items.map((item) => (
        <Row key={item.id} item={item} showStatus={showStatus} />
      ))}
    </ul>
  )
}

function Row({ item, showStatus }: { item: LibraryItem; showStatus: boolean }) {
  const p = useProgress(item)
  const { name, season } = seasonOf(item.title, item.format)
  return (
    <li className="flex min-h-20 items-center gap-2 py-1.5">
      <Link to={`/anime/${item.id}`} className="flex min-w-0 flex-1 items-center gap-3 py-0.5" aria-label={item.title}>
        <Thumb item={item} className="w-12" />
        <span className="min-w-0 flex-1">
          <span className="line-clamp-2 text-[15px] leading-5 font-semibold text-base-100">{name}</span>
          {season && <span className="block truncate text-xs text-base-400">{season}</span>}
          <ProgressMeta p={p} prefix={showStatus ? statusWord(item.status) : undefined} className="mt-0.5" />
          {(p.percent > 0 || p.done) && <ProgressBar3 p={p} className="mt-1.5" />}
        </span>
      </Link>
      <PlusOne item={item} p={p} />
      <QuickEdit item={item} large />
    </li>
  )
}

const COLUMNS = [
  { sort: 'title', label: 'Title', className: 'w-auto' },
  { sort: 'progress', label: 'Progress', className: 'w-64' },
  { sort: 'score', label: 'Score', className: 'w-20' },
  { sort: 'status', label: 'Status', className: 'w-36' },
  { sort: 'watched', label: 'Last watched', className: 'w-36' },
] as const

export function LibraryTable({
  items,
  sort,
  onSort,
}: {
  items: LibraryItem[]
  sort: string
  onSort: (sort: string) => void
}) {
  const now = useNow(600_000) / 1000
  return (
    <table className="w-full table-fixed border-collapse text-sm">
      <thead>
        <tr className="border-b border-base-800 text-left text-[13px] text-base-400">
          {COLUMNS.map((c) => {
            const active = c.sort === sort
            const rising = c.sort === 'title' || c.sort === 'status'
            return (
              <th
                key={c.sort}
                scope="col"
                aria-sort={active ? (rising ? 'ascending' : 'descending') : undefined}
                className={cx('py-2 pr-3 font-medium', c.className)}
              >
                <button
                  type="button"
                  onClick={() => onSort(c.sort)}
                  className={cx(
                    'flex h-9 items-center gap-1 rounded-md transition-colors hover:text-white',
                    active && 'text-white',
                  )}
                >
                  {c.label}
                  {active && <span aria-hidden>{rising ? '▴' : '▾'}</span>}
                </button>
              </th>
            )
          })}
          <th scope="col" className="w-12 py-2">
            <span className="sr-only">Edit</span>
          </th>
        </tr>
      </thead>
      <tbody>
        {items.map((item) => (
          <TableRow key={item.id} item={item} now={now} />
        ))}
      </tbody>
    </table>
  )
}

function TableRow({ item, now }: { item: LibraryItem; now: number }) {
  const p = useProgress(item)
  const { name, season } = seasonOf(item.title, item.format)
  const tens = Math.round(item.score / 10)
  const total = item.episodes ?? p.aired
  const when = lastWatched(item.resume?.lastPlayed, now)
  return (
    <tr className="h-14 border-b border-base-800/60 transition-colors hover:bg-base-900">
      <td className="py-1.5 pr-3">
        <Link to={`/anime/${item.id}`} className="flex min-w-0 items-center gap-3 text-base-100 hover:text-white">
          <Thumb item={item} className="w-8" />
          <span className="min-w-0">
            <span className="block truncate font-medium">{name}</span>
            {season && <span className="block truncate text-xs text-base-400">{season}</span>}
          </span>
        </Link>
      </td>
      <td className="py-1.5 pr-3">
        <span className="flex items-center gap-3">
          <ProgressBar3 p={p} className="w-24 shrink-0" />
          <span className={cx('min-w-0 flex-1 truncate tabular-nums', p.done ? 'text-emerald-400' : 'text-base-200')}>
            {p.watched} / {total ?? '?'}
          </span>
          <PlusOne item={item} p={p} />
        </span>
      </td>
      <td className={cx('py-1.5 pr-3 tabular-nums', tens ? 'text-base-100' : 'text-base-600')}>{tens || '·'}</td>
      <td className={cx('truncate py-1.5 pr-3', item.status ? 'text-base-200' : 'text-base-500')}>
        {statusLabel(item.status) || 'Not in a list'}
      </td>
      <td className={cx('py-1.5 pr-3', when ? 'text-base-300' : 'text-base-600')}>{when ?? 'Never'}</td>
      <td className="py-1.5">
        <QuickEdit item={item} />
      </td>
    </tr>
  )
}
