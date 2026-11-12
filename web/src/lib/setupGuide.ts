import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from './api'
import { useAccess, usePrefs, useSetPref, useSetup, useTrackers, type SetupComponent, type SetupProgress } from './queries'
import { useProfile } from './profile'
import { readKey, writeKey } from './storage'

export const STEPS = [
  { id: 'essentials', title: 'Essentials', sub: 'What Yoru needs to play' },
  { id: 'sources', title: 'Sources', sub: 'Where episodes come from' },
  { id: 'account', title: 'Account', sub: 'Your list and your name' },
  { id: 'watching', title: 'Watching', sub: 'Sound, subtitles, quality' },
  { id: 'storage', title: 'Storage', sub: 'Where things go' },
  { id: 'computer', title: 'This computer', sub: 'Start-up, alerts, updates' },
] as const

export type StepId = (typeof STEPS)[number]['id']
export type Screen = 'welcome' | StepId | 'done'

export const SCREENS: readonly Screen[] = ['welcome', ...STEPS.map((s) => s.id), 'done']

export const screenOf = (value: string | null): Screen =>
  SCREENS.includes(value as Screen) ? (value as Screen) : 'welcome'

export const stepPath = (screen: Screen) => (screen === 'welcome' ? '/setup' : `/setup?step=${screen}`)

export const nextClass =
  'press inline-flex h-11 shrink-0 items-center gap-2.5 rounded-xl bg-accent-500 px-6 text-[15px] font-semibold whitespace-nowrap text-white transition-colors hover:bg-accent-600 focus:outline-hidden focus-visible:ring-2 focus-visible:ring-white focus-visible:ring-offset-2 focus-visible:ring-offset-base-950 disabled:opacity-50'

export const PARTS: Record<string, { title: string; short: string; text: string }> = {
  ffmpeg: { title: 'Player engine', short: 'Engine', text: 'Prepares every episode for the built-in player.' },
  flyhigh: { title: 'Smooth motion', short: 'Smooth motion', text: "Extra frames between the episode's own, made by your graphics card." },
  cloudflared: {
    title: 'Host watch-together rooms',
    short: 'Rooms',
    text: "Only for opening your own room so friends anywhere can join. Joining a friend's room needs nothing.",
  },
  mpv: { title: 'Desktop player', short: 'Desktop player', text: 'mpv, a separate player window. Only if you prefer it to the built-in one.' },
  anime4k: { title: 'Anime4K for the desktop player', short: 'Anime4K for mpv', text: 'The built-in player already has its own.' },
}

const PART_ORDER = ['ffmpeg', 'flyhigh', 'cloudflared', 'mpv', 'anime4k']
export const FIRST_PICKS = ['ffmpeg', 'flyhigh', 'cloudflared']

export const AUDIO = [
  { value: 'sub', title: 'Japanese, English subtitles', text: 'The original voices', short: 'Japanese' },
  { value: 'dub', title: 'English dub', text: 'Japanese when a show has no dub', short: 'English dub' },
  { value: 'either', title: 'Either', text: 'Whichever release is best', short: 'Either sound' },
] as const

export const PICTURE = [
  { value: 'best', title: 'Best there is', text: 'The best release, 1080p first', short: 'best picture' },
  { value: '720p', title: 'Up to 720p', text: 'Smaller files', short: 'up to 720p' },
  { value: '480p', title: 'Up to 480p', text: 'For a slow connection', short: 'up to 480p' },
] as const

export const TITLES = [
  { value: 'english', title: 'English' },
  { value: 'romaji', title: 'Romaji' },
] as const

export const AFTER = [
  { value: 'off', title: 'Keep it until space is needed', short: 'Kept until space is needed' },
  { value: 'keep2', title: 'Delete it once two more are watched', short: 'Deleted two episodes later' },
  { value: 'now', title: 'Delete it right away', short: 'Deleted once watched' },
] as const

export const BUDGETS_GB = [5, 10, 20, 40, 80]
const GB = 1024 ** 3

export interface StepNote {
  done: boolean
  sub: string
  value: string
  detail: string
  warn?: string
}

const PASSED_KEY = 'yoru.setup.passed'
const readPassed = () => (readKey(PASSED_KEY) ?? '').split(',').filter(Boolean)

const moving = (p?: SetupProgress) => !!p && p.stage !== 'done' && p.stage !== 'failed'
const pick = <T extends { value: string }>(list: readonly T[], value: string) => list.find((o) => o.value === value) ?? list[0]
const many = (n: number, one: string) => `${n} ${one}${n === 1 ? '' : 's'}`

