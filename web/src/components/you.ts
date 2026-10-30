import { deviceActivity, IDLE, pcActivity, useDoneFlash, type Activity } from '../lib/activity'
import { sortDownloads } from '../lib/downloads'
import { speed } from '../lib/format'
import { savedKey, useSaved } from '../lib/offline'
import { useDownloadQueue, useDownloads, usePrefs, useSetPref, useSpeedUnit, useTrackers } from '../lib/queries'

export function useAccount() {
  const trackers = useTrackers().data?.trackers ?? []
  const signedIn = trackers.filter((t) => t.connected)
  return {
    user: signedIn.find((t) => t.user)?.user,
    signedIn,
    attention: trackers.some((t) => t.reconnect),
  }
}

export function useTitleLanguage() {
  const prefs = usePrefs()
  const setPref = useSetPref()
  const saved = prefs.data?.effective['display.titles'] === 'romaji'
  const settling = setPref.isPending || (setPref.isSuccess && prefs.isFetching)
  const japanese = settling ? setPref.variables?.value === 'romaji' : saved
  const choose = (jp: boolean) => setPref.mutate({ key: 'display.titles', value: jp ? 'romaji' : 'english' })
  return { japanese, settling, choose }
}

export interface DownloadsLine {
  activity: Activity
  words: string
  idle: boolean
}

const idleOf = (a: Activity, done: boolean) => a.count === 0 && a.trouble === 0 && !done

const attention = (n: number) => `${n} ${n === 1 ? 'needs' : 'need'} attention`

function lineWords(moving: number, verb: string, rate: string, waiting: number, a: Activity, done: boolean): string {
  const parts = [
    moving > 0 && `${moving} ${verb}`,
    moving > 0 && rate,
    waiting > 0 && `${waiting} waiting`,
    a.trouble > 0 && attention(a.trouble),
  ].filter(Boolean)
  if (parts.length > 0) return parts.join(' · ')
  return done ? 'All done' : 'Nothing downloading'
}

export function usePcLine(): DownloadsLine {
  const downloads = useDownloads()
  const queue = useDownloadQueue()
  const unit = useSpeedUnit()
  const s = sortDownloads(downloads.data?.items ?? [], queue.data?.items ?? [])
  const activity = pcActivity(s)
  const done = useDoneFlash(activity)
  const mbps = s.active.reduce((n, d) => n + (d.mbps ?? 0), 0)
  const moving = s.active.length + s.searching.length
  const waiting = s.held.length + s.waitingQueue.length
  return {
    activity,
    words: lineWords(moving, 'downloading', mbps > 0 ? speed(mbps, unit) : '', waiting, activity, done),
    idle: idleOf(activity, done),
  }
}

export function useDeviceLine(): DownloadsLine {
  const { jobs, status } = useSaved()
  const unit = useSpeedUnit()
  const activity = deviceActivity(jobs, status)
  const done = useDoneFlash(activity)
  const states = jobs.map((j) => status.get(savedKey(j.animeId, j.episode)))
  const rate = states.reduce((n, s) => n + (s?.phase === 'saving' ? (s.rate ?? 0) : 0), 0)
  return {
    activity,
    words: lineWords(activity.count, 'saving', rate > 0 ? speed((rate * 8) / 1_000_000, unit) : '', 0, activity, done),
    idle: idleOf(activity, done),
  }
}

export function useNoLine(): DownloadsLine {
  return { activity: IDLE, words: '', idle: true }
}
