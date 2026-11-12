import { useState, type ReactNode } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { api, ApiError } from '../lib/api'
import { bytes, cx } from '../lib/format'
import { useAccess, useDriveRoom, type SetupState } from '../lib/queries'
import { buttonClass } from '../lib/ui'
import { BrowseFolder } from './BrowseFolder'
import { TextInput } from './ui'

type Which = 'data' | 'cache' | 'bin' | 'config'

const ICONS: Record<'data' | 'cache' | 'bin' | 'own', ReactNode> = {
  data: (
    <>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 7.5V12l3 2" />
    </>
  ),
  cache: <path d="M8.5 6.2v11.6l9.5-5.8z" />,
  bin: (
    <>
      <path d="M12 3.5 20 8v8l-8 4.5L4 16V8z" />
      <path d="M4 8l8 4.5L20 8M12 12.5v8" />
    </>
  ),
  own: <path d="M3.5 8A1.5 1.5 0 0 1 5 6.5h4l2 2h8A1.5 1.5 0 0 1 20.5 10v7.5A1.5 1.5 0 0 1 19 19H5a1.5 1.5 0 0 1-1.5-1.5z" />,
}

const small = buttonClass()

function Place({
  icon,
  title,
  text,
  path,
  actions,
  children,
}: {
  icon: keyof typeof ICONS
  title: string
  text: ReactNode
  path?: string
  actions?: ReactNode
  children?: ReactNode
}) {
  return (
    <li className="py-3.5">
      <div className="flex flex-wrap items-center gap-x-3.5 gap-y-2.5">
        <span aria-hidden className="grid size-10 shrink-0 place-items-center rounded-full bg-accent-500/15 text-accent-300">
          <svg viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
            {ICONS[icon]}
          </svg>
        </span>
        <div className="min-w-0 flex-1 basis-52">
          <p className="text-[15px] font-medium text-base-100 sm:text-sm">{title}</p>
          <div className="mt-0.5 text-[13px] text-base-400">{text}</div>
          {path && (
            <p className="mt-1 truncate font-mono text-xs text-base-500" title={path}>
              {path}
            </p>
          )}
        </div>
        {actions && <div className="flex shrink-0 flex-wrap gap-2 max-sm:pl-[3.375rem]">{actions}</div>}
      </div>
      {children && <div className="mt-3 sm:pl-[3.375rem]">{children}</div>}
    </li>
  )
}

export function WhereThingsGo({ setup }: { setup: SetupState }) {
  const host = useAccess().data?.host === true
  const used = useDriveRoom().data
  const [moving, setMoving] = useState(false)
  const show = useMutation({
    meta: { inline: true },
    mutationFn: (which: Which) => api.post('/api/setup/show', { which }),
  })
  const open = (which: Which) =>
    host && (
      <button type="button" onClick={() => show.mutate(which)} disabled={show.isPending} className={small}>
        Open
      </button>
    )
  const folders = setup.libraryPaths ?? []
  const budget = setup.cacheBudget
  const share = budget > 0 && used?.bytes !== undefined ? Math.min(1, used.bytes / budget) : null

  return (
    <div>
      <ul className="divide-y divide-white/5">
        <Place
          icon="data"
          title="History and settings"
          text="What you watched, your lists and every choice in Settings."
          path={setup.dataDir}
          actions={
            host && (
              <>
                {open('data')}
                <button type="button" aria-expanded={moving} onClick={() => setMoving((v) => !v)} className={small}>
                  {moving ? 'Cancel' : 'Move'}
                </button>
              </>
            )
          }
        >
          {moving && <DataDirPicker fallback={setup.defaultDataDir ?? setup.dataDir} current={setup.dataDir} />}
        </Place>
        <Place
          icon="cache"
          title="Episodes"
          text={
            share === null ? (
              `Kept after watching, up to ${bytes(budget)}.`
            ) : (
              <span className="flex items-center gap-2.5">
                <span className="tabular-nums">
                  {bytes(used!.bytes!)} of {bytes(budget)}
                </span>
                <span aria-hidden className="h-1.5 w-24 overflow-hidden rounded-full bg-base-800">
                  <span className={cx('block h-full rounded-full', share > 0.9 ? 'bg-amber-400' : 'bg-accent-400')} style={{ width: `${Math.max(share * 100, 3)}%` }} />
                </span>
              </span>
            )
          }
          path={setup.cacheDir}
          actions={open('cache')}
        />
        <Place icon="bin" title="Programs" text="The player engine and the other parts from Essentials." path={setup.binDir} actions={open('bin')} />
        <Place
          icon="own"
          title="Your own files"
          text={
            folders.length > 0
              ? `${folders.length} folder${folders.length === 1 ? '' : 's'} of episodes you already have`
              : 'Episodes you already have on this computer, played without downloading.'
          }
          actions={
            host && (
              <Link to="/settings?tab=Library" className={small}>
                {folders.length > 0 ? 'Manage' : 'Add a folder'}
              </Link>
            )
          }
        />
      </ul>
      {show.isError && <p className="pb-2 text-xs text-recap">{(show.error as Error).message}</p>}
      {setup.strayConfig && (
        <div className="mb-3 rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-[13px] text-amber-100">
          <p>
            There is a config.toml.txt next to config.toml. Yoru only reads config.toml, so move your changes into it and delete
            the .txt one.
          </p>
          <p className="mt-1 truncate font-mono text-xs text-amber-200/70" title={setup.strayConfig}>
            {setup.strayConfig}
          </p>
        </div>
      )}
      {host && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-t border-white/5 py-3.5 text-[13px] text-base-400">
          <p className="min-w-0 flex-1 basis-64">
            Episodes and programs move by editing <code className="text-base-300">cache_dir</code> or{' '}
            <code className="text-base-300">bin_dir</code> in config.toml, then restarting.
          </p>
          <button type="button" onClick={() => show.mutate('config')} disabled={show.isPending} className={buttonClass('ghost', 'sm')}>
            Show config.toml
          </button>
        </div>
      )}
    </div>
  )
}