export function useGuide() {
  const setup = useSetup({
    refetchInterval: (q) => ((q.state.data?.progress ?? []).some(moving) ? 1000 : false),
  })
  const access = useAccess()
  const host = access.data?.host === true
  const trackers = useTrackers().data?.trackers ?? []
  const prefs = usePrefs()
  const setPref = useSetPref()
  const profile = useProfile().data
  const startup = useQuery({
    queryKey: ['app-startup'],
    queryFn: () => api.get<{ on: boolean; tray: boolean }>('/api/app/startup'),
    enabled: host,
  })
  const [passed, setPassed] = useState(readPassed)
  const [chosen, setChosen] = useState<Record<string, string>>({})

  const values = { ...prefs.data?.effective, ...chosen }
  const value = (key: string) => values[key] ?? ''
  const on = (key: string) => values[key] === 'true'
  const pass = (id: StepId) => {
    if (passed.includes(id)) return
    const next = [...passed, id]
    writeKey(PASSED_KEY, next.join(','))
    setPassed(next)
  }

  const components = setup.data?.components ?? []
  const parts: SetupComponent[] = [
    ...PART_ORDER.flatMap((name) => components.filter((c) => c.name === name)),
    ...components.filter((c) => c.name !== 'engine' && !PART_ORDER.includes(c.name)),
  ]
  const progress = (name: string) => (setup.data?.progress ?? []).find((p) => p.component === name)
  const loading = parts.filter((c) => moving(progress(c.name))).length
  const installed = parts.filter((c) => c.present)
  const engine = parts.find((c) => c.name === 'ffmpeg')?.present ?? false

  const sites = setup.data?.indexers ?? 0
  const badSites = setup.data?.badIndexers?.length ?? 0
  const connected = trackers.filter((t) => t.connected)
  const names = connected.map((t) => t.name).join(' and ')
  const audio = pick(AUDIO, value('audio.prefer'))
  const picture = pick(PICTURE, value('quality.prefer') || 'best')
  const after = pick(AFTER, value('cache.autodelete') || 'off')
  const gb = Math.round((Number(value('cache.budget_bytes')) || 0) / GB)
  const atSignIn = startup.data?.on ?? false
  const extras = [
    value('window.on_close') === 'tray' && 'stays in the tray',
    on('notify.enabled') && 'alerts',
    on('update.auto') && 'updates itself',
  ].filter(Boolean) as string[]

  const notes: Record<StepId, StepNote> = {
    essentials: {
      done: engine,
      sub: loading > 0 ? `Downloading · ${many(loading, 'item')}` : engine ? 'Installed' : STEPS[0].sub,
      value: loading > 0 ? `${many(loading, 'download')} running` : `${installed.length} installed`,
      detail: installed.map((c) => PARTS[c.name]?.short ?? c.label).join(', ') || 'Nothing yet',
      warn: !engine && loading === 0 ? "The player engine isn't installed" : undefined,
    },
    sources: {
      done: sites > 0,
      sub: sites > 0 ? many(sites, 'site') : STEPS[1].sub,
      value: sites > 0 ? many(sites, 'site') : 'No sites yet',
      detail: sites > 0 ? 'Searched for every episode' : '',
      warn: sites === 0 ? 'Nothing can play without one' : badSites > 0 ? `${many(badSites, 'site')} can't be used` : undefined,
    },
    account: {
      done: connected.length > 0,
      sub: connected.length > 0 ? `${names} connected` : STEPS[2].sub,
      value: names || "Yoru's own list",
      detail: profile?.shown ? `You appear as ${profile.shown}` : connected.length > 0 ? 'Connected' : 'No account connected',
      warn: trackers.some((t) => t.reconnect) ? 'Needs you to sign in again' : undefined,
    },
    watching: {
      done: passed.includes('watching'),
      sub: passed.includes('watching') ? `${audio.short} · ${picture.short}` : STEPS[3].sub,
      value: audio.title,
      detail: picture.title,
    },
    storage: {
      done: passed.includes('storage'),
      sub: passed.includes('storage') && gb > 0 ? `${gb} GB` : STEPS[4].sub,
      value: gb > 0 ? `Up to ${gb} GB` : 'No limit set',
      detail: after.short,
    },
    computer: {
      done: passed.includes('computer'),
      sub: passed.includes('computer') ? (atSignIn ? 'Starts when you sign in' : 'Starts when you open it') : STEPS[5].sub,
      value: atSignIn ? 'Starts when you sign in' : 'Starts when you open it',
      detail: extras.length > 0 ? extras.join(', ').replace(/^./, (c) => c.toUpperCase()) : 'Exits when closed',
    },
  }

  return {
    pending: setup.isPending || access.isPending || prefs.isPending,
    error: setup.isError && !setup.data ? setup.error : prefs.isError && !prefs.data ? prefs.error : null,
    retry: () => {
      void setup.refetch()
      void prefs.refetch()
    },
    host,
    setup: setup.data,
    parts,
    progress,
    trackers,
    value,
    on,
    set: (key: string, next: string) => {
      setChosen((was) => ({ ...was, [key]: next }))
      setPref.mutate({ key, value: next })
    },
    notes,
    pass,
  }
}

export type Guide = ReturnType<typeof useGuide>
export const partMoving = moving
