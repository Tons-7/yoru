import { useEffect, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { api, ApiError, fetchAuthed, type DownloadFile, type Page } from '../lib/api'
import { downloadLabel, retryNote, sortDownloads } from '../lib/downloads'
import { bytes, cx, relativeTime, speed } from '../lib/format'
import {
  refreshDownloads,
  useAccess,
  useDownloadQueue,
  useDownloads,
  useBetterCopies,
  useDriveRoom,
  useReleaseHolds,
  useNow,
  usePrefs,
  useSetPref,
  useSpeedUnit,
  type Download,
  type DriveRoom,
  type Queued,
  type ReleaseHold,
  type BetterCopy,
} from '../lib/queries'
import { BetterCopyOffer } from '../components/BetterCopy'
import { BottomSheet } from '../components/BottomSheet'
import {
  CheckBox,
  DownloadIcon,
  FreeRow,
  iconButton,
  PanelCard,
  PauseGlyph,
  PlayGlyph,
  Poster,
  RetryGlyph,
  SelectBar,
  ShelfTools,
  StorageCard,
} from '../components/DownloadsBits'
import { MoreMenu, type MoreItem } from '../components/MoreMenu'
import { cannotSave, saveEpisodes, savedKey, sendWatched, useSaved, useWatched } from '../lib/offline'
import {
  Badge,
  Button,
  Empty,
  ErrorState,
  FilterToggle,
  LinkButton,
  ConfirmInline,
  PageHeader,
  Pager,
  ProgressBar,
  RefreshFailed,
  Segmented,
  Select,
  Skeleton,
  TextInput,
} from '../components/ui'
import { useClampPage, useDebounced } from '../lib/hooks'
import { buttonClass } from '../lib/ui'
import { toast } from '../lib/toast'
import { ReleasePicker } from '../components/ReleasePicker'
import {
  aboutLeft,
  episodeCount,
  lastSide,
  LOW_BYTES,
  LOW_SHARE,
  ordinal,
  rememberSide,
  sortShelf,
  useDeviceWord,
  useSort,
  type Side,
} from '../lib/shelf'
import { removeWithUndo, useHiddenKeys, type Removal } from '../lib/undo'
import { DeviceFreeUp, DeviceList, DeviceSettings, DeviceStorage } from './Saved'
import { NotificationsView } from '../components/NotificationsView'

export function Notifications() {
  return <NotificationsView />
}

function FolderIcon() {
  return (
    <svg viewBox="0 0 24 24" className="size-6" aria-hidden>
      <path
        d="M4 7.5A1.5 1.5 0 0 1 5.5 6h3.4a1.5 1.5 0 0 1 1.2.6l.9 1.2h7.5A1.5 1.5 0 0 1 20 9.3v8.2a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 17.5v-10Z"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function KeepGlyph({ filled }: { filled: boolean }) {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M9 3.5h6l-1 6 3.5 3.5v1.5h-11V13L10 9.5l-1-6Z" fill={filled ? 'currentColor' : 'none'} {...stroke} />
      <path d="M12 14.5V21" fill="none" {...stroke} />
    </svg>
  )
}

const stroke = { stroke: 'currentColor', strokeWidth: 2, strokeLinecap: 'round', strokeLinejoin: 'round' } as const

function FrontGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M5 5h14M12 20V9m-5 5 5-5 5 5" fill="none" {...stroke} />
    </svg>
  )
}

function SwapGlyph() {
  return (
    <svg viewBox="0 0 24 24" className="size-4" aria-hidden>
      <path d="M4 8h13m0 0-3-3m3 3-3 3M20 16H7m0 0 3-3m-3 3 3 3" fill="none" {...stroke} />
    </svg>
  )
}

