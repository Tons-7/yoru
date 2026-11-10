import { useSyncExternalStore } from 'react'
import { catalogueSource } from '../lib/api'
import { cx } from '../lib/format'
import { freshness } from '../lib/freshness'

const LABEL = { fresh: 'Up to date', updating: 'Updating', saved: 'Saved copy' }
const TEXT = { fresh: 'text-emerald-300', updating: 'text-accent-300', saved: 'text-amber-200' }

export function CatalogueStatus({ label = true }: { label?: boolean }) {
  const updating = useSyncExternalStore(freshness.subscribe, freshness.updating)
  const savedAt = useSyncExternalStore(catalogueSource.subscribe, catalogueSource.savedAt)
  const state = savedAt != null ? 'saved' : updating ? 'updating' : 'fresh'
  return (
    <span
      role="img"
      aria-label={`Catalogue: ${LABEL[state]}`}
      data-catalogue={state}
      className={cx('flex h-9 shrink-0 items-center gap-1.5 px-1 text-xs font-medium whitespace-nowrap', TEXT[state])}
    >
      {state === 'updating' ? (
        <span className="size-3 animate-spin rounded-full border-2 border-accent-400/30 border-t-accent-400 motion-reduce:animate-none" />
      ) : (
        <span className={cx('m-0.5 size-2 rounded-full', state === 'fresh' ? 'bg-emerald-400' : 'bg-amber-400')} />
      )}
      {label && <span className="hidden xl:inline">{LABEL[state]}</span>}
    </span>
  )
}
