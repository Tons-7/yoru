import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { Button, ConfirmInline, Empty, LinkButton, PageHeader, ProgressBar, Segmented } from '../components/ui'
import { api, fetchAuthed } from '../lib/api'
import { bytes, cx } from '../lib/format'
import {
  cannotSave,
  removeSaved,
  retrySave,
  savedKey,
  sendWatched,
  setSaveQuality,
  useSaved,
  useWatched,
  watchedThrough,
  type Job,
  type SavedEpisode,
  type SaveQuality,
  type SaveStatus,
} from '../lib/offline'
import { useAccess } from '../lib/queries'

const QUALITIES: ReadonlyArray<{ value: SaveQuality; label: string }> = [
  { value: 'original', label: 'Original' },
  { value: '720', label: 'Smaller (720p)' },
]

function useRoomHere(deps: unknown) {
  const [free, setFree] = useState<number | null>(null)
  useEffect(() => {
    void navigator.storage
      ?.estimate?.()
      .then((e) => setFree(e.quota ? e.quota - (e.usage ?? 0) : null))
      .catch(() => {})
  }, [deps])
  return free
}

export function SavedPage({ offline = false }: { offline?: boolean }) {
  const { saved, jobs, status, loaded, quality } = useSaved()
  const watched = useWatched()
  const free = useRoomHere(saved.length)
  const total = saved.reduce((n, e) => n + e.bytes, 0)
  const unsent = Object.values(watched).filter((w) => w.dirty).length
  const shows = groupByShow(saved)
  const why = cannotSave()
  const suffix = offline ? '?offline=1' : ''

  return (
    <div className={cx('space-y-5', offline && 'mx-auto max-w-3xl px-4 pt-6 pb-16')}>
      {offline && <Unreachable unsent={unsent} />}
      <PageHeader
        title="On this device"
        meta={[
          saved.length > 0 && `${saved.length} ${saved.length === 1 ? 'episode' : 'episodes'}`,
          total > 0 && bytes(total),
          free != null && `${bytes(free)} free here`,
        ]
          .filter(Boolean)
          .join(' · ')}
        actions={!offline && <LinkButton to="/downloads">On the PC</LinkButton>}
      />

      {why && !offline && <CantSave why={why} />}

      {!why && !offline && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <span className="text-sm text-base-300">New saves</span>
          <Segmented size="sm" options={QUALITIES} value={quality} onChange={setSaveQuality} />
          <span className="text-xs text-base-500">
            {quality === '720'
              ? 'Converted to 720p on the PC: less room, a little less sharp.'
              : "The release as it is, converted only where this device can't play it."}
          </span>
        </div>
      )}

      {jobs.length > 0 && (
        <section aria-label="Saving" className="surface space-y-1 p-3 sm:p-4">
          <h2 className="text-sm font-semibold text-white">Saving</h2>
          <p className="text-xs text-base-500">Keep Yoru open while it saves; it carries on where it stopped.</p>
          <ul className="divide-y divide-white/[0.06]">
            {jobs.map((j) => (
              <JobRow key={savedKey(j.animeId, j.episode)} job={j} status={status.get(savedKey(j.animeId, j.episode))} />
            ))}
          </ul>
        </section>
      )}

      {loaded && saved.length === 0 && jobs.length === 0 && (
        <Empty
          title="Nothing saved on this device"
          hint="Save episodes from a show's episode list or the watch page. They play here with the PC off, subtitles and all."
        />
      )}

      {shows.map((show) => (
        <section key={show.animeId} aria-label={show.title} className="surface p-3 sm:p-4">
          <div className="flex items-center gap-3">
            {show.cover && <img src={show.cover} alt="" className="h-14 w-10 shrink-0 rounded-md object-cover" />}
            <div className="min-w-0">
              <h2 className="truncate font-medium text-white">{show.title}</h2>
              <p className="text-xs text-base-500">
                {show.episodes.length} {show.episodes.length === 1 ? 'episode' : 'episodes'} ·{' '}
                {bytes(show.episodes.reduce((n, e) => n + e.bytes, 0))}
              </p>
            </div>
          </div>
          <ul className="mt-2 divide-y divide-white/[0.06]">
            {show.episodes.map((e) => (
              <SavedRow key={e.key} episode={e} through={watchedThrough(e, watched)} to={`/saved/${e.key}${suffix}`} />
            ))}
          </ul>
        </section>
      ))}
    </div>
  )
}

function groupByShow(saved: SavedEpisode[]) {
  const shows: Array<{ animeId: number; title: string; cover?: string; episodes: SavedEpisode[] }> = []
  for (const e of saved) {
    const show = shows.find((s) => s.animeId === e.animeId)
    if (show) show.episodes.push(e)
    else shows.push({ animeId: e.animeId, title: e.title, cover: e.cover, episodes: [e] })
  }
  return shows
}