function DataDirPicker({ fallback, current }: { fallback: string; current: string }) {
  const [path, setPath] = useState('')
  const move = useMutation({
    meta: { inline: true },
    mutationFn: (v: { path: string; useExisting?: boolean }) =>
      api.post<{ dataDir: string; copied: boolean; restart: boolean }>('/api/setup/data-dir', v),
  })
  const existing =
    move.error instanceof ApiError && move.error.status === 409
      ? ((move.error.body as { modified?: number }) ?? {})
      : null

  if (move.isSuccess) {
    return (
      <p className="text-[13px] text-accent-300">
        {move.data.restart === false
          ? 'Already using that folder.'
          : `Saved: ${move.data.dataDir}. Restart Yoru to use it${move.data.copied ? '. Your history was copied there.' : '.'}`}
      </p>
    )
  }
  return (
    <div className="space-y-2.5 rounded-xl bg-base-950/50 p-3 text-[13px]">
      <p className="text-base-400">
        Leave it empty for the default ({fallback}), or{' '}
        <button type="button" onClick={() => setPath('data')} className="text-accent-300 hover:underline">
          use Yoru's own folder
        </button>
        .
      </p>
      <form
        className="flex flex-wrap gap-2"
        onSubmit={(e) => {
          e.preventDefault()
          move.mutate({ path: path.trim() })
        }}
      >
        <TextInput
          value={path}
          onChange={(e) => {
            setPath(e.target.value)
            if (existing) move.reset()
          }}
          placeholder="D:\Yoru\data"
          aria-label="New folder for history and settings"
          className="min-w-0 flex-1 basis-56"
        />
        <BrowseFolder
          title="Choose where Yoru keeps your history and settings"
          start={current}
          onPick={(picked) => {
            setPath(picked)
            if (existing) move.reset()
          }}
        />
        <button type="submit" disabled={move.isPending} className={buttonClass('primary')}>
          Move here
        </button>
      </form>
      {existing ? (
        <div className="space-y-2 rounded-lg border border-amber-400/30 bg-amber-400/10 p-3 text-base-200">
          <p>
            That folder already has a Yoru history
            {existing.modified ? `, last changed ${new Date(existing.modified * 1000).toLocaleDateString()}` : ''}. Switching
            opens that one; what you have now stays where it is.
          </p>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={() => move.mutate({ path: move.variables?.path ?? path.trim(), useExisting: true })}
              className={buttonClass('primary', 'sm')}
            >
              Use that history
            </button>
            <button type="button" onClick={() => move.reset()} className={buttonClass('secondary', 'sm')}>
              Cancel
            </button>
          </div>
        </div>
      ) : (
        move.isError && <p className="text-recap">{(move.error as Error).message}</p>
      )}
    </div>
  )
}
