import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import { bytes, cx } from '../lib/format'
import { useAccess } from '../lib/queries'
import { toast } from '../lib/toast'
import { buttonClass } from '../lib/ui'
import { BrowseFolder } from './BrowseFolder'
import { Spinner, TextInput } from './ui'

interface LocalState {
  stats: { files: number; matched: number; unmatched: number; missing: number; bytes: number; anime: number }
  roots: string[]
  scanning: boolean
  lastScan?: { seen: number; video: number; took: string; errors?: string[] } | null
}

const small = buttonClass()

export function LocalFolders() {
  const qc = useQueryClient()
  const host = useAccess().data?.host === true
  const local = useQuery({
    queryKey: ['local'],
    queryFn: () => api.get<LocalState>('/api/local'),
    refetchInterval: (q) => (q.state.data?.scanning ? 2000 : false),
  })
  const [path, setPath] = useState('')
  const save = useMutation({
    meta: { inline: true },
    mutationFn: (paths: string[]) => api.post('/api/local/paths', { paths }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['local'] }),
  })
  const scan = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post('/api/local/scan'),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['local'] }),
  })
  const show = useMutation({
    meta: { inline: true },
    mutationFn: (folder: string) => api.post('/api/setup/show', { which: 'library', path: folder }),
  })

  const scanning = !!local.data?.scanning
  const wasScanning = useRef(scanning)
  useEffect(() => {
    if (wasScanning.current && !scanning) {
      for (const key of ['episodes', 'anime', 'localFiles']) void qc.invalidateQueries({ queryKey: [key] })
    }
    wasScanning.current = scanning
  }, [scanning, qc])

  const roots = local.data?.roots ?? []
  const stats = local.data?.stats
  const problems =
    save.error instanceof ApiError ? ((save.error.body as { paths?: Record<string, string> } | undefined)?.paths ?? {}) : {}
  const remove = (folder: string) => {
    const before = roots
    save.mutate(
      roots.filter((r) => r !== folder),
      { onSuccess: () => toast(`Removed ${folder}`, 'info', { label: 'Undo', run: () => save.mutate(before) }) },
    )
  }
  const add = () => {
    const next = path.trim()
    if (next) save.mutate([...roots, next], { onSuccess: () => setPath('') })
  }

  return (
    <div className="py-1">
      {roots.length > 0 ? (
        <ul className="divide-y divide-white/5">
          {roots.map((folder) => (
            <li key={folder} className="flex flex-wrap items-center gap-x-3.5 gap-y-2 py-3">
              <span aria-hidden className="grid size-10 shrink-0 place-items-center rounded-full bg-accent-500/15 text-accent-300">
                <svg viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinejoin="round">
                  <path d="M3.5 8A1.5 1.5 0 0 1 5 6.5h4l2 2h8A1.5 1.5 0 0 1 20.5 10v7.5A1.5 1.5 0 0 1 19 19H5a1.5 1.5 0 0 1-1.5-1.5z" />
                </svg>
              </span>
              <p className="min-w-0 flex-1 basis-48 truncate font-mono text-[13px] text-base-100" title={folder}>
                {folder}
              </p>
              {host && (
                <div className="flex shrink-0 gap-2 max-sm:pl-[3.375rem]">
                  <button type="button" onClick={() => show.mutate(folder)} disabled={show.isPending} className={small}>
                    Open
                  </button>
                  <button type="button" onClick={() => remove(folder)} disabled={save.isPending} aria-label={`Remove ${folder}`} className={small}>
                    Remove
                  </button>
                </div>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="py-3 text-[13px] text-base-400">
          Add a folder of anime you already have. Yoru matches each file to its show and plays it straight from disk.
        </p>
      )}

      {host && (
        <form
          className="flex flex-wrap gap-2 border-t border-white/5 py-3"
          onSubmit={(e) => {
            e.preventDefault()
            add()
          }}
        >
          <TextInput
            value={path}
            onChange={(e) => {
              setPath(e.target.value)
              if (save.isError) save.reset()
            }}
            placeholder="Paste a folder, like D:\Anime"
            aria-label="Folder to add"
            className="min-w-0 flex-1 basis-56"
          />
          <BrowseFolder
            title="Add a folder of anime you already have"
            start={roots.at(-1)}
            disabled={save.isPending}
            onPick={(picked) => save.mutate([...roots, picked], { onSuccess: () => setPath('') })}
          />
          <button type="submit" disabled={save.isPending || path.trim() === ''} className={buttonClass('primary')}>
            {save.isPending ? 'Adding…' : 'Add folder'}
          </button>
        </form>
      )}
      {save.isError && (
        <div className="pb-3 text-xs text-recap">
          {Object.keys(problems).length > 0 ? (
            Object.entries(problems).map(([p, why]) => (
              <p key={p}>
                {p}: {why}
              </p>
            ))
          ) : (
            <p>{(save.error as Error).message}</p>
          )}
        </div>
      )}
      {show.isError && <p className="pb-3 text-xs text-recap">{(show.error as Error).message}</p>}

      {roots.length > 0 && stats && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-3 border-t border-white/5 py-3.5">
          <dl className="flex min-w-0 flex-1 flex-wrap gap-x-6 gap-y-2">
            <Stat label="Files" value={stats.files.toLocaleString()} />
            <Stat label="Shows" value={stats.anime.toLocaleString()} />
            <Stat label="Size" value={bytes(stats.bytes)} />
            {stats.unmatched > 0 && <Stat label="Need a match" value={stats.unmatched.toLocaleString()} warn />}
          </dl>
          <div className="flex shrink-0 flex-wrap items-center gap-2">
            <Link to="/local" className={small}>
              {stats.unmatched > 0 ? 'Match files' : 'See files'}
            </Link>
            {host && (
              <button type="button" onClick={() => scan.mutate()} disabled={scanning || scan.isPending} className={small}>
                {scanning && <Spinner className="size-3.5" />}
                {scanning ? 'Scanning…' : 'Scan again'}
              </button>
            )}
          </div>
        </div>
      )}
      {scan.isError && <p className="pb-3 text-xs text-recap">{(scan.error as Error).message}</p>}
    </div>
  )
}

function Stat({ label, value, warn }: { label: string; value: string; warn?: boolean }) {
  return (
    <div className="flex flex-col">
      <dt className="text-xs text-base-400">{label}</dt>
      <dd className={cx('text-[15px] font-semibold tabular-nums', warn ? 'text-amber-200' : 'text-white')}>{value}</dd>
    </div>
  )
}
