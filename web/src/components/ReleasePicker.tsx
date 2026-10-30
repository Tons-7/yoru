import { useCallback, useState } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { api, query, type ReleaseCandidate } from '../lib/api'
import { bytes, cx } from '../lib/format'
import { Badge, Button, Skeleton, TextInput } from './ui'
import { useDismiss, useModalFocus } from '../lib/hooks'

/** Every release the finder saw, best first; ineligible ones greyed with the reason. */
export function ReleasePicker({
  animeId,
  episode,
  audio,
  current,
  purpose,
  onPick,
  onClose,
}: {
  animeId: number
  episode: number
  /** Sub or dub chosen for this episode, so the list is ranked for it. */
  audio?: string
  /** Info hash of what is playing or downloading now, to mark the row. */
  current?: string
  /** What picking does: plays the episode (default) or downloads it in place of the current release. */
  purpose?: 'play' | 'download'
  onPick: (infoHash: string, title: string) => void
  onClose: () => void
}) {
  const close = useCallback(() => onClose(), [onClose])
  const panel = useDismiss<HTMLDivElement>(close)
  useModalFocus(panel)

  // Words of the person's own, searched before the show's titles, or a magnet found elsewhere.
  const [typed, setTyped] = useState('')
  const [asked, setAsked] = useState('')
  const magnet = asked.startsWith('magnet:') ? asked : undefined
  const sources = useQuery({
    queryKey: ['sources', animeId, episode, audio, asked],
    // Asking is a POST: the server keeps the words or magnet for the next searches, so another site mustn't.
    queryFn: ({ signal }) => {
      const path = `/api/episode/sources${query({ id: animeId, episode, audio, q: magnet ? undefined : asked || undefined, magnet })}`
      type Found = { results: ReleaseCandidate[]; queries: string[] }
      return asked ? api.post<Found>(path, undefined, signal) : api.get<Found>(path, signal)
    },
    // Kept while the picker is open, even through a refetch of everything: each search is seconds per site.
    staleTime: 'static',
    // The search already tries every title; retrying it tripled the wait before saying a site is down.
    retry: false,
  })

  const results = sources.data?.results ?? []

  // Into the fullscreen player when there is one: outside it nothing is drawn,
  // and the next click in the player counts as outside the panel.
  return createPortal(
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/70 backdrop-blur-sm" />
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label="Choose a release"
        className="animate-fade-in relative flex max-h-[85vh] w-full max-w-2xl flex-col rounded-xl border border-base-800 bg-base-900 shadow-2xl"
      >
        <div className="flex items-start justify-between gap-4 border-b border-base-850 p-4">
          <div>
            <h2 className="text-base font-semibold text-white">Choose a release</h2>
            <p className="mt-0.5 text-xs text-base-500">
              Episode {episode} · best first.{' '}
              {purpose === 'download'
                ? 'Picking one downloads it instead of the current release.'
                : 'Picking one plays it now; the choice is not remembered.'}
            </p>
          </div>
          <button
            onClick={close}
            aria-label="Close"
            className="tap relative grid size-9 shrink-0 place-items-center rounded-md text-base-400 hover:bg-base-800 hover:text-base-100"
          >
            ✕
          </button>
        </div>

        {/* The titles find nothing when a group spells the show its own way; a pasted magnet skips search. */}
        <form
          className="flex gap-2 border-b border-base-850 px-4 py-2.5"
          onSubmit={(e) => {
            e.preventDefault()
            setAsked(typed.trim())
          }}
        >
          <TextInput
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder="Search the sites for…, or paste a magnet link"
            aria-label="Search the sites, or paste a magnet link"
            className="min-w-0 flex-1"
          />
          <Button type="submit" size="sm" disabled={!typed.trim() || sources.isFetching}>
            {typed.trim().startsWith('magnet:') ? 'Add' : 'Search'}
          </Button>
        </form>

        <div className="min-h-0 flex-1 overflow-y-auto p-2 scrollbar-thin">
          {sources.isPending || (sources.isError && sources.isFetching) ? (
            <div className="space-y-2 p-2">
              {Array.from({ length: 5 }, (_, i) => (
                <Skeleton key={i} className="h-16 w-full" />
              ))}
            </div>
          ) : sources.isError ? (
            // A way to ask again without closing the dialog mid-episode.
            <div className="flex flex-wrap items-center gap-3 p-4">
              <p className="text-sm text-recap">{(sources.error as Error).message}</p>
              <Button size="sm" onClick={() => void sources.refetch()}>
                Try again
              </Button>
            </div>
          ) : results.length === 0 ? (
            <p className="p-6 text-center text-sm text-base-500">
              No release found under any of the show's titles.
            </p>
          ) : (
            <ul className="space-y-1">
              {results.map((r) => (
                <Row
                  key={r.Torrent.infoHash}
                  candidate={r}
                  active={r.Torrent.infoHash === current}
                  // From Downloads the current copy isn't necessarily playing.
                  activeLabel={purpose === 'download' ? 'Current' : 'Playing'}
                  onPick={() => onPick(r.Torrent.infoHash, r.Torrent.title)}
                />
              ))}
            </ul>
          )}
          {/* Picked here by hand they still play; the limit itself is a setting. */}
          {results.some((r) => r.blocked === 'larger than the automatic size limit') && (
            <p className="px-2 pt-2 text-xs text-base-500">
              Some are over the automatic size limit; pick one here, or{' '}
              <Link to="/settings?tab=Downloads&q=largest" onClick={close} className="text-accent-300 hover:underline">
                change the limit
              </Link>
              .
            </p>
          )}
        </div>
      </div>
    </div>,
    document.fullscreenElement ?? document.body,
  )
}