function SavedRow({ episode: e, through, to }: { episode: SavedEpisode; through: number; to: string }) {
  const [confirm, setConfirm] = useState(false)
  const done = through >= 0.9
  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-2 py-2.5">
      <Link to={to} className="min-w-0 flex-1 py-1">
        <p className="truncate text-sm text-base-100">{e.label}</p>
        <p className="text-xs text-base-500">
          {[done ? 'Watched' : through > 0 && `${Math.round(through * 100)}% watched`, bytes(e.bytes), e.quality === '720' && '720p']
            .filter(Boolean)
            .join(' · ')}
        </p>
        {through > 0 && !done && <ProgressBar value={through * 100} className="mt-1.5 max-w-48" label="Watched" />}
      </Link>
      <div className="flex items-center gap-2">
        {confirm ? (
          <ConfirmInline
            size="sm"
            label="Delete"
            note="From this device only"
            onConfirm={() => void removeSaved(e.key)}
            onCancel={() => setConfirm(false)}
          />
        ) : (
          <>
            <LinkButton to={to} size="sm" variant="primary">
              Play
            </LinkButton>
            <Button size="sm" onClick={() => setConfirm(true)} aria-label={`Delete ${e.label} from this device`}>
              Delete
            </Button>
          </>
        )}
      </div>
    </li>
  )
}

function JobRow({ job, status }: { job: Job; status?: SaveStatus }) {
  const key = savedKey(job.animeId, job.episode)
  const show = useQuery({
    queryKey: ['anime', job.animeId],
    queryFn: () => api.get<{ title: string }>(`/api/anime/${job.animeId}`),
    staleTime: 5 * 60_000,
  })
  const pct = status?.total ? (status.done / status.total) * 100 : 0
  const words = (() => {
    switch (status?.phase) {
      case 'saving':
        return `Saving ${Math.floor(pct)}% · ${bytes(status.bytes)}`
      case 'preparing':
        return 'Getting it ready on the PC'
      case 'waiting':
        return `Waiting for the PC to download it${status.percent != null ? ` · ${Math.floor(status.percent)}%` : ''}`
      case 'stopped':
        return `${status.message ?? 'Stopped'}; carries on when it answers`
      case 'failed':
        return `Couldn't save: ${status.message ?? 'something went wrong'}`
      default:
        return 'Waiting its turn'
    }
  })()
  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-2 py-2.5">
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm text-base-100">
          {[show.data?.title, `Episode ${job.episode}`, job.quality === '720' && '720p'].filter(Boolean).join(' · ')}
        </p>
        <p role="status" className={cx('text-xs', status?.phase === 'failed' ? 'text-recap' : 'text-base-500')}>
          {words}
        </p>
        {status?.phase === 'saving' && <ProgressBar value={pct} className="mt-1.5 max-w-48" label="Saved" />}
      </div>
      <div className="flex items-center gap-2">
        {status?.phase === 'failed' && (
          <Button size="sm" onClick={() => retrySave(key)}>
            Try again
          </Button>
        )}
        <Button size="sm" onClick={() => void removeSaved(key)} aria-label={`Stop saving episode ${job.episode}`}>
          {status?.phase === 'failed' ? 'Remove' : 'Stop'}
        </Button>
      </div>
    </li>
  )
}

function CantSave({ why }: { why: string }) {
  const access = useAccess()
  const secure = access.data?.urls.find((u) => u.startsWith('https://'))
  return (
    <div className="surface space-y-2 p-4 text-sm">
      <p className="text-base-100">{why}</p>
      <p className="text-base-400">
        {secure
          ? 'Open Yoru by its secure address on this device, and save from there.'
          : 'Set up HTTPS on the PC under Settings, Devices, then open Yoru by that address here.'}
      </p>
      {secure && (
        <a href={secure} className="inline-block text-accent-300 underline-offset-2 hover:underline">
          Open the secure address
        </a>
      )}
    </div>
  )
}

function Unreachable({ unsent }: { unsent: number }) {
  const [back, setBack] = useState(false)
  const [checking, setChecking] = useState(false)
  const check = async () => {
    setChecking(true)
    const ok = await fetchAuthed('/api/access', { cache: 'no-store', signal: AbortSignal.timeout(4000) }).then(
      (r) => r.ok,
      () => false,
    )
    setChecking(false)
    setBack(ok)
    if (ok) void sendWatched()
  }
  useEffect(() => {
    const first = window.setTimeout(() => void check(), 0)
    const id = window.setInterval(() => void check(), 15_000)
    return () => {
      window.clearTimeout(first)
      window.clearInterval(id)
    }
  }, [])
  return (
    <div role="status" className="surface flex flex-wrap items-center justify-between gap-3 p-4">
      <div className="min-w-0 text-sm">
        <p className="text-base-100">{back ? 'Yoru answers again' : "Can't reach Yoru on your PC"}</p>
        <p className="text-base-400">
          {back
            ? 'Everything else is back.'
            : `What's saved here still plays.${unsent > 0 ? ' What you watch is sent to Yoru once it answers.' : ''}`}
        </p>
      </div>
      {back ? (
        <Button variant="primary" onClick={() => window.location.assign('/')}>
          Open Yoru
        </Button>
      ) : (
        <Button onClick={() => void check()} disabled={checking}>
          {checking ? 'Trying…' : 'Try again'}
        </Button>
      )}
    </div>
  )
}
