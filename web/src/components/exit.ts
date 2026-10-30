import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../lib/api'
import { useAccess, useDownloads, type Download } from '../lib/queries'

export interface Watching {
  device: string
  title: string
  episode: number
}

export interface ExitStops {
  downloads: number
  left: number
  watching: Watching[]
  unknown?: boolean
}

function stopsOf(items: Download[] | undefined, watching: Watching[] | undefined): ExitStops {
  const active = (items ?? []).filter((d) => d.percent < 100 && !d.paused)
  return {
    downloads: active.length,
    left: active.reduce((sum, d) => sum + Math.max(0, d.totalBytes - d.bytesOnDisk), 0),
    watching: watching ?? [],
    unknown: items === undefined || watching === undefined,
  }
}

const stopsSummary = (s: ExitStops) =>
  [
    s.downloads > 0 && `${s.downloads} downloading`,
    s.watching.length === 1 && `${s.watching[0].device} is watching`,
    s.watching.length > 1 && `${s.watching.length} devices watching`,
  ]
    .filter(Boolean)
    .join(' · ')

export function useExitYoru() {
  const [exiting, setExiting] = useState(false)
  const [failed, setFailed] = useState('')
  const exit = () => {
    setFailed('')
    setExiting(true)
    api.post('/api/app/exit').catch((e: unknown) => {
      setExiting(false)
      setFailed(e instanceof Error ? e.message : "Couldn't exit Yoru")
    })
  }
  return { exit, exiting, failed }
}

export function useExitCheck(onAsk: (stops: ExitStops) => void) {
  const access = useAccess()
  const host = !!access.data?.host
  const downloads = useDownloads()
  const activity = useQuery({
    queryKey: ['app-activity'],
    queryFn: () => api.get<{ watching: Watching[] }>('/api/app/activity'),
    enabled: host,
    staleTime: 0,
    refetchInterval: 5000,
  })
  const { exit, exiting, failed } = useExitYoru()
  const [checking, setChecking] = useState(false)
  const summary = host ? stopsSummary(stopsOf(downloads.data?.items, activity.data?.watching)) : ''

  const choose = async () => {
    setChecking(true)
    const [d, a] = await Promise.all([downloads.refetch(), activity.refetch()])
    setChecking(false)
    const stops = stopsOf(d.data?.items, a.data?.watching)
    if (stops.unknown || stopsSummary(stops)) return onAsk(stops)
    exit()
  }
  return { host, summary, choose, exiting, checking, failed }
}
