export function clockTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '0:00'
  const total = Math.floor(seconds)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`
}

export function bytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  return `${(n / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

/** A whole number of hours for a sentence: "1 hour", "3 hours". */
export function waitLabel(hours: string): string {
  return hours === '1' ? '1 hour' : `${hours || '0'} hours`
}

/** Settings' unit for speeds: megabits as speed tests give them, or megabytes as file sizes are. */
export type SpeedUnit = 'mbps' | 'mbs'

/** A speed given in megabits a second: "12.4 Mbps", or in bytes()'s units "1.5 MB/s", "176 KB/s". */
export function speed(mbps: number, unit: SpeedUnit = 'mbps'): string {
  if (unit !== 'mbs') return `${mbps.toFixed(1)} Mbps`
  const perSecond = mbps * 125_000
  const kb = Math.round(perSecond / 1024)
  return kb < 1000 ? `${kb} KB/s` : `${(perSecond / 1024 ** 2).toFixed(1)} MB/s`
}

/** A speed limit, stored in bytes a second, as its choice reads: "10 Mbps", "1.2 MB/s". */
export function speedLimit(bytesPerSecond: number, unit: SpeedUnit = 'mbps'): string {
  return unit === 'mbs'
    ? `${+(bytesPerSecond / 1024 ** 2).toFixed(1)} MB/s`
    : `${+((bytesPerSecond * 8) / 1_000_000).toFixed(1)} Mbps`
}

/** "in 2h 14m" / "3d ago" — the schedule and episode lists both want this. */
export function relativeTime(unix: number): string {
  const delta = unix * 1000 - Date.now()
  const abs = Math.abs(delta)
  const minute = 60_000
  const hour = 60 * minute
  const day = 24 * hour

  // Rounded to whole minutes once, so 59.6 minutes reads "1h", not "60m", and never "2h 60m".
  const mins = Math.max(1, Math.round(abs / minute))
  let text: string
  if (mins < 60) text = `${mins}m`
  else if (abs < day) {
    const h = Math.floor(mins / 60)
    const m = mins % 60
    text = m > 0 ? `${h}h ${m}m` : `${h}h`
  } else text = `${Math.round(abs / day)}d`

  return delta >= 0 ? `in ${text}` : `${text} ago`
}

/** "10/13 eps" while airing, "13 eps" once it has finished, "10 eps" with no total. */
export function episodeCount(anime: {
  status?: string | null
  episodes?: number | null
  aired?: number | null
}): string | null {
  const { episodes, aired } = anime
  if (anime.status === 'RELEASING' && aired && episodes && aired < episodes) return `${aired}/${episodes} eps`
  if (episodes) return `${episodes} ${episodes === 1 ? 'ep' : 'eps'}`
  if (aired) return `${aired} eps`
  return null
}

export function airTime(unix: number): string {
  return new Date(unix * 1000).toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
  })
}

// A schedule day as TV guides name it: Yesterday, Today, Tomorrow, then its weekday; short, for a narrow
// column, Today or "Fri". Dates are "2026-10-29", today's from the schedule itself.
export function dayName(date: string, weekday: string, today: string | undefined, short = false): string {
  const from = today ? Math.round((Date.parse(`${date}T00:00Z`) - Date.parse(`${today}T00:00Z`)) / 86_400_000) : NaN
  if (from === 0) return 'Today'
  if (short) return weekday.slice(0, 3)
  return from === -1 ? 'Yesterday' : from === 1 ? 'Tomorrow' : weekday
}

/** MyAnimeList's "Fukuyama, Jun" as it's said: "Jun Fukuyama". Anything else stays as it is. */
export function personName(name: string): string {
  const parts = name.split(', ')
  return parts.length === 2 && parts[0] && parts[1] ? `${parts[1]} ${parts[0]}` : name
}