function PackEpisodes({ hash }: { hash: string }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const files = useQuery({
    enabled: open,
    queryKey: ['download-files', hash],
    queryFn: () => api.get<{ items: DownloadFile[] }>(`/api/downloads/${hash}/files`),
  })
  const keep = useMutation({
    mutationFn: (f: DownloadFile) =>
      api.post(`/api/downloads/${hash}/files/${f.fileIndex}/${f.kept ? 'unkeep' : 'keep'}`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['download-files', hash] })
      refreshDownloads(qc)
    },
  })

  return (
    <div className="mt-2">
      <button
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="tap relative py-1 text-xs text-base-400 hover:text-base-200"
      >
        {open ? 'Hide episodes' : 'Keep episodes one by one…'}
      </button>
      {open && files.isPending && <Skeleton className="mt-1 h-16 w-full" />}
      {open && files.isError && (
        <p className="mt-1 flex flex-wrap items-center gap-2 text-xs text-red-300">
          {(files.error as Error).message}
          <button onClick={() => void files.refetch()} className="text-accent-300 hover:underline">
            Try again
          </button>
        </p>
      )}
      {open && files.isSuccess && (
        <ul className="mt-1 grid gap-1 sm:grid-cols-2">
          {(files.data?.items ?? []).map((f) => (
            <li
              key={f.fileIndex}
              className="flex items-center justify-between rounded-md bg-base-800 px-2 py-1 text-xs"
            >
              <span className="text-base-300">
                Episode {f.epKey}
                {!f.complete && <span className="text-base-500"> · downloading</span>}
              </span>
              <button
                onClick={() => keep.mutate(f)}
                disabled={keep.isPending}
                aria-pressed={f.kept}
                aria-label="Keep"
                title={f.kept ? 'Kept. Click to stop keeping it.' : 'Keep: stays until you remove it.'}
                className={keepButton(f.kept)}
              >
                <KeepGlyph filled={f.kept} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

const pcKey = (hash: string) => `pc:${hash}`
const episodesIn = (ds: Download[]) => ds.reduce((n, d) => n + Math.max(d.episodes.length, 1), 0)
const sizeOf = (ds: Download[]) => ds.reduce((n, d) => n + d.bytesOnDisk, 0)

interface CacheRoom extends DriveRoom {
  totalBytes?: number
}

function setKept(qc: ReturnType<typeof useQueryClient>, infoHash: string, kept: boolean) {
  qc.setQueryData<{ items: Download[]; count: number }>(['downloads'], (old) =>
    old
      ? { ...old, items: old.items.map((d) => (d.infoHash === infoHash ? { ...d, kept } : d)) }
      : old,
  )
}

function useVisibleDownloads() {
  const downloads = useDownloads()
  const queue = useDownloadQueue()
  const hidden = useHiddenKeys()
  const items = (downloads.data?.items ?? []).filter((d) => !hidden.has(pcKey(d.infoHash)))
  return { downloads, queue, items, sections: sortDownloads(items, queue.data?.items ?? []) }
}

const removedWords = (ds: Download[]) =>
  ds.length === 1 ? `Removed ${downloadLabel(ds[0])}` : `Removed ${episodeCount(episodesIn(ds))}`

function useRemoveLater() {
  const qc = useQueryClient()
  return (ds: Download[]) => {
    let left = ds.filter((d) => !d.pinned)
    if (left.length === 0) {
      if (ds.length > 0) {
        toast(ds.length === 1 ? `Can't remove ${downloadLabel(ds[0])} while it plays` : "Can't remove these while they play", 'info')
      }
      return
    }
    const removal: Removal = {
      keys: left.map((d) => pcKey(d.infoHash)),
      message: removedWords(left),
      failed: "Couldn't delete",
      run: async () => {
        try {
          const sent = await Promise.allSettled(left.map((d) => api.del(`/api/downloads/${d.infoHash}`)))
          const failures = sent.flatMap((r, i) =>
            r.status === 'rejected' && !(r.reason instanceof ApiError && r.reason.status === 404)
              ? [{ d: left[i], err: r.reason as unknown }]
              : [],
          )
          if (failures.length === 0) return
          left = failures.map((f) => f.d)
          removal.keys = left.map((d) => pcKey(d.infoHash))
          removal.message = removedWords(left)
          throw failures[0].err
        } finally {
          await qc.invalidateQueries({ queryKey: ['downloads'] })
          refreshDownloads(qc)
        }
      },
    }
    removeWithUndo(removal)
  }
}

export function Downloads({ phone = false, offline = false }: { phone?: boolean; offline?: boolean }) {
  return offline ? <OfflineDownloads /> : <OnlineDownloads phone={phone} />
}

function OnlineDownloads({ phone }: { phone: boolean }) {
  const access = useAccess()
  const here = useSaved()
  const word = useDeviceWord()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const { sections } = useVisibleDownloads()
  const hidden = useHiddenKeys()
  const host = access.data?.host === true
  const asked = phone ? 'phone' : params.get('on')
  const chosen = asked === 'phone' || asked === 'pc' ? asked : null
  const phoneCount =
    here.saved.filter((e) => !hidden.has(`saved:${e.key}`)).length +
    here.jobs.filter((j) => !hidden.has(`saved:${savedKey(j.animeId, j.episode)}`)).length

  if (!access.data && !access.isError) return <Skeleton className="mx-auto h-64 w-full max-w-[1120px]" />
  if (!host && !chosen && !here.loaded) return <Skeleton className="mx-auto h-64 w-full max-w-[1120px]" />

  const side: Side = host
    ? 'pc'
    : (chosen ?? (phoneCount > 0 ? 'phone' : (lastSide() ?? (cannotSave() ? 'pc' : 'phone'))))
  const choose = (next: Side) => {
    rememberSide(next)
    if (phone) navigate(next === 'pc' ? '/downloads?on=pc' : '/saved', { replace: true })
    else setParams({ on: next }, { replace: true })
  }

  return (
    <div className="mx-auto w-full max-w-[1120px] space-y-4">
      <PageHeader
        title="Downloads"
        meta={side === 'pc' ? <PcMeta /> : undefined}
        actions={side === 'pc' ? <PcActions /> : undefined}
      />
      {!host && (
        <SideSwitch
          side={side}
          onChange={choose}
          word={word}
          phoneCount={phoneCount}
          pcCount={episodesIn(sections.finished)}
        />
      )}
      {host && phone && (
        <p className="surface p-4 text-sm text-base-300">
          This is the PC Yoru runs on, so every download is here already. Saving is for phones and laptops, to watch
          away from it.
        </p>
      )}
      {side === 'pc' ? (
        <Columns
          list={<PcList host={host} />}
          top={<PcStorage host={host} />}
          rest={
            <>
              <PcSettings />
              <PcFreeUp />
            </>
          }
        />
      ) : (
        <Columns
          list={<DeviceList />}
          top={<DeviceStorage />}
          rest={
            <>
              <DeviceSettings />
              <DeviceFreeUp />
            </>
          }
        />
      )}
    </div>
  )
}

function SideSwitch({
  side,
  onChange,
  word,
  phoneCount,
  pcCount,
}: {
  side: Side
  onChange: (side: Side) => void
  word: 'phone' | 'device'
  phoneCount: number
  pcCount?: number
}) {
  return (
    <Segmented<Side>
      options={[
        { value: 'phone', label: word === 'phone' ? 'This phone' : 'This device', count: phoneCount },
        { value: 'pc', label: 'On the PC', count: pcCount },
      ]}
      value={side}
      onChange={onChange}
      full
      className="sm:w-[22.5rem]"
    />
  )
}

function Columns({ list, top, rest }: { list: ReactNode; top: ReactNode; rest: ReactNode }) {
  return (
    <div className="flex flex-col gap-4 lg:grid lg:grid-cols-[minmax(0,1fr)_20rem] lg:items-start lg:gap-8">
      <div className="order-2 min-w-0 lg:order-none">{list}</div>
      <div className="contents lg:sticky lg:top-22 lg:block lg:max-h-[calc(100dvh-6.5rem)] lg:space-y-4 lg:overflow-y-auto scrollbar-thin">
        <div className="order-1 empty:hidden lg:order-none">{top}</div>
        <div className="order-3 space-y-4 empty:hidden lg:order-none">{rest}</div>
      </div>
    </div>
  )
}

function useReachable() {
  const [back, setBack] = useState(false)
  const [checking, setChecking] = useState(false)
  const check = async () => {
    setChecking(true)
    const ok = await fetchAuthed('/api/access', { cache: 'no-store', signal: AbortSignal.timeout(4000) }).then(
      (r) => r.ok,
      () => false,
    )
    setChecking(false)
    setBack(ok)
    if (ok) void sendWatched()
  }
  useEffect(() => {
    const first = window.setTimeout(() => void check(), 0)
    const id = window.setInterval(() => void check(), 15_000)
    return () => {
      window.clearTimeout(first)
      window.clearInterval(id)
    }
  }, [])
  return { back, checking, check: () => void check() }
}

function OfflineDownloads() {
  const [side, setSide] = useState<Side>('phone')
  const here = useSaved()
  const watched = useWatched()
  const word = useDeviceWord()
  const reach = useReachable()
  const unsent = Object.values(watched).filter((w) => w.dirty).length

  return (
    <div className="mx-auto w-full max-w-[1120px] space-y-4 px-4 pt-6 pb-16 sm:px-6">
      <PageHeader title="Downloads" />
      <div
        role="status"
        className="surface flex min-h-10 flex-wrap items-center justify-between gap-x-3 gap-y-2 py-2 pr-2 pl-4"
      >
        <div className="min-w-0 text-sm">
          <p className="text-base-100">{reach.back ? 'Back online' : "Can't reach the PC. Saved episodes still play."}</p>
          {(reach.back || unsent > 0) && (
            <p className="text-[13px] text-base-400">
              {reach.back ? 'Everything else is back.' : 'What you watch is sent to Yoru once it answers.'}
            </p>
          )}
        </div>
        {reach.back ? (
          <Button variant="primary" onClick={() => window.location.assign('/')}>
            Open Yoru
          </Button>
        ) : (
          <Button onClick={reach.check} disabled={reach.checking}>
            {reach.checking ? 'Trying…' : 'Retry'}
          </Button>
        )}
      </div>
      <SideSwitch side={side} onChange={setSide} word={word} phoneCount={here.saved.length + here.jobs.length} />
      {side === 'pc' ? (
        <section className="surface flex flex-col items-center gap-4 px-6 py-8 text-center">
          <p className="max-w-sm text-[15px] text-balance text-base-300">
            The PC is off or out of reach. Its list comes back when it's on.
          </p>
          <Button onClick={reach.check} disabled={reach.checking}>
            {reach.checking ? 'Trying…' : 'Retry'}
          </Button>
        </section>
      ) : (
        <Columns
          list={<DeviceList offline />}
          top={<DeviceStorage />}
          rest={
            <>
              <DeviceSettings />
              <DeviceFreeUp />
            </>
          }
        />
      )}
    </div>
  )
}

function PcMeta() {
  const { sections: s } = useVisibleDownloads()
  const busy = s.active.length + s.searching.length
  const waiting = s.held.length + s.waitingQueue.length
  const words = [busy > 0 && `${busy} downloading`, waiting > 0 && `${waiting} waiting`, s.failed.length > 0 && `${s.failed.length} failed`]
    .filter(Boolean)
    .join(' · ')
  return words ? <span className="tabular-nums">{words}</span> : null
}

function PcActions() {
  const qc = useQueryClient()
  const { queue, sections: s } = useVisibleDownloads()
  const paused = queue.data?.paused ?? false
  const pauseAll = useMutation({
    mutationFn: (pause: boolean) => api.post(`/api/download/queue/${pause ? 'pause' : 'resume'}`),
    onSuccess: () => refreshDownloads(qc),
  })
  if (!paused && s.active.length + s.searching.length + s.held.length + s.waitingQueue.length === 0) return null
  return (
    <Button
      onClick={() => pauseAll.mutate(!paused)}
      disabled={pauseAll.isPending}
      title={
        paused
          ? 'Start downloading again, one at a time'
          : 'Stop every download. Pausing a single one lets the next in line start.'
      }
    >
      {paused ? 'Resume downloads' : 'Pause all'}
    </Button>
  )
}

function tidyRule(mode: string) {
  if (mode === 'now') return 'Deleted right after watching'
  if (mode === 'keep2') return 'Deleted once two more are watched, or when space runs low'
  return 'Cleared when space runs low'
}

function PcList({ host }: { host: boolean }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const word = useDeviceWord()
  const [openShows, setOpenShows] = useState<ReadonlySet<string>>(new Set())
  const toggleShow = (key: string) =>
    setOpenShows((open) => {
      const next = new Set(open)
      if (!next.delete(key)) next.add(key)
      return next
    })
  const [sort, setSort] = useSort()
  const [selecting, setSelecting] = useState(false)
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set())
  const [confirmPicked, setConfirmPicked] = useState(false)

  const { downloads, queue, items: visible, sections } = useVisibleDownloads()
  const { data, isPending, isError, error, refetch } = downloads
  const access = useAccess()
  const here = useSaved()
  const elsewhere = !!access.data && !access.data.host
  const onDevice = new Set(here.saved.map((e) => e.key))
  const savingHere = new Set(here.jobs.map((j) => savedKey(j.animeId, j.episode)))
  const holds = useReleaseHolds()
  const better = useBetterCopies()
  const removeLater = useRemoveLater()
  const takeNow = useMutation({
    mutationFn: (h: ReleaseHold) => api.post(`/api/releases/waiting/${h.animeId}/${h.episode}`),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: ['release-holds'] })
      refreshDownloads(qc)
    },
  })
  const prefs = usePrefs()
  const mode = prefs.data?.effective['cache.autodelete'] ?? 'off'
  const keptGo = mode !== 'off' && prefs.data?.effective['cache.autodelete_downloads'] === 'true'

  const done = () => refreshDownloads(qc)

  const waitingKeys = new Set(
    (queue.data?.items ?? [])
      .filter((q) => q.state === 'pending')
      .map((q) => `${q.animeId}-${q.epKey}`),
  )

  const dequeue = useMutation({
    mutationFn: (q: Queued) =>
      api.del(`/api/download/all/${q.animeId}?episode=${encodeURIComponent(q.epKey)}`),
    onSuccess: done,
  })

  const [picking, setPicking] = useState<{ animeId: number; episode: number; current?: string } | null>(null)
  const changeRelease = useMutation({
    mutationFn: (pick: { animeId: number; episode: number; infoHash: string; title: string }) =>
      api.post('/api/download/release', pick),
    onSuccess: done,
  })

  const requeue = useMutation({
    mutationFn: (q: Queued) =>
      api.post('/api/download', { animeId: q.animeId, episode: q.episode }),
    onSuccess: done,
  })

  const now = useMutation({
    mutationFn: (q: Queued) =>
      api.post('/api/download/queue/now', { animeId: q.animeId, epKey: q.epKey }),
    onSuccess: done,
  })

  const queuePaused = queue.data?.paused ?? false
  const pauseAll = useMutation({
    mutationFn: (paused: boolean) => api.post(`/api/download/queue/${paused ? 'pause' : 'resume'}`),
    onSuccess: done,
  })
  const proceed = useMutation({
    mutationFn: () => api.post('/api/download/queue/proceed'),
    onSuccess: done,
  })

  const remove = useMutation({
    meta: { inline: true },
    mutationFn: async (hashes: string[]) => {
      const sent = await Promise.allSettled(hashes.map((h) => api.del(`/api/downloads/${h}`)))
      const failed = sent.flatMap((r, i) =>
        r.status === 'rejected' && !(r.reason instanceof ApiError && r.reason.status === 404)
          ? [{ hash: hashes[i], err: r.reason as unknown }]
          : [],
      )
      if (failed.length === 0) return
      const first = failed[0].err
      throw Object.assign(new Error(first instanceof Error ? first.message : String(first)), {
        hashes: failed.map((f) => f.hash),
      })
    },
    onError: (err, hashes) => {
      const left = (err as { hashes?: string[] }).hashes ?? hashes
      toast(`Couldn't delete: ${(err as Error).message}`, 'error', {
        label: 'Try again',
        run: () => remove.mutate(left),
      })
    },
    onSettled: () => {
      setConfirmRemove(null)
      done()
    },
  })
  const [confirmRemove, setConfirmRemove] = useState<string | null>(null)

  const pause = useMutation({
    mutationFn: (hash: string) => api.post(`/api/downloads/${hash}/pause`),
    onSuccess: done,
  })

  const resume = useMutation({
    mutationFn: (hash: string) => api.post(`/api/downloads/${hash}/resume`),
    onSuccess: done,
  })
  const keep = useMutation({
    meta: { inline: true },
    mutationFn: (ds: Download[]) =>
      Promise.all(
        ds.map((d) =>
          api.post<{ infoHash: string; kept: boolean }>(`/api/downloads/${d.infoHash}/${d.kept ? 'unkeep' : 'keep'}`),
        ),
      ),
    onMutate: async (ds: Download[]) => {
      await qc.cancelQueries({ queryKey: ['downloads'] })
      const previous = qc.getQueryData<{ items: Download[]; count: number }>(['downloads'])
      for (const d of ds) setKept(qc, d.infoHash, !d.kept)
      return { previous }
    },
    onError: (err, ds, context) => {
      if (context?.previous) qc.setQueryData(['downloads'], context.previous)
      toast(`Couldn't change what is kept: ${(err as Error).message}`, 'error', {
        label: 'Try again',
        run: () => keep.mutate(ds),
      })
    },
    onSuccess: (res) => res.forEach((r) => setKept(qc, r.infoHash, r.kept)),
    onSettled: done,
  })
  const keepNow = (ds: Download[]) => {
    if (ds.length === 0) return
    keep.mutate(ds)
    const one = ds.length === 1 ? ds[0] : null
    const what = one ? downloadLabel(one) : episodeCount(episodesIn(ds))
    if (ds.some((d) => d.kept)) {
      toast(`Stopped keeping ${what}. Auto tidy can delete ${one ? 'it' : 'them'} once watched.`, 'info', {
        label: 'Undo',
        run: () => keep.mutate(ds.map((d) => ({ ...d, kept: false }))),
      })
      return
    }
    toast(
      one ? `Kept ${what}. It stays until you remove it.` : `Kept ${what}. They stay until you remove them.`,
      'info',
      { label: 'Undo', run: () => keep.mutate(ds.map((d) => ({ ...d, kept: true }))) },
    )
  }

  if (isError && !data) return <ErrorState error={error} retry={() => refetch()} />

  const queued = queue.data?.items ?? []
  const holdItems = holds.data?.items ?? []
  const offers = better.data?.items ?? []

  const { active, held, finished, searching, waitingQueue, failed } = sections
  const waiting = held.length + waitingQueue.length
  const kept = finished.filter((d) => d.kept)
  const passing = finished.filter((d) => !d.kept)
  const order = new Map(visible.map((d, i) => [d.infoHash, i]))
  const shelf = (list: Download[], section: string): Row[] =>
    sortShelf(byShow(list), sort, (x) => {
      const ds = 'items' in x ? x.items : [x]
      return {
        recent: -Math.min(...ds.map((d) => order.get(d.infoHash) ?? 0)),
        size: sizeOf(ds),
        name: (('items' in x ? x.title : x.title) ?? ('items' in x ? '' : x.name)).toLowerCase(),
      }
    }).map((x) => ('items' in x ? { show: x, section } : { d: x }))

  const rows: Row[] = [
    ...(active.length + searching.length > 0 ? [{ group: 'Downloading', meta: 'one at a time' }] : []),
    ...searching.map((q) => ({ q })),
    ...active.map((d) => ({ d })),
    ...(waiting > 0 ? [{ group: 'Waiting', meta: String(waiting) }] : []),
    ...held.map((d, i) => ({ d, position: i + 1 })),
    ...waitingQueue.map((q, i) => ({ q, position: held.length + i + 1 })),
    ...(holdItems.length > 0 ? [{ group: 'Held for a better release', meta: String(holdItems.length) }] : []),
    ...holdItems.map((hold) => ({ hold })),
    ...(failed.length > 0 ? [{ group: 'Failed' }] : []),
    ...failed.map((q) => ({ q })),
    ...(offers.length > 0 ? [{ group: 'Better copies', meta: "SeaDex's best for what you kept" }] : []),
    ...offers.map((offer) => ({ offer })),
    ...(finished.length > 0 ? [{ tools: true as const }] : []),
    ...(kept.length > 0
      ? [
          {
            group: 'Kept',
            meta: `${episodeCount(episodesIn(kept))} · ${bytes(sizeOf(kept))}`,
            note: keptGo ? 'Kept episodes are deleted too, once watched.' : 'They stay until you remove them.',
            change: keptGo ? '/settings?tab=Storage' : undefined,
          },
        ]
      : []),
    ...shelf(kept, 'kept'),
    ...(passing.length > 0
      ? [
          {
            group: 'Not kept',
            meta: `${episodeCount(episodesIn(passing))} · ${bytes(sizeOf(passing))}`,
            note: tidyRule(mode),
          },
        ]
      : []),
    ...shelf(passing, 'passing'),
  ]

  const choosable = finished.filter((d) => !d.pinned)
  const chosen = choosable.filter((d) => picked.has(d.infoHash))
  const toggle = (ds: Download[], on: boolean) =>
    setPicked((was) => {
      const next = new Set(was)
      for (const d of ds) {
        if (on) next.add(d.infoHash)
        else next.delete(d.infoHash)
      }
      return next
    })
  const stopSelecting = () => {
    setSelecting(false)
    setPicked(new Set())
    setConfirmPicked(false)
  }
  const removeChosen = () => {
    const shows = byShow(finished).filter((x): x is Show => 'items' in x)
    const wholeShow = shows.some((s) => s.items.every((d) => d.pinned || picked.has(d.infoHash)))
    if (!confirmPicked && (episodesIn(chosen) > 5 || wholeShow)) return setConfirmPicked(true)
    removeLater(chosen)
    stopSelecting()
  }

  const actions: RowActions = {
    host,
    word,
    selecting: selecting ? { picked, toggle } : undefined,
    queueHeld: (d) => d.episodes.some((e) => waitingKeys.has(`${d.animeId}-${e}`)),
    changeRelease: (animeId, episode, current) => setPicking({ animeId, episode, current }),
    chosenFor: (d) => queued.find((q) => q.animeId === d.animeId && d.episodes.includes(q.epKey) && !!q.release),
    confirming: confirmRemove,
    confirm: setConfirmRemove,
    removing: remove.isPending,
    toggling: pause.isPending || resume.isPending,
    keep: keepNow,
    remove: (hashes) => remove.mutate(hashes),
    removeLater,
    pauseResume: (d) => (d.paused ? resume : pause).mutate(d.infoHash),
    toDevice: elsewhere
      ? {
          state: (d) => {
            const keys = d.episodes.map((e) => savedKey(d.animeId ?? 0, Number(e)))
            if (keys.every((k) => onDevice.has(k))) return 'saved'
            return keys.some((k) => savingHere.has(k)) ? 'saving' : 'none'
          },
          save: (ds) => {
            if (cannotSave()) navigate('/saved')
            else
              saveEpisodes(
                ds.flatMap((d) => d.episodes.map((e) => ({ animeId: d.animeId!, episode: Number(e) }))).filter((e) => e.episode > 0),
              )
          },
        }
      : undefined,
  }
  const notice = cx(card, 'flex items-center justify-between gap-3 px-4 py-3 text-sm text-base-200')

  return (
    <div className="space-y-4">
      {isError && data && <RefreshFailed retry={() => refetch()} />}

      {!queuePaused && queue.data?.short && (
        <p role="status" className={notice}>
          {`Downloads wait for room: ${host ? queue.data.short.drive : 'the PC'} has ${bytes(queue.data.short.freeBytes)} free, and they need ${bytes(queue.data.short.needBytes)}. They carry on once there's space.`}
          <Link to="/settings?tab=Storage" className="tap relative shrink-0 text-[13px] font-medium text-accent-300 hover:underline">
            Free space
          </Link>
        </p>
      )}
      {queuePaused && (
        <p role="status" className={notice}>
          Downloads are paused. Nothing starts until you resume.
          <Button size="sm" variant="primary" onClick={() => pauseAll.mutate(false)} disabled={pauseAll.isPending}>
            Resume
          </Button>
        </p>
      )}
      {!queuePaused && queue.data?.watching && waitingQueue.length > 0 && (
        <p role="status" className={notice}>
          Waiting until the episode you're watching has downloaded, so it gets the whole connection.
          <span className="flex shrink-0 items-center gap-1">
            <LinkButton to={`/settings?q=${encodeURIComponent('download queue')}`} size="sm" variant="ghost">
              Change
            </LinkButton>
            <Button size="sm" variant="primary" onClick={() => proceed.mutate()} disabled={proceed.isPending}>
              Download anyway
            </Button>
          </span>
        </p>
      )}

      {isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : rows.length === 0 ? (
        <section className="surface flex flex-col items-center px-6 py-8 text-center">
          <span className="mb-4 grid size-14 place-items-center rounded-2xl bg-base-800 text-base-300">
            <DownloadIcon />
          </span>
          <p className="text-[17px] font-semibold text-base-100">Nothing downloaded</p>
          <p className="mt-1 max-w-sm text-[15px] text-balance text-base-400">
            {keptGo
              ? 'Episodes you watch stay here for a while. Downloads are deleted once watched too.'
              : 'Episodes you watch stay here for a while. Episodes you download stay until you remove them.'}
          </p>
          <LinkButton to="/schedule?view=out" variant="primary" className="mt-5">
            Find something to watch
          </LinkButton>
          <Link to="/library" className="tap relative mt-3 text-sm font-medium text-accent-300 hover:underline">
            Open Library
          </Link>
        </section>
      ) : (
        <ul className="space-y-2">
          {rows.map((r) => {
            if ('tools' in r)
              return (
                <li key="tools" className="pt-3 first:pt-0">
                  <ShelfTools
                    sort={sort}
                    onSort={setSort}
                    selecting={selecting}
                    onSelecting={(on) => (on ? setSelecting(true) : stopSelecting())}
                  />
                </li>
              )
            if ('group' in r) return <Group key={r.group} label={r.group} meta={r.meta} note={r.note} change={r.change} />
            if ('offer' in r)
              return (
                <li key={`offer-${r.offer.animeId}`} className={cx(card, 'flex items-center gap-3 p-3')}>
                  <Gutter />
                  <Poster src={r.offer.cover} />
                  <BetterCopyOffer offer={r.offer} className="min-w-0 flex-1" />
                </li>
              )
            if ('hold' in r)
              return (
                <HoldRow
                  key={`hold-${r.hold.animeId}-${r.hold.episode}`}
                  hold={r.hold}
                  pending={takeNow.isPending}
                  onNow={() => takeNow.mutate(r.hold)}
                />
              )
            if ('q' in r)
              return (
                <QueueRow
                  key={`q-${r.q.animeId}-${r.q.epKey}`}
                  q={r.q}
                  position={r.position}
                  actions={actions}
                  onNow={() => now.mutate(r.q)}
                  onRetry={() => requeue.mutate(r.q)}
                  onRemove={() => dequeue.mutate(r.q)}
                  onChangeRelease={() => actions.changeRelease(r.q.animeId, r.q.episode, r.q.infoHash)}
                />
              )
            if ('show' in r) {
              const key = `${r.section}-${r.show.animeId}`
              return (
                <ShowRows
                  key={key}
                  show={r.show}
                  confirmKey={`show-${key}`}
                  open={openShows.has(key)}
                  onToggle={() => toggleShow(key)}
                  actions={actions}
                />
              )
            }
            return <DownloadRow key={r.d.infoHash} d={r.d} position={r.position} actions={actions} />
          })}
        </ul>
      )}
      {changeRelease.isError && <p className="text-[13px] text-red-300">{(changeRelease.error as Error).message}</p>}

      {selecting && (
        <SelectBar
          count={episodesIn(chosen)}
          size={sizeOf(chosen)}
          confirming={confirmPicked}
          onRemove={removeChosen}
          onConfirm={() => {
            removeLater(chosen)
            stopSelecting()
          }}
          onKeep={() => setConfirmPicked(false)}
          onCancel={stopSelecting}
        />
      )}

      {picking && (
        <ReleasePicker
          animeId={picking.animeId}
          episode={picking.episode}
          current={picking.current}
          purpose="download"
          onClose={() => setPicking(null)}
          onPick={(infoHash, title) => {
            changeRelease.mutate({ animeId: picking.animeId, episode: picking.episode, infoHash, title })
            setPicking(null)
          }}
        />
      )}
    </div>
  )
}

type Row =
  | { tools: true }
  | { group: string; meta?: string; note?: string; change?: string }
  | { d: Download; position?: number }
  | { q: Queued; position?: number }
  | { show: Show; section: string }
  | { hold: ReleaseHold }
  | { offer: BetterCopy }

function HoldRow({ hold, pending, onNow }: { hold: ReleaseHold; pending: boolean; onNow: () => void }) {
  const now = useNow()
  return (
    <li className={cx(card, 'p-3')}>
      <div className="flex items-center gap-3 max-sm:flex-wrap">
        <Gutter />
        <Poster src={hold.cover} />
        <div className="min-w-0 flex-1">
          <Link to={`/anime/${hold.animeId}`} className="block truncate text-sm font-medium text-base-100 hover:underline">
            {hold.title || `Anime ${hold.animeId}`}
            <span className="ml-1.5 font-normal text-base-400">episode {hold.episode}</span>
          </Link>
          <p className="text-[13px] text-base-400">
            Waiting for {hold.wants} · takes the best there is{' '}
            {hold.until * 1000 > now ? relativeTime(hold.until) : 'at the next check'}
          </p>
        </div>
        <div className={rail}>
          <button
            onClick={onNow}
            disabled={pending}
            aria-label={`Download episode ${hold.episode} now`}
            title="Download now: take the best release there is"
            className={iconButton}
          >
            <FrontGlyph />
          </button>
        </div>
      </div>
    </li>
  )
}

interface Show {
  animeId: number
  title?: string
  cover?: string
  items: Download[]
}

function byShow(finished: Download[]): (Download | Show)[] {
  const shows = new Map<number, Download[]>()
  for (const d of finished) {
    if (d.animeId) shows.set(d.animeId, [...(shows.get(d.animeId) ?? []), d])
  }
  const placed = new Set<number>()
  return finished.flatMap((d): (Download | Show)[] => {
    const items = d.animeId ? shows.get(d.animeId)! : [d]
    if (!d.animeId || items.length < 2) return [d]
    if (placed.has(d.animeId)) return []
    placed.add(d.animeId)
    const first = (x: Download) => Number(x.episodes[0]) || 0
    return [{ animeId: d.animeId, title: d.title, cover: d.cover, items: [...items].sort((a, b) => first(a) - first(b)) }]
  })
}

interface RowActions {
  host: boolean
  word: 'phone' | 'device'
  selecting?: { picked: ReadonlySet<string>; toggle: (ds: Download[], on: boolean) => void }
  queueHeld: (d: Download) => boolean
  changeRelease: (animeId: number, episode: number, current?: string) => void
  chosenFor: (d: Download) => Queued | undefined
  confirming: string | null
  confirm: (key: string | null) => void
  removing: boolean
  toggling: boolean
  keep: (ds: Download[]) => void
  remove: (hashes: string[]) => void
  removeLater: (ds: Download[]) => void
  pauseResume: (d: Download) => void
  toDevice?: { state: (d: Download) => 'none' | 'saving' | 'saved'; save: (ds: Download[]) => void }
}

function ConfirmDelete({ label, actions, onDelete }: { label: string; actions: RowActions; onDelete: () => void }) {
  return (
    <span className="flex flex-wrap items-center justify-end gap-1">
      <ConfirmInline
        label={label}
        onConfirm={onDelete}
        onCancel={() => actions.confirm(null)}
        pending={actions.removing}
        size="sm"
      />
    </span>
  )
}

const keepButton = (kept: boolean) =>
  cx(iconButton, kept && 'bg-accent-500/15 text-accent-300 hover:bg-accent-500/25 hover:text-accent-200')

const deleteButton =
  'tap relative grid size-7 place-items-center rounded-md text-base-500 transition-colors hover:bg-base-800 hover:text-red-300 pointer-coarse:size-11'

const rail = 'flex shrink-0 items-center gap-1.5 max-sm:w-full max-sm:justify-end'

const inlineRail = 'flex shrink-0 items-center gap-1.5 self-center'

const card = 'rounded-card bg-base-900'

const menuClass = iconButton

function deviceItem(ds: Download[], actions: RowActions): MoreItem | null {
  const to = actions.toDevice
  const ready = ds.filter((d) => d.percent >= 100 && !!d.animeId && d.episodes.length > 0)
  if (!to || ready.length === 0) return null
  const states = ready.map((d) => to.state(d))
  if (states.every((s) => s === 'saved')) return { label: `On this ${actions.word}`, to: '/saved' }
  if (states.some((s) => s === 'saving')) return { label: `Saving to this ${actions.word}`, to: '/saved' }
  const all = episodesIn(ready) > 1 ? ` all ${episodesIn(ready)}` : ''
  return { label: `Save${all} to this ${actions.word}`, run: () => to.save(ready.filter((d) => to.state(d) === 'none')) }
}

function ShowRows({
  show,
  confirmKey,
  open,
  onToggle,
  actions,
}: {
  show: Show
  confirmKey: string
  open: boolean
  onToggle: () => void
  actions: RowActions
}) {
  const name = show.title ?? `Anime ${show.animeId}`
  const episodes = episodesIn(show.items)
  const size = sizeOf(show.items)
  const allKept = show.items.every((d) => d.kept)
  const removable = show.items.filter((d) => !d.pinned)
  const select = actions.selecting
  const savedHere = actions.toDevice && show.items.every((d) => actions.toDevice!.state(d) === 'saved')
  const items = [
    deviceItem(show.items, actions),
    {
      label: allKept ? 'Stop keeping all' : 'Keep all',
      run: () => actions.keep(show.items.filter((d) => d.kept === allKept)),
    },
    removable.length > 0 && {
      label: `Remove ${episodeCount(episodesIn(removable))}`,
      danger: true,
      divider: true,
      run: () => actions.confirm(confirmKey),
    },
  ].filter((item): item is MoreItem => !!item)

  const playing = show.items.some((d) => d.pinned)
  const roomy = playing || !!savedHere || actions.confirming === confirmKey

  return (
    <li className={cx(card, 'overflow-hidden')}>
      <div className={cx('flex items-center gap-3 p-3', roomy && 'max-sm:flex-wrap')}>
        {select ? (
          <CheckBox
            checked={removable.length > 0 && removable.every((d) => select.picked.has(d.infoHash))}
            onChange={(on) => select.toggle(removable, on)}
            disabled={removable.length === 0}
            label={`Select every download of ${name}`}
          />
        ) : null}
        <button
          onClick={onToggle}
          aria-expanded={open}
          aria-label={`${open ? 'Hide' : 'Show'} downloads of ${name}`}
          className="tap relative grid size-7 shrink-0 place-items-center rounded-md text-base-400 hover:bg-base-800 hover:text-white"
        >
          <svg viewBox="0 0 24 24" className={cx('size-4 transition-transform', open && 'rotate-90')} aria-hidden>
            <path d="m9 6 6 6-6 6" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </button>
        <Link to={`/anime/${show.animeId}`} className="shrink-0">
          <Poster src={show.cover} />
        </Link>
        <button onClick={onToggle} className="min-w-0 flex-1 text-left">
          <p className="truncate font-semibold text-base-100">{name}</p>
          <p className="mt-0.5 text-sm text-base-400 tabular-nums">
            {episodes} episodes · {bytes(size)}
          </p>
        </button>
        {!select && (
          <div className={roomy ? rail : inlineRail}>
            {playing && <Badge tone="accent">Playing</Badge>}
            {savedHere && <Badge tone="success">On this {actions.word}</Badge>}
            <button
              onClick={() => actions.keep(show.items.filter((d) => d.kept === allKept))}
              aria-pressed={allKept}
              aria-label="Keep all"
              title={allKept ? 'All kept. Click to stop keeping them' : 'Keep every episode until you remove it'}
              className={keepButton(allKept)}
            >
              <KeepGlyph filled={allKept} />
            </button>
            {actions.confirming === confirmKey ? (
              <ConfirmDelete
                label={`Remove ${episodeCount(episodesIn(removable))} · ${bytes(sizeOf(removable))}`}
                actions={actions}
                onDelete={() => actions.remove(removable.map((d) => d.infoHash))}
              />
            ) : (
              <MoreMenu label={`More for ${name}`} title={name} detail={`${episodeCount(episodes)} · ${bytes(size)}`} className={menuClass} items={items} />
            )}
          </div>
        )}
      </div>
      {open && (
        <ul>
          {show.items.map((d) => (
            <DownloadRow key={d.infoHash} d={d} actions={actions} nested groupKept={allKept} />
          ))}
        </ul>
      )}
    </li>
  )
}

const DISK_FULL_BELOW = 512 * 1024 * 1024

function DiskProblem({ d, host }: { d: Download; host: boolean }) {
  const full = d.freeBytes != null && d.freeBytes < DISK_FULL_BELOW
  return (
    <p className="flex flex-wrap items-center gap-x-2 text-[13px] text-red-300">
      <span className="min-w-0">
        {full
          ? `Disk full · ${host ? d.drive : 'the PC'} has ${bytes(d.freeBytes!)} free. It carries on once there's space.`
          : d.warning!.charAt(0).toUpperCase() + d.warning!.slice(1)}
      </span>
      {full && (
        <Link to="/settings?tab=Storage" className="font-medium text-accent-300 hover:underline">
          Free space
        </Link>
      )}
    </p>
  )
}

function progressWords(d: Download, held: boolean, mbps: number | undefined): string {
  const pct = `${Math.round(d.percent)}%`
  if (d.checking) return `Checking the file · ${pct}`
  if (d.paused) return held ? `Waiting its turn · ${pct}` : `Paused · ${pct}`
  const left = mbps ? aboutLeft((d.totalBytes - d.bytesOnDisk) / ((mbps * 1_000_000) / 8)) : ''
  return ['Downloading', pct, left].filter(Boolean).join(' · ')
}

const CODECS: Record<string, string> = { H264: 'H.264' }

function releaseFacts(d: Download): string[] {
  return [d.resolution, d.group, d.codec && (CODECS[d.codec] ?? d.codec)].filter((f): f is string => !!f)
}

function DownloadRow({
  d,
  position,
  actions,
  nested = false,
  groupKept = false,
}: {
  d: Download
  position?: number
  actions: RowActions
  nested?: boolean
  groupKept?: boolean
}) {
  const facts = releaseFacts(d)
  const unfinished = d.percent < 100
  const chosen = actions.chosenFor(d)
  const retry = retryNote(d.queued)
  const unit = useSpeedUnit()
  const numbers = d.episodes.length > 0 && `${d.episodes.length > 1 ? 's' : ''} ${d.episode}`
  const held = actions.queueHeld(d)
  const select = !unfinished && !d.pinned ? actions.selecting : undefined
  const first = Number(d.episodes[0])
  const name = `${d.title ?? d.name}${numbers ? ` episode${numbers}` : ''}`
  const onPhone = !unfinished && actions.toDevice?.state(d) === 'saved'
  const items = [
    !!d.animeId && first > 0 && { label: 'Play', to: `/watch/${d.animeId}/${first}` },
    !d.pinned && !!d.animeId && d.episodes.length === 1 && first > 0 && {
      label: 'Change release',
      run: () => actions.changeRelease(d.animeId!, first, d.infoHash),
    },
    deviceItem([d], actions),
    { label: d.kept ? 'Stop keeping' : 'Keep', run: () => actions.keep([d]) },
    !d.pinned && {
      label: unfinished ? 'Stop and remove' : 'Remove',
      danger: true,
      divider: true,
      run: () => actions.removeLater([d]),
    },
  ].filter((item): item is MoreItem => !!item)

  const roomy = unfinished || d.pinned || onPhone

  return (
    <li className={cx('p-3', nested ? 'border-t border-white/[0.06] bg-base-950/30' : card)}>
      <div className={cx('flex items-start gap-3', roomy && 'max-sm:flex-wrap')}>
        {actions.selecting && !unfinished ? (
          <span className="self-center">
            <CheckBox
              checked={!!select && select.picked.has(d.infoHash)}
              onChange={(on) => select?.toggle([d], on)}
              disabled={!select}
              label={d.pinned ? `${name} is playing` : `Select ${name}`}
            />
          </span>
        ) : (
          <Gutter>{position}</Gutter>
        )}
        {!nested && (
          <Link to={d.animeId ? `/anime/${d.animeId}` : '#'} className="shrink-0">
            <Poster src={d.cover} />
          </Link>
        )}
        <div className="min-w-0 flex-1">
          {nested ? (
            <p className="truncate text-sm font-medium text-base-100">{numbers ? `Episode${numbers}` : d.name}</p>
          ) : (
            <p className="truncate text-sm font-medium text-base-100">
              {d.title ?? d.name}
              {numbers && <span className="ml-1.5 font-normal text-base-400">episode{numbers}</span>}
            </p>
          )}
          {facts.length === 0 && d.title && (numbers || !nested) && (
            <p className="truncate text-xs text-base-500">{d.name}</p>
          )}
          {facts.length > 0 && unfinished && (
            <p className="truncate text-[13px] text-base-400">{facts.join(' · ')}</p>
          )}
          {chosen && (
            <p className="truncate text-[13px] text-accent-300" title={chosen.release}>
              Changing to: {chosen.release}
            </p>
          )}
          {retry && (
            <p className="truncate text-[13px] text-amber-300">
              {retry} · {d.queued?.error}
            </p>
          )}
          {d.warning && unfinished && <DiskProblem d={d} host={actions.host} />}
          {unfinished ? (
            <>
              <p className="mt-0.5 text-[13px] font-medium text-base-100 tabular-nums">
                {progressWords(d, held, d.paused ? undefined : d.mbps)}
              </p>
              <p className="text-xs text-base-400 tabular-nums">
                {`${bytes(d.bytesOnDisk)} of ${bytes(d.totalBytes)}`}
                {!d.paused && d.mbps ? ` · ${speed(d.mbps, unit)}` : ''}
                {!d.paused && d.peers ? ` · ${d.peers} peers` : ''}
              </p>
            </>
          ) : (
            <p className="mt-0.5 truncate text-[13px] text-base-400 tabular-nums">
              {[...facts, bytes(d.bytesOnDisk)].join(' · ')}
            </p>
          )}
        </div>
        {!(actions.selecting && !unfinished) && (
          <div className={roomy ? rail : inlineRail}>
            {d.pinned && <Badge tone="accent">Playing</Badge>}
            {onPhone && <Badge tone="success">On this {actions.word}</Badge>}

            {unfinished && !d.checking && (
              <button
                onClick={() => actions.pauseResume(d)}
                disabled={actions.toggling}
                aria-label={d.paused ? 'Resume download' : 'Pause download'}
                title={d.paused ? 'Resume this one' : 'Pause this one. The next in line starts; "Pause all" stops everything.'}
                className={iconButton}
              >
                {d.paused ? <PlayGlyph /> : <PauseGlyph />}
              </button>
            )}

            {!(nested && groupKept) && (
              <button
                onClick={() => actions.keep([d])}
                aria-pressed={d.kept}
                aria-label="Keep"
                title={
                  d.kept
                    ? `Kept until you remove ${d.episodes.length > 1 ? 'them' : 'it'}. Click to stop keeping ${
                        d.episodes.length > 1 ? `all ${d.episodes.length} episodes` : 'it'
                      }.`
                    : `Keep${d.episodes.length > 1 ? ` all ${d.episodes.length} episodes` : ''} until you remove ${
                        d.episodes.length > 1 ? 'them' : 'it'
                      }.`
                }
                className={keepButton(d.kept)}
              >
                <KeepGlyph filled={d.kept} />
              </button>
            )}

            <MoreMenu label={`More for ${name}`} title={numbers ? `${d.title ?? d.name} · Episode${numbers}` : d.title ?? d.name} detail={d.name} className={menuClass} items={items} />
          </div>
        )}
      </div>
      {unfinished && <ProgressBar value={d.percent} className="mt-2" label="Downloaded" />}
      {d.episodes.length > 1 && <PackEpisodes hash={d.infoHash} />}
    </li>
  )
}

function Gutter({ children }: { children?: ReactNode }) {
  return (
    <span
      className={cx(
        'grid h-7 min-w-7 shrink-0 place-items-center self-center rounded-md px-1 text-xs tabular-nums text-base-400',
        children != null ? 'bg-white/[0.05]' : 'max-sm:hidden',
      )}
    >
      {children}
    </span>
  )
}

function Group({ label, meta, note, change }: { label: string; meta?: string; note?: string; change?: string }) {
  return (
    <li className="px-1 pt-3 first:pt-0">
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-[13px] font-semibold text-base-300">{label}</h2>
        {meta && <span className="text-xs text-base-500 tabular-nums">{meta}</span>}
      </div>
      {note && (
        <p className="text-[13px] text-base-400">
          {note}
          {change && (
            <Link to={change} className="tap relative ml-2 font-medium text-accent-300 hover:underline">
              Change
            </Link>
          )}
        </p>
      )}
    </li>
  )
}

function QueueRow({
  q,
  position,
  actions,
  onNow,
  onRetry,
  onRemove,
  onChangeRelease,
}: {
  q: Queued
  position?: number
  actions: RowActions
  onNow: () => void
  onRetry: () => void
  onRemove: () => void
  onChangeRelease: () => void
}) {
  const key = `q-${q.animeId}-${q.epKey}`
  const got = q.torrent
  return (
    <li className={cx(card, 'group p-3')}>
      <div className="flex items-center gap-3 max-sm:flex-wrap">
        <Gutter>{position}</Gutter>
        <Poster src={q.cover} />

        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium text-base-100">
            {q.title ?? `Anime ${q.animeId}`}
            <span className="ml-1.5 font-normal text-base-400">episode {q.episode}</span>
          </p>
          {q.error && q.state === 'failed' && <p className="truncate text-[13px] text-red-300">{q.error}</p>}
          {q.retryAt && (
            <p className="truncate text-[13px] text-amber-300">
              {retryNote(q)} · {q.error}
            </p>
          )}
          {got ? (
            <p className="truncate text-xs text-base-500" title={got.name}>
              {got.name}
            </p>
          ) : q.release ? (
            <p className="truncate text-xs text-base-500" title={q.release}>
              Chosen: {q.release}
            </p>
          ) : q.state === 'active' ? (
            <p className="text-[13px] text-base-400">Finding a release…</p>
          ) : (
            q.state === 'pending' &&
            !q.retryAt &&
            position != null && (
              <p className="text-[13px] text-base-400">{position > 1 ? `Waiting · ${ordinal(position)} in line` : 'Next in line'}</p>
            )
          )}
          {got && (
            <p className="mt-0.5 text-[13px] text-base-400 tabular-nums">
              <span className="font-medium text-base-100">{Math.round(got.percent)}% · </span>
              {bytes(got.bytesOnDisk)} of {bytes(got.totalBytes)}
            </p>
          )}
        </div>

        <div className={rail}>
          <button
            onClick={onChangeRelease}
            aria-label={`Change release for episode ${q.episode}`}
            title="Change release: pick the one to download instead of what the search finds"
            className={iconButton}
          >
            <SwapGlyph />
          </button>
          {q.state === 'active' ? null : q.state === 'failed' ? (
            <button onClick={onRetry} aria-label="Try again" title="Try again" className={iconButton}>
              <RetryGlyph />
            </button>
          ) : (
            <button
              onClick={onNow}
              aria-label="Download now"
              title="Download now. The one downloading goes back to wait behind it."
              className={iconButton}
            >
              <FrontGlyph />
            </button>
          )}
          {got && actions.confirming === key ? (
            <ConfirmDelete
              label={`Delete ${bytes(got.bytesOnDisk)}`}
              actions={actions}
              onDelete={() => {
                onRemove()
                actions.remove([got.infoHash])
              }}
            />
          ) : (
            <button
              onClick={got ? () => actions.confirm(key) : onRemove}
              aria-label="Remove from queue"
              title={got ? 'Remove from the queue and delete what it got' : 'Remove from the queue'}
              className={deleteButton}
            >
              ✕
            </button>
          )}
        </div>
      </div>
      {got && <ProgressBar value={got.percent} className="mt-2" label="Downloaded" />}
    </li>
  )
}

function PcStorage({ host }: { host: boolean }) {
  const room = useDriveRoom()
  const { queue, items, sections } = useVisibleDownloads()
  const [freeing, setFreeing] = useState(false)
  const info = room.data as CacheRoom | undefined
  const used = sizeOf(items)
  const free = info?.freeBytes ?? null
  const total = info?.totalBytes ?? null
  const where = host && info?.drive ? `on ${info.drive}` : 'on the PC'
  const low = !!queue.data?.short || (free != null && (total ? free / total < LOW_SHARE : free < LOW_BYTES))
  const clearable = sizeOf(sections.finished.filter((d) => !d.kept && !d.pinned))
  const yoru = used > 0 ? `Yoru ${bytes(used)}` : 'Nothing from Yoru yet'
  const line =
    free == null
      ? `${yoru} ${where}`
      : low
        ? `Space is running low: ${bytes(free)} free ${where}.${clearable > 0 ? ` Clear watched episodes to free ${bytes(clearable)}.` : ''}`
        : `${yoru} · ${bytes(free)} free ${where}`
  return (
    <>
      <StorageCard
        used={used}
        other={total && free != null ? Math.max(0, total - used - free) : 0}
        free={free}
        total={total}
        line={line}
        low={low}
        onFree={sections.finished.length > 0 ? () => setFreeing(true) : undefined}
      />
      {freeing && (
        <BottomSheet title="Free up space" detail="On the PC" onClose={() => setFreeing(false)}>
          <PcFreeUpList onDone={() => setFreeing(false)} />
        </BottomSheet>
      )}
    </>
  )
}

function PcFreeUp() {
  const { sections } = useVisibleDownloads()
  if (sections.finished.length === 0) return null
  return (
    <PanelCard title="Free up space" className="max-lg:hidden">
      <PcFreeUpList />
    </PanelCard>
  )
}

function PcFreeUpList({ onDone }: { onDone?: () => void }) {
  const qc = useQueryClient()
  const { sections } = useVisibleDownloads()
  const removeLater = useRemoveLater()
  const [confirm, setConfirm] = useState<string | null>(null)
  const clearAll = useMutation({
    mutationFn: () => api.post('/api/downloads/clear?scope=all'),
    onSettled: () => {
      setConfirm(null)
      refreshDownloads(qc)
    },
  })
  const passing = sections.finished.filter((d) => !d.kept && !d.pinned)
  const unfinished = [...sections.active, ...sections.held].filter((d) => !d.kept && !d.pinned)
  const shows = new Map<string, { title: string; items: Download[] }>()
  for (const d of sections.finished) {
    const key = d.animeId ? `a${d.animeId}` : d.infoHash
    const show = shows.get(key) ?? { title: d.title ?? d.name, items: [] }
    show.items.push(d)
    shows.set(key, show)
  }
  const biggest = [...shows.entries()].sort((a, b) => sizeOf(b[1].items) - sizeOf(a[1].items))

  const clearNotKept = () => {
    removeLater(passing)
    onDone?.()
  }

  return (
    <ul className="divide-y divide-white/[0.06] px-3">
      <FreeRow
        title="Not kept"
        detail={passing.length > 0 ? `${episodeCount(episodesIn(passing))} · ${bytes(sizeOf(passing))}` : 'Nothing to clear: the rest are kept'}
        action={
          <Button size="sm" disabled={passing.length === 0} onClick={clearNotKept}>
            Clear
          </Button>
        }
      />
      {unfinished.length > 0 && (
        <FreeRow
          title="Unfinished downloads"
          detail={`${episodeCount(episodesIn(unfinished))} · ${bytes(sizeOf(unfinished))} so far`}
          action={
            confirm === 'all' ? (
              <ConfirmInline
                size="sm"
                label="Clear all, unfinished too"
                pending={clearAll.isPending}
                onConfirm={() => clearAll.mutate()}
                onCancel={() => setConfirm(null)}
              />
            ) : (
              <Button size="sm" onClick={() => setConfirm('all')}>
                Clear
              </Button>
            )
          }
        />
      )}
      {biggest.map(([key, show]) => {
        const removable = show.items.filter((d) => !d.pinned)
        return (
          <FreeRow
            key={key}
            title={show.title}
            detail={`${episodeCount(episodesIn(show.items))} · ${bytes(sizeOf(show.items))}${show.items.some((d) => d.kept) ? ' · kept' : ''}`}
            action={
              confirm === key ? (
                <ConfirmInline
                  size="sm"
                  label={`Remove ${episodeCount(episodesIn(removable))}`}
                  onConfirm={() => {
                    setConfirm(null)
                    removeLater(removable)
                  }}
                  onCancel={() => setConfirm(null)}
                />
              ) : (
                <Button
                  size="sm"
                  disabled={removable.length === 0}
                  onClick={() => setConfirm(key)}
                  aria-label={`Remove ${show.title} from the PC`}
                >
                  Remove
                </Button>
              )
            }
          />
        )
      })}
    </ul>
  )
}

const BUDGETS = [5, 10, 20, 40, 80].map((gb) => String(gb * 1024 ** 3))

function PcSettings() {
  const prefs = usePrefs()
  const setPref = useSetPref()
  const { sections } = useVisibleDownloads()
  const effective = prefs.data?.effective
  const mode = effective?.['cache.autodelete'] || 'off'
  const budget = effective?.['cache.budget_bytes'] ?? ''
  const keptGo = mode !== 'off' && effective?.['cache.autodelete_downloads'] === 'true'
  const passing = sizeOf(sections.finished.filter((d) => !d.kept))
  const budgets = budget && !BUDGETS.includes(budget) ? [...BUDGETS, budget].sort((a, b) => Number(a) - Number(b)) : BUDGETS
  const set = (key: string, value: string) => setPref.mutate({ key, value })
  return (
    <PanelCard title="Auto tidy on the PC">
      <label className="block space-y-1.5">
        <span className="block text-[13px] text-base-400">Watched episodes</span>
        <Select value={mode} onChange={(v) => set('cache.autodelete', v)} aria-label="Watched episodes" className="w-full">
          <option value="off">Keep until space runs low</option>
          <option value="now">Delete right after watching</option>
          <option value="keep2">Delete once two more are watched</option>
        </Select>
      </label>
      <label className="block space-y-1.5">
        <span className="block text-[13px] text-base-400">Room for watched episodes</span>
        <Select value={budget} onChange={(v) => set('cache.budget_bytes', v)} aria-label="Room for watched episodes" className="w-full">
          {budgets.map((b) => (
            <option key={b} value={b}>
              {bytes(Number(b))}
            </option>
          ))}
        </Select>
        <span className={cx('block text-[13px] tabular-nums', budget && passing > Number(budget) ? 'text-amber-300' : 'text-base-400')}>
          {budget ? `${bytes(passing)} in use of ${bytes(Number(budget))}` : `${bytes(passing)} in use`}
        </span>
      </label>
      <p className="text-[13px] text-base-400">
        {keptGo ? 'Kept episodes are deleted too, once watched. ' : 'Kept episodes stay until you remove them. '}
        <Link to="/settings?tab=Storage" className="font-medium text-accent-300 hover:underline">
          More in Settings
        </Link>
      </p>
    </PanelCard>
  )
}

interface LocalFile {
  id: number
  path: string
  size: number
  animeId?: number
  episode?: number
  parsedTitle?: string
  resolution?: string
  releaseGroup?: string
  confidence: number
}

interface SearchHit {
  id: number
  title: { romaji?: string; english?: string }
  coverImage?: { medium?: string }
  format?: string
  seasonYear?: number
  episodes?: number
}

function AssignFile({ file, onDone }: { file: LocalFile; onDone: () => void }) {
  const qc = useQueryClient()
  const [term, setTerm] = useState(file.parsedTitle ?? '')
  const q = useDebounced(term, 300)
  const [picked, setPicked] = useState<SearchHit | null>(null)
  const [episode, setEpisode] = useState(String(file.episode || 1))

  const results = useQuery({
    enabled: q.trim().length >= 3 && !picked,
    queryKey: ['search', q],
    queryFn: ({ signal }) =>
      api.get<{ results: SearchHit[] }>(`/api/search?q=${encodeURIComponent(q)}&limit=8`, signal),
    staleTime: 60_000,
  })

  const assign = useMutation({
    meta: { inline: true },
    mutationFn: () =>
      api.post('/api/local/assign', { id: file.id, animeId: picked!.id, episode: Number(episode) }),
    onSuccess: () => {
      for (const key of ['localFiles', 'local', 'episodes', 'anime']) void qc.invalidateQueries({ queryKey: [key] })
      onDone()
    },
  })

  const name = (h: SearchHit) => h.title.english || h.title.romaji || `Anime ${h.id}`

  return (
    <div className="mt-2 space-y-2 rounded-lg border border-base-850 bg-base-950/40 p-3">
      {picked ? (
        <div className="flex items-center gap-3">
          {picked.coverImage?.medium && (
            <img src={picked.coverImage.medium} alt="" className="h-12 w-8 rounded object-cover" />
          )}
          <p className="min-w-0 flex-1 truncate text-sm text-base-100">{name(picked)}</p>
          <button onClick={() => setPicked(null)} className="text-xs text-base-400 hover:text-white">
            Change
          </button>
        </div>
      ) : (
        <>
          <TextInput
            value={term}
            onChange={(e) => setTerm(e.target.value)}
            placeholder="Search for the show…"
            aria-label="Search for the show"
            autoFocus
            className="w-full"
          />
          {results.isPending && q.trim().length >= 3 && <Skeleton className="h-10 w-full" />}
          {results.data && results.data.results.length === 0 && (
            <p className="text-xs text-base-500">Nothing matched.</p>
          )}
          {results.data && results.data.results.length > 0 && (
            <ul className="max-h-56 space-y-0.5 overflow-y-auto scrollbar-thin">
              {results.data.results.map((h) => (
                <li key={h.id}>
                  <button
                    onClick={() => setPicked(h)}
                    className="flex w-full items-center gap-2.5 rounded-md px-2 py-1 text-left hover:bg-base-850"
                  >
                    {h.coverImage?.medium ? (
                      <img src={h.coverImage.medium} alt="" className="h-10 w-7 rounded object-cover" />
                    ) : (
                      <div className="h-10 w-7 rounded bg-base-850" />
                    )}
                    <span className="min-w-0 flex-1 truncate text-sm text-base-100">{name(h)}</span>
                    <span className="shrink-0 text-xs text-base-500">
                      {[h.format?.replace('_', ' '), h.seasonYear, h.episodes && `${h.episodes} ep`]
                        .filter(Boolean)
                        .join(' · ')}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </>
      )}

      <div className="flex items-center gap-2">
        <label className="flex items-center gap-1.5 text-xs text-base-400">
          Episode
          <TextInput
            type="number"
            min={1}
            value={episode}
            onChange={(e) => setEpisode(e.target.value)}
            aria-label="Episode number"
            className="w-20"
          />
        </label>
        <button
          onClick={() => assign.mutate()}
          disabled={!picked || Number(episode) < 1 || assign.isPending}
          className={buttonClass('primary', 'sm')}
        >
          {assign.isPending ? 'Saving…' : 'Save'}
        </button>
        {assign.isError && <span className="text-xs text-recap">{(assign.error as Error).message}</span>}
      </div>
    </div>
  )
}

export function LocalFiles() {
  const [unmatchedOnly, setUnmatchedOnly] = useState(false)
  const [page, setPage] = useState(1)
  const setUnmatched = (v: boolean) => {
    setUnmatchedOnly(v)
    setPage(1)
  }
  const qc = useQueryClient()
  const { data, isPending, isError, error, refetch } = useQuery({
    queryKey: ['localFiles', unmatchedOnly, page],
    queryFn: () =>
      api.get<Page<LocalFile>>(
        `/api/local/files?perPage=100&page=${page}${unmatchedOnly ? '&unmatched=true' : ''}`,
      ),
    placeholderData: (prev) => prev,
  })
  const stats = useQuery({
    queryKey: ['local'],
    queryFn: () => api.get<{ stats: { missing: number } }>('/api/local'),
  })
  const missing = stats.data?.stats.missing ?? 0
  const forget = useMutation({
    mutationFn: () => api.post<{ forgotten: number }>('/api/local/forget'),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['local'] })
      void qc.invalidateQueries({ queryKey: ['localFiles'] })
    },
  })
  const [assigning, setAssigning] = useState<number | null>(null)
  useClampPage(data?.items.length === 0, page, data?.total, 100, setPage)

  if (isError && !data) return <ErrorState error={error} retry={() => refetch()} />

  return (
    <div className="space-y-4">
      <PageHeader
        title="Local files"
        meta={data && data.items.length > 0 ? `${data.total} files` : undefined}
        actions={
          <>
            {missing > 0 && (
              <Button
                onClick={() => forget.mutate()}
                disabled={forget.isPending}
                title="Files a scan no longer found. Forgetting drops their records; unplug a drive and they stay."
              >
                Forget {missing} missing
              </Button>
            )}
            <FilterToggle on={unmatchedOnly} onChange={setUnmatched}>
              Unmatched only
            </FilterToggle>
            <LinkButton to="/settings?tab=Library">Manage folders</LinkButton>
          </>
        }
      />

      {isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : data.items.length === 0 ? (
        <Empty
          title={unmatchedOnly ? 'Every file is matched' : 'No files scanned'}
          hint={
            unmatchedOnly
              ? 'Nothing needs assigning by hand.'
              : 'Add a folder in settings and run a scan to play anime you already have.'
          }
          icon={<FolderIcon />}
          action={
            unmatchedOnly ? undefined : (
              <LinkButton to="/settings?tab=Library" variant="primary">
                Add a folder
              </LinkButton>
            )
          }
        />
      ) : (
        <>
          <ul className="surface divide-y divide-white/[0.05] overflow-hidden">
            {data.items.map((f) => (
              <li key={f.id} className="p-2.5">
                <div className="flex items-center gap-3">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm text-base-100">{f.path.split(/[\\/]/).pop()}</p>
                    <p className="mt-0.5 truncate text-xs text-base-500">
                      {bytes(f.size)}
                      {f.resolution && ` · ${f.resolution}`}
                      {f.releaseGroup && ` · ${f.releaseGroup}`}
                    </p>
                  </div>
                  {f.animeId ? (
                    <Link
                      to={`/watch/${f.animeId}/${f.episode ?? 1}`}
                      className="shrink-0 rounded-md bg-base-800 px-2.5 py-1 text-xs text-base-100 hover:bg-base-700"
                    >
                      Play ep {f.episode ?? '?'}
                    </Link>
                  ) : (
                    <Badge tone="filler">Unmatched</Badge>
                  )}
                  <button
                    onClick={() => setAssigning(assigning === f.id ? null : f.id)}
                    className="shrink-0 rounded-md px-2.5 py-1 text-xs text-base-300 transition-colors hover:bg-base-800 hover:text-white"
                  >
                    {assigning === f.id ? 'Cancel' : f.animeId ? 'Reassign' : 'Assign'}
                  </button>
                </div>
                {assigning === f.id && (
                  <AssignFile file={f} onDone={() => setAssigning(null)} />
                )}
              </li>
            ))}
          </ul>
          <Pager page={page} hasMore={data.hasMore} onPage={setPage} />
        </>
      )}
    </div>
  )
}
