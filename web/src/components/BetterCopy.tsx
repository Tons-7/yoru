import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '../lib/api'
import { bytes, cx } from '../lib/format'
import { refreshDownloads, type BetterCopy } from '../lib/queries'
import { Button, ConfirmInline } from './ui'

/** SeaDex's best release over a show's kept episodes, to replace or turn down for good; never taken alone. */
export function BetterCopyOffer({ offer, showTitle = true, className }: { offer: BetterCopy; showTitle?: boolean; className?: string }) {
  const qc = useQueryClient()
  const [confirming, setConfirming] = useState(false)
  const settled = () => {
    void qc.invalidateQueries({ queryKey: ['better-copies'] })
    refreshDownloads(qc)
  }
  const replace = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post(`/api/better-copies/${offer.animeId}/replace`, { infoHash: offer.infoHash }),
    onSettled: settled,
  })
  const dismiss = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post(`/api/better-copies/${offer.animeId}/dismiss`, { infoHash: offer.infoHash }),
    onSettled: settled,
  })
  const count = offer.episodes.length
  const episodes = count === 1 ? `episode ${offer.episodes[0]}` : `${count} episodes`
  const error = (replace.error ?? dismiss.error) as Error | null

  return (
    <div className={cx('flex flex-wrap items-center gap-x-3 gap-y-2', className)}>
      <div className="min-w-0 flex-1 basis-64">
        {showTitle && <p className="truncate text-sm font-medium text-base-100">{offer.title}</p>}
        <p className="text-xs text-base-300">
          Better copy · {offer.release} · SeaDex's best · {episodes} · {bytes(offer.size)}
        </p>
        <p className="truncate text-xs text-base-500" title={offer.kept}>
          Kept now: {offer.kept}
        </p>
        {error && <p className="text-xs text-recap">{error.message}</p>}
      </div>
      <div className="flex flex-wrap items-center gap-2">
        {confirming ? (
          <ConfirmInline
            tone="accent"
            size="sm"
            label={replace.isPending ? 'Queuing…' : `Replace ${episodes}`}
            note="Each kept copy goes once its replacement is complete"
            pending={replace.isPending}
            onConfirm={() => replace.mutate(undefined, { onSettled: () => setConfirming(false) })}
            onCancel={() => setConfirming(false)}
          />
        ) : (
          <>
            <Button size="sm" variant="primary" onClick={() => setConfirming(true)}>
              Replace
            </Button>
            <Button size="sm" variant="ghost" onClick={() => dismiss.mutate()} disabled={dismiss.isPending}>
              Don't offer again
            </Button>
          </>
        )}
      </div>
    </div>
  )
}
