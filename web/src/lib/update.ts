import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from './api'
import { useAccess } from './queries'
import type { Notes } from '../components/ReleaseNotes'

export interface UpdateStatus {
  current: string
  latest?: { version: string; url: string; notes?: string }
  available: boolean
  checkedAt?: string
  checkError?: string
  stage: 'idle' | 'downloading' | 'verifying' | 'ready' | 'restarting' | 'failed'
  bytes: number
  total: number
  error?: string
  staged?: string
  pending?: Notes[]
  whatsNew?: Notes[]
  updatedFrom?: string
  auto?: boolean
  busy?: string[]
  fixed?: boolean
}

const KEY = ['update']
const IDLE_MS = 30_000
const MOVING_MS = 500

export const updateMoving = (stage?: UpdateStatus['stage']) =>
  stage === 'downloading' || stage === 'verifying' || stage === 'restarting'

export const updatePercent = (s: UpdateStatus) => (s.total > 0 ? Math.round((s.bytes / s.total) * 100) : 0)

export function useUpdateStatus(enabled = true) {
  return useQuery({
    queryKey: KEY,
    queryFn: () => api.get<UpdateStatus>('/api/update'),
    retry: false,
    enabled,
    refetchInterval: (q) => {
      if (q.state.data?.fixed || q.state.error instanceof ApiError) return false
      return updateMoving(q.state.data?.stage) ? MOVING_MS : IDLE_MS
    },
  })
}

export function useUpdateActions() {
  const qc = useQueryClient()
  const keep = (data: UpdateStatus) => qc.setQueryData(KEY, data)
  const check = useMutation({ mutationFn: () => api.post<UpdateStatus>('/api/update/check'), onSuccess: keep })
  const download = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<UpdateStatus>('/api/update/download'),
    onSuccess: keep,
  })
  const restart = useMutation({
    meta: { inline: true },
    mutationFn: () => api.post<UpdateStatus>('/api/update/restart'),
    onSuccess: keep,
  })
  return { check, download, restart }
}

export function useRestartToUpdate() {
  const qc = useQueryClient()
  const { restart } = useUpdateActions()
  const [ask, setAsk] = useState<string[] | null>(null)
  const [checking, setChecking] = useState(false)
  const choose = async () => {
    setChecking(true)
    const fresh = await api.get<UpdateStatus>('/api/update').catch(() => null)
    setChecking(false)
    if (fresh) qc.setQueryData(KEY, fresh)
    if (!fresh) setAsk(["Yoru couldn't check what is running, so anything playing stops"])
    else if (fresh.busy?.length) setAsk(fresh.busy)
    else restart.mutate()
  }
  return { choose, checking, ask, close: () => setAsk(null), restart }
}

export function useUpdateOffered() {
  const host = useAccess().data?.host === true
  const s = useUpdateStatus(host).data
  return host && !!s?.available && !!s.latest
}
