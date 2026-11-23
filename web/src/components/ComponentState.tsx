import { bytes } from '../lib/format'
import type { SetupProgress } from '../lib/queries'
import { ProgressBar } from './ui'

export function ComponentState({
  progress,
  present,
  manual,
  version,
  latest,
  onInstall,
  pending,
  requestError,
}: {
  progress?: SetupProgress
  present: boolean
  manual?: string
  version?: string
  latest?: string
  onInstall: () => void
  pending: boolean
  requestError?: string
}) {
  const update = present && !!latest && !!version && latest !== version
  if (present && progress?.stage !== 'failed' && !update) return null
  if (progress?.stage === 'done' && (!update || progress.version === latest)) {
    return <p className="mt-2 text-xs text-emerald-300">Installed</p>
  }

  if (manual) {
    return (
      <div className="mt-2">
        <p className="mb-1 text-xs text-base-400">Install it, then reload:</p>
        <code className="select-all rounded bg-base-800 px-2 py-1 text-xs text-base-100">{manual}</code>
      </div>
    )
  }

  if (progress && progress.stage !== 'done' && progress.stage !== 'failed') {
    const percent = progress.total > 0 ? (progress.bytes / progress.total) * 100 : 0
    return (
      <div className="mt-2">
        <p className="mb-1 text-xs text-base-400">
          {progress.stage === 'downloading'
            ? `Downloading ${bytes(progress.bytes)}${progress.total > 0 ? ` of ${bytes(progress.total)}` : ''}`
            : progress.stage === 'extracting'
              ? 'Extracting…'
              : 'Looking up the current version…'}
        </p>
        {percent > 0 && <ProgressBar value={percent} />}
      </div>
    )
  }

  return (
    <div className="mt-2 flex items-center gap-2">
      <button
        onClick={onInstall}
        disabled={pending}
        className="rounded-md bg-base-800 px-3 py-1 text-xs font-medium text-base-100 hover:bg-base-750 disabled:opacity-50 pointer-coarse:min-h-11"
      >
        {progress?.stage === 'failed' ? 'Try again' : update ? `Update to ${latest}` : 'Install'}
      </button>
      {(progress?.error ?? requestError) && (
        <span className="text-xs text-red-300" title={progress?.error ?? requestError}>
          {(progress?.error ?? requestError)!.slice(0, 80)}
        </span>
      )}
    </div>
  )
}
