import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import { bytes, cx } from '../lib/format'
import {
  pauseSave,
  removeSaved,
  resumeSave,
  retrySave,
  saveEta,
  savedKey,
  savedShare,
  saveWords,
  type Job,
  type SaveStatus,
} from '../lib/offline'
import { CrossGlyph, iconButton, PauseGlyph, PlayGlyph } from './DownloadsBits'
import { Button, ProgressBar } from './ui'

export function SavingJob({
  job,
  status,
  place,
  compact = false,
}: {
  job: Job
  status?: SaveStatus
  place: number
  compact?: boolean
}) {
  const key = savedKey(job.animeId, job.episode)
  const show = useQuery({
    queryKey: ['anime', job.animeId],
    queryFn: () => api.get<{ title: string }>(`/api/anime/${job.animeId}`),
    staleTime: 5 * 60_000,
  })
  const eta = saveEta(status)
  const [asking, setAsking] = useState(false)
  const phase = status?.phase
  const share =
    phase === 'saving' ? savedShare(status) * 100 : phase === 'waiting' && status?.percent != null ? status.percent : null
  const name = [show.data?.title, `Episode ${job.episode}`, job.quality === '720' && '720p'].filter(Boolean).join(' · ')
  const words = [saveWords(status, place), eta].filter(Boolean).join(' · ')
  const cancel = () => (status?.bytes ? setAsking(true) : void removeSaved(key))

  return (
    <li className={cx('relative', compact ? 'py-2' : 'py-3')}>
      <div className="flex items-center gap-3">
        <div className="min-w-0 flex-1">
          <p className={cx('truncate text-base-100', compact ? 'text-xs font-medium' : 'text-sm')}>{name}</p>
          <p
            role="status"
            className={cx(
              compact ? 'text-xs' : 'text-[13px]',
              phase === 'failed' ? 'text-red-300' : phase === 'stopped' || phase === 'paused' ? 'text-amber-300' : 'text-base-400',
            )}
          >
            {words}
          </p>
        </div>
        {!compact && !asking && (
          <div className="flex shrink-0 items-center gap-1">
            {phase === 'failed' ? (
              <Button size="sm" onClick={() => retrySave(key)}>
                Try again
              </Button>
            ) : (
              <button
                type="button"
                onClick={() => (phase === 'paused' ? resumeSave(key) : pauseSave(key))}
                aria-label={phase === 'paused' ? `Resume saving episode ${job.episode}` : `Pause saving episode ${job.episode}`}
                className={iconButton}
              >
                {phase === 'paused' ? <PlayGlyph /> : <PauseGlyph />}
              </button>
            )}
            <button
              type="button"
              onClick={cancel}
              aria-label={phase === 'failed' ? `Remove episode ${job.episode}` : `Cancel saving episode ${job.episode}`}
              className={iconButton}
            >
              <CrossGlyph />
            </button>
          </div>
        )}
      </div>
      {asking && (
        <div className="mt-2 flex flex-wrap items-center justify-end gap-2">
          <span className="text-[13px] text-base-300 max-sm:basis-full">
            Cancel and lose {bytes(status?.bytes ?? 0)} saved so far?
          </span>
          <Button size="sm" variant="ghost" onClick={() => setAsking(false)} autoFocus>
            Keep saving
          </Button>
          <Button size="sm" variant="danger" onClick={() => void removeSaved(key)}>
            Cancel save
          </Button>
        </div>
      )}
      {share != null && (
        <ProgressBar
          value={share}
          label={phase === 'waiting' ? 'Downloaded on the PC' : 'Saved'}
          className={compact ? 'mt-1.5' : 'mt-2'}
        />
      )}
    </li>
  )
}
