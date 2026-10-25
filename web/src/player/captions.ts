import { useEffect, useRef } from 'react'

// Subtitles as the browser's own text track, for windows showing only those; dialogue only, not placed signs.

export interface Cue {
  start: number
  end: number
  text: string
}

const time = (t: string) => {
  const [h, m, s] = t.trim().split(':')
  return Number(h) * 3600 + Number(m) * 60 + Number(s)
}

/** The spoken lines of an ASS track, its tags dropped. */
export function assCues(ass: string): Cue[] {
  const out: Cue[] = []
  let format: string[] = []
  let events = false
  for (const raw of ass.split(/\r?\n/)) {
    const line = raw.trim()
    if (line.startsWith('[')) {
      events = line.toLowerCase() === '[events]'
      continue
    }
    if (!events) continue
    if (line.startsWith('Format:')) {
      format = line.slice(7).split(',').map((f) => f.trim().toLowerCase())
      continue
    }
    if (!line.startsWith('Dialogue:') || format.length === 0) continue
    // The text is the last field and may hold commas of its own.
    const fields = line.slice(9).split(',')
    const text = fields.slice(format.length - 1).join(',')
    const at = (name: string) => fields[format.indexOf(name)] ?? ''
    // Placed or drawn: a sign, not a line said.
    if (/\\(pos|move)\(|\\p[1-9]/.test(text)) continue
    const plain = text
      .replace(/\{[^}]*\}/g, '')
      .replace(/\\[Nn]/g, '\n')
      .replace(/\\h/g, ' ')
      .trim()
    if (!plain) continue
    out.push({ start: time(at('start')), end: time(at('end')), text: plain })
  }
  return out
}

/** url's lines as the video's text track, shown only when the page can't draw them; re-asked while growing. */
export function useNativeCaptions(
  video: HTMLVideoElement | null,
  source: { url: string; whole?: boolean } | undefined,
  showing: boolean,
  delay: number,
) {
  const track = useRef<{ video: HTMLVideoElement; tt: TextTrack } | null>(null)
  const cues = useRef<Cue[]>([])
  // Read at each refresh, so a refresh after the viewer moves the delay keeps the new one.
  const shift = useRef(delay)
  const url = source?.url
  const whole = !!source?.whole

  useEffect(() => {
    if (!video || !url || !showing) return
    // One per element: a text track can't be taken off again.
    if (track.current?.video !== video) track.current = { video, tt: video.addTextTrack('subtitles', 'Subtitles') }
    const tt = track.current.tt
    let stop = false
    let timer: number | undefined
    const fill = async () => {
      try {
        const res = await fetch(url)
        // The session is gone; so is the player, shortly. Any other failure keeps what is shown and asks again.
        if (stop || res.status === 404) return
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const body = await res.text()
        if (stop) return
        cues.current = assCues(body)
        place(tt, cues.current, shift.current)
        if (!whole && res.headers.get('X-Yoru-Complete') !== 'true') timer = window.setTimeout(fill, 10_000)
      } catch {
        if (!stop) timer = window.setTimeout(fill, 10_000)
      }
    }
    void fill()
    tt.mode = 'showing'
    return () => {
      stop = true
      window.clearTimeout(timer)
      tt.mode = 'hidden'
    }
  }, [video, url, whole, showing])

  // A delay moved is applied without fetching again.
  useEffect(() => {
    shift.current = delay
    if (track.current && showing) place(track.current.tt, cues.current, delay)
  }, [delay, showing])
}

function place(tt: TextTrack, lines: Cue[], delay: number) {
  for (const old of [...(tt.cues ?? [])]) tt.removeCue(old)
  for (const c of lines) {
    if (c.end + delay > 0) tt.addCue(new VTTCue(Math.max(0, c.start + delay), c.end + delay, c.text))
  }
}