function Row({
  candidate: r,
  active,
  activeLabel,
  onPick,
}: {
  candidate: ReleaseCandidate
  active: boolean
  activeLabel: string
  onPick: () => void
}) {
  const rel = r.Release
  const specs = [
    rel.Resolution,
    rel.Source,
    rel.Codec && `${rel.Codec}${rel.BitDepth === 10 ? ' 10-bit' : ''}`,
    rel.DualAudio && 'dual audio',
    rel.Batch && 'batch',
  ].filter(Boolean)

  // The curator's word on a release SeaDex lists: its notes and comparisons, under the row, not in its button.
  const curated = r.SeaDexBest || r.SeaDexListed
  const notes = curated ? r.SeaDexNotes : undefined
  const compare = curated ? (r.SeaDexCompare ?? []) : []
  const longNotes = !!notes && (notes.length > 140 || notes.includes('\n'))

  return (
    <li
      className={cx(
        'rounded-lg border transition-colors',
        active ? 'border-accent-500/50 bg-accent-500/10' : 'border-base-850',
        r.blocked && 'opacity-60',
      )}
    >
      <button
        onClick={onPick}
        title={r.reasons?.join('\n')}
        className={cx('block w-full rounded-lg px-3 py-2 text-left', !active && 'hover:bg-base-850')}
      >
        <p className="line-clamp-2 text-sm text-base-100">{r.Torrent.title}</p>
        <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-base-400">
          {rel.Group && <span className="text-base-200">{rel.Group}</span>}
          {specs.map((s) => (
            <span key={String(s)}>{s}</span>
          ))}
          <span>{bytes(r.Torrent.size)}</span>
          {r.Torrent.seedersKnown === false ? (
            <span>peers unknown</span>
          ) : (
            <span className={cx(r.Torrent.seeders === 0 && 'text-recap')}>
              {r.Torrent.seeders} seeders
            </span>
          )}
          <span className="ml-auto tabular-nums text-base-500">{Math.round(r.score)}</span>
          {active && <Badge tone="accent">{activeLabel}</Badge>}
          {r.SeaDexBest && <Badge tone="accent">SeaDex</Badge>}
          {!r.SeaDexBest && r.SeaDexListed && <Badge>SeaDex alt</Badge>}
          {curated && r.SeaDexIncomplete && <Badge tone="filler">SeaDex incomplete</Badge>}
          {!r.playable && <Badge tone="filler">Software transcode</Badge>}
          {r.blocked && <Badge tone="recap">{r.blocked}</Badge>}
        </div>
      </button>
      {(notes || compare.length > 0) && <CuratorNotes notes={notes} compare={compare} long={longNotes} />}
    </li>
  )
}

function CuratorNotes({ notes, compare, long }: { notes?: string; compare: string[]; long: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="space-y-1 px-3 pb-2 text-xs text-base-400">
      {notes && <p className={cx('whitespace-pre-line', long && !open && 'line-clamp-2')}>{notes}</p>}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        {long && (
          <button onClick={() => setOpen((o) => !o)} className="py-1 font-medium text-accent-300 hover:underline">
            {open ? 'Fewer notes' : 'All notes'}
          </button>
        )}
        {compare.map((url, i) => (
          <a
            key={url}
            href={url}
            target="_blank"
            rel="noopener noreferrer"
            className="py-1 font-medium text-accent-300 hover:underline"
          >
            {compare.length > 1 ? `Comparison ${i + 1}` : 'Comparison'}
          </a>
        ))}
      </div>
    </div>
  )
}
