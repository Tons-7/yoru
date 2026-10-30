import { useEffect, useState } from 'react'
import { sortDownloads, type DownloadSections } from './downloads'
import { savedKey, savedShare, useSaved, type Job, type SaveStatus } from './offline'
import { useDownloadQueue, useDownloads } from './queries'

export interface Activity {
  count: number
  progress: number | null
  trouble: number
}

export const IDLE: Activity = { count: 0, progress: null, trouble: 0 }

export function pcActivity(s: DownloadSections): Activity {
  const moving = [...s.active, ...s.held]
  const sized = moving.filter((d) => d.totalBytes > 0)
  const count = moving.length + s.searching.length + s.waitingQueue.length
  return {
    count,
    progress: sized.length > 0 ? sized.reduce((n, d) => n + Math.min(1, d.bytesOnDisk / d.totalBytes), 0) / count : null,
    trouble: s.failed.length,
  }
}

export function deviceActivity(jobs: Job[], status: Map<string, SaveStatus>): Activity {
  const states = jobs.map((j) => status.get(savedKey(j.animeId, j.episode)))
  const going = states.filter((s) => s?.phase !== 'failed' && s?.phase !== 'paused')
  return {
    count: going.length,
    progress: going.length > 0 ? going.reduce((n, s) => n + savedShare(s), 0) / going.length : null,
    trouble: states.filter((s) => s?.phase === 'failed').length,
  }
}

export function joinActivity(a: Activity, b: Activity): Activity {
  const count = a.count + b.count
  const weigh = (x: Activity) => (x.progress ?? 0) * x.count
  return {
    count,
    progress: a.progress == null && b.progress == null ? null : count > 0 ? (weigh(a) + weigh(b)) / count : null,
    trouble: a.trouble + b.trouble,
  }
}

export function usePcActivity(): Activity {
  const downloads = useDownloads()
  const queue = useDownloadQueue()
  return pcActivity(sortDownloads(downloads.data?.items ?? [], queue.data?.items ?? []))
}

export function useDeviceActivity(): Activity {
  const { jobs, status } = useSaved()
  return deviceActivity(jobs, status)
}

export function activityWords(a: Activity, done: boolean): string {
  return [
    a.count > 0 && `${a.count} in progress`,
    a.trouble > 0 && `${a.trouble} ${a.trouble === 1 ? 'needs' : 'need'} attention`,
    a.count === 0 && a.trouble === 0 && done && 'all done',
  ]
    .filter(Boolean)
    .join(', ')
}

export function useDoneFlash({ count, trouble }: Activity): boolean {
  const [was, setWas] = useState(count)
  const [done, setDone] = useState(false)
  if (was !== count) {
    setWas(count)
    if (was > 0 && count === 0 && trouble === 0) setDone(true)
    else if (count > 0 && done) setDone(false)
  }
  useEffect(() => {
    if (!done) return
    const t = window.setTimeout(() => setDone(false), 60_000)
    return () => window.clearTimeout(t)
  }, [done])
  return done
}
