import { useEffect, useState, useSyncExternalStore } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, catalogueSource } from '../lib/api'
import { relativeTime } from '../lib/format'
import { useNow } from '../lib/queries'

export function SavedCopyNotice() {
  const savedAt = useSyncExternalStore(catalogueSource.subscribe, catalogueSource.savedAt)
  const qc = useQueryClient()
  const now = useNow(60_000)
  const [retrying, setRetrying] = useState(false)
  const [stillDown, setStillDown] = useState(false)
  const [shownFor, setShownFor] = useState(savedAt)
  if (shownFor !== savedAt) {
    setShownFor(savedAt)
    if (savedAt == null) setStillDown(false)
  }
  const down = savedAt != null
  useEffect(() => {
    if (!down) return
    const id = window.setInterval(() => {
      void api
        .post<{ live: boolean }>('/api/anilist/recheck')
        .then((answer) => {
          if (!answer.live) return
          catalogueSource.clear()
          void qc.refetchQueries({ type: 'active' })
        })
        .catch(() => {})
    }, 120_000)
    return () => window.clearInterval(id)
  }, [down, qc])
  if (savedAt == null) return null

  return (
    <div
      role="status"
      className="mx-4 mb-2 flex items-center gap-3 rounded-xl bg-amber-500/10 px-3 py-2 text-xs text-amber-200 ring-1 ring-amber-400/25 sm:mx-6"
    >
      <span className="size-1.5 shrink-0 rounded-full bg-amber-400" />
      <p className="min-w-0 flex-1">
        AniList isn't answering, so this is Yoru's saved copy
        <span className="text-amber-200/70"> from {relativeTime(savedAt, now)}</span>. It updates by itself once AniList is back.
      </p>
      <button
        type="button"
        disabled={retrying}
        onClick={async () => {
          setRetrying(true)
          setStillDown(false)
          const answer = await api.post<{ live: boolean }>('/api/anilist/recheck').catch(() => ({ live: false }))
          if (answer.live) {
            catalogueSource.clear()
            await qc.refetchQueries({ type: 'active' }).catch(() => {})
          } else setStillDown(true)
          setRetrying(false)
        }}
        className="shrink-0 rounded-md px-3 py-1 font-medium pointer-coarse:min-h-11 text-amber-100 transition-colors hover:bg-amber-400/15 disabled:opacity-50"
      >
        {retrying ? 'Checking…' : stillDown ? 'Still down · Try again' : 'Try now'}
      </button>
    </div>
  )
}
