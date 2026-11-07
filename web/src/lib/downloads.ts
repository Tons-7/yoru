import { bytes, speed, type SpeedUnit } from './format'
import type { Download, Queued } from './queries'

export interface DownloadPart {
  epKey: string
  fileIndex: number
  size: number
  done: number
  complete: boolean
  kept: boolean
  pinned: boolean
  paused: boolean
}

export interface EpisodeRow extends Download {
  key: string
  part?: DownloadPart
  whole?: Download
}

export interface DownloadSections {
  active: EpisodeRow[]
  held: EpisodeRow[]
  finished: EpisodeRow[]
  searching: Queued[]
  waitingQueue: Queued[]
  failed: Queued[]
  line: ({ d: EpisodeRow; q?: Queued } | { q: Queued; d?: undefined })[]
}

export function byEpisode(items: Download[]): EpisodeRow[] {
  return items.flatMap((d): EpisodeRow[] => {
    const parts = (d as Download & { parts?: DownloadPart[] }).parts ?? []
    if (parts.length < 2) return [{ ...d, key: d.infoHash }]
    return parts.map((p) => ({
      ...d,
      key: `${d.infoHash}:${p.fileIndex}`,
      episodes: [p.epKey],
      episode: p.epKey,
      totalBytes: p.size,
      bytesOnDisk: p.done,
      percent: p.complete ? 100 : p.size > 0 ? Math.min(99.9, (p.done / p.size) * 100) : 0,
      kept: p.kept,
      pinned: p.pinned,
      paused: !!d.paused || p.paused,
      part: p,
      whole: d,
    }))
  })
}

export const rowKey = (d: EpisodeRow) => `pc:${d.key}`

export function sortDownloads(items: Download[], queued: Queued[], hidden?: ReadonlySet<string>): DownloadSections {
  const entries = new Map(queued.map((q) => [`${q.animeId}-${q.epKey}`, q]))
  const rows = byEpisode(items).filter((d) => !hidden?.has(rowKey(d)))
  const paired = rows.map((d) => {
    const q = d.episodes.map((e) => entries.get(`${d.animeId}-${e}`)).find(Boolean)
    return q ? { ...d, queued: q } : d
  })
  const absorbed = (d: Download) => d.percent < 100 && !!d.paused && d.queued?.state === 'failed'
  const started = new Set(paired.filter((d) => !absorbed(d)).flatMap((d) => d.episodes.map((e) => `${d.animeId}-${e}`)))
  const unstarted = (q: Queued) => !started.has(`${q.animeId}-${q.epKey}`)
  const held = paired.filter((d) => d.percent < 100 && d.paused && !absorbed(d))
  const waitingQueue = queued.filter((q) => (q.state === 'pending' || q.state === 'paused') && unstarted(q))
  const placed = new Set<EpisodeRow>()
  const line: DownloadSections['line'] = []
  for (const q of queued) {
    if (q.state !== 'pending' && q.state !== 'paused') continue
    const d = held.find((h) => !placed.has(h) && h.animeId === q.animeId && h.episodes.includes(q.epKey))
    if (d) {
      placed.add(d)
      line.push({ d, q })
    } else if (waitingQueue.includes(q)) {
      line.push({ q })
    }
  }
  for (const d of held) if (!placed.has(d)) line.push({ d })
  return {
    line,
    active: paired.filter((d) => d.percent < 100 && !d.paused),
    held,
    finished: paired.filter((d) => d.percent >= 100),
    searching: queued.filter((q) => q.state === 'active' && unstarted(q)),
    waitingQueue,
    failed: queued
      .filter((q) => q.state === 'failed')
      .map((q) => {
        const torrent = paired.find((d) => absorbed(d) && d.queued === q)
        return torrent ? { ...q, torrent } : q
      }),
  }
}

export function totalSpeed(rows: Download[]): number {
  const seen = new Map<string, number>()
  for (const d of rows) seen.set(d.infoHash, d.mbps ?? 0)
  return [...seen.values()].reduce((n, v) => n + v, 0)
}

export function retryNote(q: Queued | undefined): string {
  if (!q?.retryAt) return ''
  return `tries again at ${new Date(q.retryAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}`
}

export function downloadLabel(d: Download): string {
  if (!d.title) return d.name
  return d.episodes.length > 0 ? `${d.title} episode${d.episodes.length > 1 ? 's' : ''} ${d.episode}` : d.title
}

export function downloadProgress(d: Download, unit?: SpeedUnit): string {
  return [
    `${Math.round(d.percent)}%`,
    `${bytes(d.bytesOnDisk)} of ${bytes(d.totalBytes)}`,
    !d.paused && d.mbps ? speed(d.mbps, unit) : '',
    !d.paused && d.peers ? `${d.peers} peers` : '',
    d.paused ? 'paused' : '',
    d.checking ? 'checking file' : '',
  ]
    .filter(Boolean)
    .join(' · ')
}

export function downloadsReport(s: DownloadSections, unit?: SpeedUnit): string {
  const waiting = (q: Queued | undefined) => (q?.retryAt ? ` · ${retryNote(q)}: ${q.error}` : '')
  const queuedLine = (q: Queued) =>
    `- ${q.title ?? `Anime ${q.animeId}`} episode ${q.episode}` +
    `${q.torrent ? ` — ${Math.round(q.torrent.percent)}% on disk` : ''}${q.error ? ` — ${q.error}` : ''}${waiting(q)}`
  const downloadLine = (d: Download) =>
    `- ${downloadLabel(d)} — ${downloadProgress(d, unit)}${d.pinned ? ' · playing' : ''}${d.kept ? ' · kept' : ''}${waiting(d.queued)}` +
    (d.title ? `\n  ${d.name}` : '')
  const section = (name: string, lines: string[]) => (lines.length ? [`${name} (${lines.length})`, ...lines, ''] : [])

  return [
    `Yoru downloads at ${new Date().toLocaleTimeString()}`,
    '',
    ...section('Downloading', [...s.searching.map((q) => `${queuedLine(q)} — finding a release`), ...s.active.map(downloadLine)]),
    ...section('Waiting', [...s.held.map(downloadLine), ...s.waitingQueue.map(queuedLine)]),
    ...section('Failed', s.failed.map(queuedLine)),
    ...section('On disk', s.finished.map((d) => `- ${downloadLabel(d)} — ${bytes(d.bytesOnDisk)}${d.kept ? ' · kept' : ''}`)),
  ]
    .join('\n')
    .trim()
}

export async function copyText(text: string, within: HTMLElement = document.body): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) {
    const written = await navigator.clipboard.writeText(text).then(
      () => true,
      () => false,
    )
    if (written) return true
  }
  const area = document.createElement('textarea')
  area.value = text
  area.setAttribute('readonly', '')
  area.style.cssText = 'position:fixed;top:0;left:0;opacity:0'
  within.appendChild(area)
  area.select()
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  area.remove()
  return ok
}