/** "Oct 29", a schedule day's date in the viewer's own format. */
export function dayDate(date: string): string {
  return new Date(`${date}T00:00`).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

/** A dense screen takes the larger poster; an ordinary one keeps the small file. */
export function posterSet(cover?: string | null, large?: string | null): string | undefined {
  return cover && large && large !== cover ? `${cover} 1x, ${large} 2x` : undefined
}

/** AniList gives each anime a dominant cover colour; it makes a good accent. */
export function tint(color?: string | null, alpha = 1): string | undefined {
  if (!color || !/^#[0-9a-f]{6}$/i.test(color)) return undefined
  if (alpha >= 1) return color
  const n = parseInt(color.slice(1), 16)
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`
}

/** The show's colour, lifted until it reads as text on the dark background. */
export function readableTint(color?: string | null): string | undefined {
  if (!color || !/^#[0-9a-f]{6}$/i.test(color)) return undefined
  const n = parseInt(color.slice(1), 16)
  let [r, g, b] = [(n >> 16) & 255, (n >> 8) & 255, n & 255]
  const luminance = () => (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255
  // Mix toward white until bright enough; a few steps cover any colour.
  for (let i = 0; i < 8 && luminance() < 0.6; i++) {
    r += (255 - r) * 0.25
    g += (255 - g) * 0.25
    b += (255 - b) * 0.25
  }
  return `rgb(${Math.round(r)}, ${Math.round(g)}, ${Math.round(b)})`
}

/** Items under "Today", "Yesterday", then dated headings, in the order given. */
export function groupByDay<T>(items: T[], at: (item: T) => number): Array<[string, T[]]> {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const groups = new Map<string, T[]>()
  for (const item of items) {
    const day = new Date(at(item) * 1000)
    day.setHours(0, 0, 0, 0)
    const back = Math.round((today.getTime() - day.getTime()) / 86_400_000)
    const label =
      back <= 0
        ? 'Today'
        : back === 1
          ? 'Yesterday'
          : day.toLocaleDateString(undefined, { weekday: 'long', month: 'short', day: 'numeric' })
    groups.set(label, [...(groups.get(label) ?? []), item])
  }
  return [...groups]
}

/** "Episodes 1–22", "Episodes 1–5, 8, 10–12", "Episode 4"; "14 episodes" past four runs. */
export function episodeSpan(numbers: number[]): string {
  const sorted = [...new Set(numbers)].sort((a, b) => a - b)
  if (sorted.length === 1) return `Episode ${sorted[0]}`
  const runs: string[] = []
  for (let i = 0; i < sorted.length; ) {
    let j = i
    while (j + 1 < sorted.length && sorted[j + 1] === sorted[j] + 1) j++
    runs.push(j > i ? `${sorted[i]}–${sorted[j]}` : String(sorted[i]))
    i = j + 1
  }
  return runs.length > 4 ? `${sorted.length} episodes` : `Episodes ${runs.join(', ')}`
}

export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(' ')
}

// ffmpeg's ISO 639-2/B codes Intl rejects ("ger", not "deu"), for languages seen on releases.
const BIBLIOGRAPHIC: Record<string, string> = {
  ger: 'de', fre: 'fr', dut: 'nl', gre: 'el', chi: 'zh', cze: 'cs',
  ice: 'is', mac: 'mk', mao: 'mi', may: 'ms', per: 'fa', rum: 'ro',
  slo: 'sk', tib: 'bo', wel: 'cy', arm: 'hy', baq: 'eu', bur: 'my', geo: 'ka',
}

/** A subtitle track's language spelled out, since groups title every track alike (Erai-raws: "CR"). */
export function languageName(code?: string | null): string | undefined {
  const raw = code?.trim().toLowerCase()
  if (!raw || raw === 'und') return undefined

  const tag = BIBLIOGRAPHIC[raw] ?? raw
  try {
    const name = new Intl.DisplayNames(['en'], { type: 'language' }).of(tag)
    // Intl echoes the input back when it does not recognise it.
    if (name && name.toLowerCase() !== tag) return name
  } catch {
    // An malformed code throws rather than returning; fall through to the code.
  }
  return raw.toUpperCase()
}

/** The YouTube id inside any of the URL shapes AniList and MAL hand out. */
export function youtubeID(url?: string | null): string | undefined {
  if (!url) return undefined
  const m = url.match(/(?:youtu\.be\/|[?&]v=|\/embed\/)([\w-]{6,})/)
  return m?.[1]
}
